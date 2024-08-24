package fugaci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/virtual-kubelet/node-cli/manager"
	"github.com/virtual-kubelet/virtual-kubelet/errdefs"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"io"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sync"

	"log"
	"time"
)

type FugaciProvider struct {
	resourceManager    *manager.ResourceManager
	region             string
	nodeName           string
	cpu                string
	memory             string
	pods               string
	daemonEndpointPort int32

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
func NewFugaciProvider(nodeName string) (*FugaciProvider, error) {
	return &FugaciProvider{
		nodeName: nodeName,
		podStore: make(map[string]*v1.Pod),
	}, nil
}

// CreatePod simulates creating a macOS VM.
func (s *FugaciProvider) CreatePod(ctx context.Context, pod *v1.Pod) error {
	log.Printf("Creating VM for Pod %s/%s on nodeSelector %#v: #%v", pod.Namespace, pod.Name, pod.Spec.NodeSelector, pod.Spec.NodeName)
	if pod.Namespace != "jenkins" {
		return errors.New("only support jenkins pods")
	}

	// Implement VM creation logic here (simulated by storing the pod in memory)

	// Store the pod in the in-memory store
	s.mu.Lock()
	defer s.mu.Unlock()
	s.podStore[namespaceNameKey(pod.Namespace, pod.Name)] = pod

	return nil
}

// GetPod returns a dummy Pod or looks it up in the in-memory store to satisfy the provider interface.
func (s *FugaciProvider) GetPod(ctx context.Context, namespace, name string) (*v1.Pod, error) {

	if namespace == "jenkins" {
		log.Printf("[%s] GetPod %s/%s", s.nodeName, namespace, name)
	}

	// Look up the pod in the in-memory store
	s.mu.Lock()
	defer s.mu.Unlock()

	if pod, exists := s.podStore[namespaceNameKey(namespace, name)]; exists {
		log.Printf("Found pod %s/%s on node %s", namespace, pod.Name, s.nodeName)
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
func (s *FugaciProvider) UpdatePod(ctx context.Context, pod *v1.Pod) error {
	log.Printf("Updating VM for Pod %s/%s on node %s", pod.Namespace, pod.Name, s.nodeName)
	// Implement VM update logic here
	return nil
}

// DeletePod simulates deleting a macOS VM.
func (s *FugaciProvider) DeletePod(ctx context.Context, pod *v1.Pod) error {
	log.Printf("[%s] Deleting VM for Pod %s/%s", s.nodeName, pod.Namespace, pod.Name)

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

// GetContainerLogs simulates fetching logs from a macOS VM
func (s *FugaciProvider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	log.Printf("Fetching logs for Pod %s/%s on node %s", namespace, podName, s.nodeName)
	// Simulated log data
	logData := []byte("Simulated log data")

	// Return the log data as an io.ReadCloser
	return io.NopCloser(bytes.NewReader(logData)), nil
}

// GetPodStatus returns a dummy Pod status
func (s *FugaciProvider) GetPodStatus(ctx context.Context, namespace, name string) (*v1.PodStatus, error) {
	if namespace != "jenkins" {
		return nil, nil
	}
	log.Printf("[%s] GetPodStatus for %s/%s", s.nodeName, namespace, name)
	pod, err := s.GetPod(ctx, namespace, name)
	if err != nil || pod == nil {
		log.Printf("[%s] Error getting pod %s/%s", s.nodeName, namespace, name)
		return nil, errdefs.NotFound("pod not found")
	}
	now := metav1.Now()
	pod.Status.Phase = v1.PodRunning
	pod.Status.StartTime = &now
	return &pod.Status, nil
}

// GetPods returns a list of dummy Pods to satisfy the provider interface
func (s *FugaciProvider) GetPods(ctx context.Context) ([]*v1.Pod, error) {
	log.Printf("Getting all Pods on node %s", s.nodeName)
	// Return a list of pods or empty list
	return []*v1.Pod{}, nil
}

// Capacity returns dummy capacity information
func (s *FugaciProvider) Capacity(ctx context.Context) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("16"),   // 16 CPUs
		v1.ResourceMemory: resource.MustParse("64Gi"), // 64 GB RAM
		v1.ResourcePods:   resource.MustParse("2"),
	}
}

// NodeAddresses returns dummy addresses
func (s *FugaciProvider) NodeAddresses(ctx context.Context) []v1.NodeAddress {
	return []v1.NodeAddress{
		{
			Type:    v1.NodeInternalIP,
			Address: "192.168.1.100",
		},
	}
}

// NodeDaemonEndpoints returns dummy endpoint information
func (s *FugaciProvider) NodeDaemonEndpoints(ctx context.Context) *v1.NodeDaemonEndpoints {
	return &v1.NodeDaemonEndpoints{
		KubeletEndpoint: v1.DaemonEndpoint{
			Port: 10250,
		},
	}
}

// OperatingSystem returns the operating system the provider runs on
func (s *FugaciProvider) OperatingSystem() string {
	return "darwin"
}

// RunInContainer executes a command in a container in the pod, copying data
// between in/out/err and the container's stdin/stdout/stderr.
func (s *FugaciProvider) RunInContainer(ctx context.Context, namespace, name, container string, cmd []string, attach api.AttachIO) error {
	log.Printf("receive ExecInContainer %q\n", container)
	return nil
}

// capacity returns the fake capacity of the node
// TODO:
func (s *FugaciProvider) capacity() v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("16"),   // 16 CPUs
		v1.ResourceMemory: resource.MustParse("64Gi"), // 64 GB RAM
		v1.ResourcePods:   resource.MustParse("2"),    // 2 Pods
	}
}

// nodeConditions returns the fake node conditions
func (s *FugaciProvider) nodeConditions() []v1.NodeCondition {
	return []v1.NodeCondition{
		{
			Type:               v1.NodeReady,
			Status:             v1.ConditionTrue,
			LastHeartbeatTime:  metav1.NewTime(time.Now()),
			LastTransitionTime: metav1.NewTime(time.Now()),
		},
		{
			Type:               v1.NodeMemoryPressure,
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.NewTime(time.Now()),
			LastTransitionTime: metav1.NewTime(time.Now()),
		},
		{
			Type:               v1.NodeDiskPressure,
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.NewTime(time.Now()),
			LastTransitionTime: metav1.NewTime(time.Now()),
		},
		{
			Type:               v1.NodeNetworkUnavailable,
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.NewTime(time.Now()),
			LastTransitionTime: metav1.NewTime(time.Now()),
		},
	}
}

// nodeAddresses returns the fake node addresses
// TODO:
func (s *FugaciProvider) nodeAddresses() []v1.NodeAddress {
	return []v1.NodeAddress{
		{
			Type:    v1.NodeInternalIP,
			Address: "192.168.1.100",
		},
		{
			Type:    v1.NodeHostName,
			Address: "fugaci-node",
		},
	}
}

// nodeDaemonEndpoints returns the fake daemon endpoints
func (s *FugaciProvider) nodeDaemonEndpoints() v1.NodeDaemonEndpoints {
	return v1.NodeDaemonEndpoints{
		KubeletEndpoint: v1.DaemonEndpoint{
			Port: 10250,
		},
	}
}

// TODO:
//func (s *FugaciProvider) NotifyPods(ctx context.Context, cb func(*v1.Pod)) {
//	log.Printf("Notifying pods on node %s", s.nodeName)
//}

// ConfigureNode enables the FugaciProvider to configure the node object
func (s *FugaciProvider) ConfigureNode(ctx context.Context, node *v1.Node) {
	node.Status.Capacity = s.capacity()
	node.Status.Allocatable = s.capacity()
	node.Status.Conditions = s.nodeConditions()
	node.Status.Addresses = s.nodeAddresses()
	node.Status.DaemonEndpoints = s.nodeDaemonEndpoints()
	node.Status.NodeInfo.OperatingSystem = s.OperatingSystem()
	node.ObjectMeta.Labels = make(map[string]string)
	node.ObjectMeta.Labels["kubernetes.io/os"] = s.OperatingSystem()
}
