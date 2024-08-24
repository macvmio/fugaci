package fugaci

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/creack/pty"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/virtual-kubelet/node-cli/manager"
	"github.com/virtual-kubelet/virtual-kubelet/errdefs"
	vknode "github.com/virtual-kubelet/virtual-kubelet/node"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"github.com/virtual-kubelet/virtual-kubelet/node/api/statsv1alpha1"
	"github.com/virtual-kubelet/virtual-kubelet/node/nodeutil"
	"io"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"os"
	"os/exec"
	"sync"
)

var _ vknode.PodLifecycleHandler = (*Provider)(nil)
var __ nodeutil.Provider = (*Provider)(nil)

type Provider struct {
	resourceManager *manager.ResourceManager
	cfg             Config

	// Mutex to synchronize access to the in-memory store.
	mu sync.Mutex
	// In-memory store for Pods.
	podStore map[string]*v1.Pod
}

// PrettyPrintStruct uses the json.MarshalIndent function to pretty print a struct.
func PrettyPrintStruct(i interface{}) {
	// MarshalIndent struct to JSON with pretty print
	jsonData, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		log.Fatalf("Error occurred during marshaling. Error: %s", err.Error())
	}
	log.Println(string(jsonData))
}

// NewZunProvider creates a new ZunProvider.
func NewProvider(cfg Config) (*Provider, error) {
	return &Provider{
		cfg:      cfg,
		podStore: make(map[string]*v1.Pod),
	}, nil
}

func (s *Provider) prefixOnNodeMatch(assignedNode string) string {
	var prefix = "[OTHER]"
	if assignedNode == s.cfg.NodeName {
		prefix = "[CURRENT]"
	}
	return prefix
}

func (s *Provider) NodeName() string {
	return s.cfg.NodeName
}

func (s *Provider) isPodAllowed(pod *v1.Pod) bool {
	return pod.Spec.NodeName == s.cfg.NodeName
}

// CreatePod simulates creating a macOS VM.
func (s *Provider) CreatePod(ctx context.Context, pod *v1.Pod) error {
	if !s.isPodAllowed(pod) {
		return nil
	}
	log.Printf("%s Creating VM for Pod %s/%s on nodeSelector %#v: #%v", s.prefixOnNodeMatch(pod.Spec.NodeName), pod.Namespace, pod.Name, pod.Spec.NodeSelector, pod.Spec.NodeName)

	// Implement VM creation logic here (simulated by storing the pod in memory)

	// Store the pod in the in-memory store
	s.mu.Lock()
	defer s.mu.Unlock()
	s.podStore[namespaceNameKey(pod.Namespace, pod.Name)] = pod

	return nil
}

// GetPod returns a dummy Pod or looks it up in the in-memory store to satisfy the provider interface.
func (s *Provider) GetPod(ctx context.Context, namespace, name string) (*v1.Pod, error) {
	// Look up the pod in the in-memory store
	s.mu.Lock()
	defer s.mu.Unlock()

	if pod, exists := s.podStore[namespaceNameKey(namespace, name)]; exists {
		log.Printf("Found pod %s/%s on node %s", namespace, pod.Name, s.cfg.NodeName)
		pod2 := *pod
		pod2.ManagedFields = nil
		PrettyPrintStruct(pod2)
		return pod, nil
	}

	// If not found, return nil or an error
	return nil, nil
}

// namespaceNameKey generates a key for the in-memory store based on the namespace and name of the pod.
func namespaceNameKey(namespace, name string) string {
	return namespace + "/" + name
}

// UpdatePod simulates updating a macOS VM
func (s *Provider) UpdatePod(ctx context.Context, pod *v1.Pod) error {
	log.Printf("Updating VM for Pod %s/%s on node %s", pod.Namespace, pod.Name, s.cfg.NodeName)
	// Implement VM update logic here
	return nil
}

// DeletePod simulates deleting a macOS VM.
func (s *Provider) DeletePod(ctx context.Context, pod *v1.Pod) error {
	if !s.isPodAllowed(pod) {
		return nil
	}
	log.Printf("[%s] Deleting VM for Pod %s/%s", s.prefixOnNodeMatch(s.cfg.NodeName), pod.Namespace, pod.Name)

	// Lock the mutex to ensure thread-safe access to the podStore
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate the key for the pod
	key := namespaceNameKey(pod.Namespace, pod.Name)

	// Check if the pod exists in the store
	if _, exists := s.podStore[key]; exists {
		// Simulate VM deletion by removing the pod from the in-memory store
		delete(s.podStore, key)
		log.Printf("Pod %s/%s successfully deleted", pod.Namespace, pod.Name)
	} else {
		log.Printf("Pod %s/%s not found in the store", pod.Namespace, pod.Name)
		return errdefs.NotFound("not found")
	}

	// Implement additional VM deletion logic here if necessary

	return nil
}

// GetPodStatus returns a dummy Pod status
func (s *Provider) GetPodStatus(ctx context.Context, namespace, name string) (*v1.PodStatus, error) {
	log.Printf("[%s] GetPodStatus for %s/%s", s.cfg.NodeName, namespace, name)
	pod, err := s.GetPod(ctx, namespace, name)
	if err != nil || pod == nil {
		log.Printf("[%s] Error getting pod %s/%s", s.cfg.NodeName, namespace, name)
		return nil, errdefs.NotFound("pod not found")
	}
	now := metav1.Now()
	pod.Status.Phase = v1.PodRunning
	pod.Status.StartTime = &now
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
	//TODO implement me
	panic("implement me")
}

func (s *Provider) GetMetricsResource(ctx context.Context) ([]*io_prometheus_client.MetricFamily, error) {
	//TODO implement me
	panic("implement me")
}

func (s *Provider) PortForward(ctx context.Context, namespace, pod string, port int32, stream io.ReadWriteCloser) error {
	//TODO implement me
	panic("implement me")
}
