package fugaci

import (
	"context"
	"errors"
	"fmt"
	"github.com/creack/pty"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/tomekjarosik/fugaci/pkg/curie"
	"github.com/virtual-kubelet/node-cli/manager"
	"github.com/virtual-kubelet/virtual-kubelet/errdefs"
	vknode "github.com/virtual-kubelet/virtual-kubelet/node"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"github.com/virtual-kubelet/virtual-kubelet/node/api/statsv1alpha1"
	"github.com/virtual-kubelet/virtual-kubelet/node/nodeutil"
	"io"
	v1 "k8s.io/api/core/v1"
	"log"
	"os"
	"os/exec"
	"sync"
)

var _ vknode.PodLifecycleHandler = (*Provider)(nil)
var __ nodeutil.Provider = (*Provider)(nil)

var ErrNotImplemented = errors.New("not implemented")

type Provider struct {
	resourceManager *manager.ResourceManager
	cfg             Config
	virt            *curie.Virtualization
	puller          Puller

	// Mutex to synchronize access to the in-memory store.
	mu sync.Mutex
	// In-memory store for Pods.
	vms [2]*VM
}

type noOpPuller struct {
}

func (n noOpPuller) Pull(ctx context.Context, image string, pullPolicy v1.PullPolicy) error {
	return nil
}

func NewProvider(cfg Config) (*LoggingProvider, error) {
	return NewLoggingProvider(&Provider{
		puller: &noOpPuller{},
		virt:   curie.NewVirtualization("/usr/local/bin/fakecurie"),
		cfg:    cfg,
		vms:    [2]*VM{},
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
		vm, err := NewVM(s.virt, s.puller, pod, 0)
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
		return s.vms[i], nil
	}
	return nil, errors.New("run out of slots to allocate VM")
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
		return fmt.Errorf("VM for pod (%s,%s) not found: %w", pod.Namespace, pod.Name, err)
	}

	err = vm.Cleanup()
	if err != nil {
		return fmt.Errorf("cleanup of VM for pod (%s,%s) failed: %w", pod.Namespace, pod.Name, err)
	}

	return s.deallocateVM(vm)
}

// GetPodStatus returns a dummy Pod status
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

func (s *Provider) ConfigureNode(ctx context.Context, node *v1.Node) {
	n := NewNode(s.cfg)
	n.Configure(node)
}

// TODO:
//func (s *Provider) NotifyPods(ctx context.Context, cb func(*v1.Pod)) {
//	log.Printf("Notifying pods on node %s", s.nodeName)
//}

func (s *Provider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	//TODO implement me
	panic("implement me")
}

func handleResize(tty *os.File, resizeCh <-chan api.TermSize) {
	for size := range resizeCh {
		// Handle TTY resize here
		pty.Setsize(tty, &pty.Winsize{Cols: size.Width, Rows: size.Height})
	}
}

func (s *Provider) RunInContainer(ctx context.Context, namespace, podName, containerName string, cmd []string, attach api.AttachIO) error {
	// Create the exec.CommandContext
	// Set the Stdin, Stdout, Stderr from the AttachIO interface
	execCmd := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	// Set the Stdin, Stdout, Stderr from the AttachIO interface
	execCmd.Stdin = attach.Stdin()
	execCmd.Stdout = attach.Stdout()
	execCmd.Stderr = attach.Stderr()
	// If TTY is enabled, use a pty (pseudo-terminal)
	if attach.TTY() {
		// TTY handling can be more complex, requiring packages like github.com/kr/pty
		// For simplicity, this is a placeholder; real TTY support would require more setup
		// Example using pty (pseudo-terminal):
		pty, tty, err := pty.Open()
		if err != nil {
			return fmt.Errorf("failed to open pty: %w", err)
		}
		defer pty.Close()
		execCmd.Stdin = tty
		execCmd.Stdout = tty
		execCmd.Stderr = tty
		go handleResize(tty, attach.Resize()) // Resize handler
	}

	// Use a WaitGroup to wait for the command to complete and handle streams closure
	var wg sync.WaitGroup

	// Close the output streams when done
	if stdout := attach.Stdout(); stdout != nil {
		defer stdout.Close()
	}
	if stderr := attach.Stderr(); stderr != nil {
		defer stderr.Close()
	}

	// Run the command asynchronously
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := execCmd.Run(); err != nil {
			fmt.Printf("failed to execute command in container: %v\n", err)
		}
	}()

	// Wait for command execution to finish
	wg.Wait()

	return nil
}

func (s *Provider) AttachToContainer(ctx context.Context, namespace, podName, containerName string, attach api.AttachIO) error {
	//TODO implement me
	panic("implement me")
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
