package fugaci

import (
	"context"
	"errors"
	"fmt"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/tomekjarosik/fugaci/pkg/curie"
	"github.com/virtual-kubelet/node-cli/manager"
	"github.com/virtual-kubelet/virtual-kubelet/errdefs"
	vknode "github.com/virtual-kubelet/virtual-kubelet/node"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"github.com/virtual-kubelet/virtual-kubelet/node/api/statsv1alpha1"
	"github.com/virtual-kubelet/virtual-kubelet/node/nodeutil"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
	"io"
	v1 "k8s.io/api/core/v1"
	"log"
	"os"
	"strings"
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
		virt:   curie.NewVirtualization(cfg.CurieBinaryPath),
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

// shellQuote properly quotes a string for use in a shell command
func shellQuote(s string) string {
	// Use single quotes if the string contains spaces or special characters
	if strings.ContainsAny(s, " '\"\\$`") {
		return fmt.Sprintf("'%s'", strings.ReplaceAll(s, "'", "'\"'\"'"))
	}
	return s
}

func (s *Provider) RunInContainer(ctx context.Context, namespace, podName, containerName string, cmd []string, attach api.AttachIO) error {
	vm, err := s.findVMByNames(namespace, podName, containerName)
	if err != nil {
		return fmt.Errorf("failed to find VM for pod %s/%s: %w", namespace, podName, err)
	}
	if len(vm.pod.Status.PodIP) == 0 {
		return fmt.Errorf("pod %s/%s has no IP address", namespace, podName)
	}
	// SSH client configuration
	config := &ssh.ClientConfig{
		User: "agent",
		Auth: []ssh.AuthMethod{
			ssh.Password("password"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	address := fmt.Sprintf("%s:22", vm.pod.Status.PodIP)
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return fmt.Errorf("failed to dial SSH to '%s': %w", address, err)
	}
	defer client.Close()

	// Create a session for running the command
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	// Set up the input/output streams
	session.Stdin = attach.Stdin()
	session.Stdout = attach.Stdout()
	session.Stderr = attach.Stderr()

	fileDescriptor := int(os.Stdin.Fd())
	if terminal.IsTerminal(fileDescriptor) {
		originalState, err := terminal.MakeRaw(fileDescriptor)
		if err != nil {
			return err
		}
		defer terminal.Restore(fileDescriptor, originalState)
	}
	// Handle TTY if needed
	if attach.TTY() {
		log.Printf("TTY attached to pod %s/%s", namespace, podName)
		modes := ssh.TerminalModes{
			ssh.ECHO:          1,     // enable echoing
			ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
			ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
		}

		if err := session.RequestPty("xterm-256color", 80, 40, modes); err != nil {
			return fmt.Errorf("request for pseudo terminal failed: %w", err)
		}
		go handleResize(session, attach.Resize())
	}

	// Quote each argument to handle spaces and special characters
	for i, arg := range cmd {
		cmd[i] = shellQuote(arg)
	}

	// Join the command and arguments into a single string
	commandStr := strings.Join(cmd, " ")
	// Start the command
	err = session.Start(commandStr)
	if err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Use a WaitGroup to wait for the command to complete
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := session.Wait(); err != nil {
			fmt.Printf("failed to execute command over SSH: %v\n", err)
		}
	}()

	// Wait for the command to finish
	wg.Wait()

	return nil

	return nil
}

// handleResize listens for resize events from the resize channel and adjusts the terminal size accordingly.
func handleResize(session *ssh.Session, resize <-chan api.TermSize) {
	for termSize := range resize {
		// Send the window change request to the SSH session with the new terminal size
		if err := session.WindowChange(int(termSize.Height), int(termSize.Width)); err != nil {
			log.Printf("failed to change window size: %v\n", err)
		}
	}
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
