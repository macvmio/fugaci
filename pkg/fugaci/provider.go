package fugaci

import (
	"context"
	"errors"
	"fmt"
	"github.com/macvmio/fugaci/pkg/curie"
	"github.com/macvmio/fugaci/pkg/sshrunner"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/virtual-kubelet/node-cli/manager"
	"github.com/virtual-kubelet/virtual-kubelet/errdefs"
	vknode "github.com/virtual-kubelet/virtual-kubelet/node"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"github.com/virtual-kubelet/virtual-kubelet/node/api/statsv1alpha1"
	"github.com/virtual-kubelet/virtual-kubelet/node/nodeutil"
	"io"
	v1 "k8s.io/api/core/v1"
	"log"
	"path"
	"strings"
	"sync"
)

var _ vknode.PodLifecycleHandler = (*Provider)(nil)
var __ nodeutil.Provider = (*Provider)(nil)

var ErrNotImplemented = errors.New("not implemented")

type Provider struct {
	appContext      context.Context
	resourceManager *manager.ResourceManager
	cfg             Config
	virt            *curie.Virtualization
	puller          Puller

	// Mutex to synchronize access to the in-memory store.
	mu sync.Mutex
	// In-memory store for Pods.
	vms [2]*VM
}

func NewProvider(appCtx context.Context, cfg Config) (*LoggingProvider, error) {
	return NewLoggingProvider(&Provider{
		appContext: appCtx,
		puller:     NewGeranosPuller(path.Join(cfg.CurieVirtualization.DataRootPath, "images")),
		virt:       curie.NewVirtualization(cfg.CurieVirtualization.BinaryPath, cfg.CurieVirtualization.DataRootPath),
		cfg:        cfg,
		vms:        [2]*VM{},
	}), nil
}

func (s *Provider) NodeName() string {
	return s.cfg.NodeName
}

func (s *Provider) allocateVM(pod *v1.Pod) (*VM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < len(s.vms); i++ {
		if s.vms[i] != nil {
			continue
		}
		vm, err := NewVM(s.appContext, s.virt, s.puller, sshrunner.NewRunner(), pod, 0)
		if err != nil {
			return nil, err
		}
		s.vms[i] = vm
		return vm, nil
	}
	return nil, errors.New("run out of slots to allocate VM")
}

func (s *Provider) findVM(pod *v1.Pod) (*VM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < len(s.vms); i++ {
		if s.vms[i] == nil {
			continue
		}
		if s.vms[i].pod.UID == pod.UID {
			return s.vms[i], nil
		}
	}
	return nil, errors.New("not found")
}

func (s *Provider) findVMByNames(namespace, podName, containerName string) (*VM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < len(s.vms); i++ {
		if s.vms[i] == nil {
			continue
		}
		// TODO: Add container name
		vm := s.vms[i]
		if vm.pod.Name == podName && vm.pod.Namespace == namespace {
			/*for _, status := range vm.pod.Status.ContainerStatuses {
				if status.Name == containerName {
					return vm, nil
				}
			}*/
			return vm, nil
		}
	}
	return nil, errors.New("not found")
}

func (s *Provider) deallocateVM(vm *VM) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < len(s.vms); i++ {
		if s.vms[i] == vm {
			s.vms[i] = nil
			return nil
		}
	}
	return errors.New("invalid VM passed for deallocation")
}

func (s *Provider) CreatePod(ctx context.Context, pod *v1.Pod) error {
	log.Printf("%s Creating VM for Pod %s/%s on nodeSelector %#v: #%v", pod.Spec.NodeName, pod.Namespace, pod.Name, pod.Spec.NodeSelector, pod.Spec.NodeName)
	log.Printf("CreatePod with data: %#v", pod)
	vm, err := s.allocateVM(pod)
	if err != nil {
		return fmt.Errorf("failed to allocate VM for Pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	go vm.Run()

	return nil
}

func (s *Provider) GetPod(ctx context.Context, namespace, name string) (*v1.Pod, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, vm := range s.vms {
		if vm != nil && vm.Matches(namespace, name) {
			return vm.GetPod(), nil
		}
	}
	return nil, errdefs.NotFound("pod not found")
}

func (s *Provider) UpdatePod(ctx context.Context, pod *v1.Pod) error {
	log.Printf("Updating VM for Pod %s/%s on node %s", pod.Namespace, pod.Name, s.cfg.NodeName)
	// Implement VM update logic here
	return nil
}

func (s *Provider) DeletePod(ctx context.Context, pod *v1.Pod) error {
	log.Printf("[%s] Deleting VM for Pod %s/%s", pod.Spec.NodeName, pod.Namespace, pod.Name)

	vm, err := s.findVM(pod)
	if err != nil {
		if pod.Status.ContainerStatuses != nil && len(pod.Status.ContainerStatuses[0].ContainerID) > 0 {
			err2 := s.virt.Destroy(ctx, pod.Status.ContainerStatuses[0].ContainerID)
			if err2 != nil {
				log.Printf("error destroying container %s for Pod %s/%s: %v", pod.Status.ContainerStatuses[0].ContainerID, pod.Namespace, pod.Name, err2)
			} else {
				log.Printf("deleted forgotten container %s for Pod %s/%s", pod.Status.ContainerStatuses[0].ContainerID, pod.Namespace, pod.Name)
			}
		}
		return errdefs.NotFoundf("VM for pod '%s/%s' not found: %w", pod.Namespace, pod.Name, err)
	}

	err = vm.Cleanup()
	if err != nil {
		return fmt.Errorf("cleanup of VM for pod '%s/%s' failed: %w", pod.Namespace, pod.Name, err)
	}

	return s.deallocateVM(vm)
}

func (s *Provider) GetPodStatus(ctx context.Context, namespace, name string) (*v1.PodStatus, error) {
	log.Printf("[%s] GetPodStatus for %s/%s", s.cfg.NodeName, namespace, name)
	pod, err := s.GetPod(ctx, namespace, name)
	if err != nil || pod == nil {
		log.Printf("[%s] Error getting pod %s/%s", s.cfg.NodeName, namespace, name)
		return nil, errdefs.NotFound("pod not found")
	}
	return &pod.Status, nil
}

// GetPods returns a list of dummy Pods to satisfy the provider interface
func (s *Provider) GetPods(ctx context.Context) ([]*v1.Pod, error) {
	log.Printf("Getting all Pods on node %s", s.cfg.NodeName)
	// Return a list of pods or empty list
	return []*v1.Pod{}, nil
}

func (s *Provider) ConfigureNode(ctx context.Context, fugaciVersion string, node *v1.Node) error {
	n := NewNode(fugaciVersion, s.cfg)
	return n.Configure(node)
}

// TODO(tjarosik):
//func (s *Provider) NotifyPods(ctx context.Context, cb func(*v1.Pod)) {
//	log.Printf("Notifying pods on node %s", s.nodeName)
//}

func (s *Provider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	// Return a simple static log line
	vm, err := s.findVMByNames(namespace, podName, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to find VM for pod %s/%s: %w", namespace, podName, err)
	}

	return io.NopCloser(strings.NewReader(strings.Join(vm.storyLine.Parts(), "\n"))), nil
}

func (s *Provider) RunInContainer(ctx context.Context, namespace, podName, containerName string, cmd []string, attach api.AttachIO) error {
	log.Printf("RunInContainer")
	vm, err := s.findVMByNames(namespace, podName, containerName)
	if err != nil {
		return fmt.Errorf("failed to find VM for pod %s/%s: %w", namespace, podName, err)
	}

	return vm.RunCommand(ctx, cmd, sshrunner.WithAttachIO(attach), sshrunner.WithEnv(vm.GetEnvVars()))
}

func (s *Provider) AttachToContainer(ctx context.Context, namespace, podName, containerName string, attach api.AttachIO) error {
	return ErrNotImplemented
}

func (s *Provider) GetStatsSummary(ctx context.Context) (*statsv1alpha1.Summary, error) {
	return nil, ErrNotImplemented
}

func (s *Provider) GetMetricsResource(ctx context.Context) ([]*io_prometheus_client.MetricFamily, error) {
	return nil, ErrNotImplemented
}

func (s *Provider) PortForward(ctx context.Context, namespace, pod string, port int32, stream io.ReadWriteCloser) error {
	return ErrNotImplemented
}
