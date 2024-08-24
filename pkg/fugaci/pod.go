package fugaci

import (
	"context"
	"encoding/json"
	"github.com/virtual-kubelet/node-cli/manager"
	"github.com/virtual-kubelet/virtual-kubelet/errdefs"
	vknode "github.com/virtual-kubelet/virtual-kubelet/node"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"sync"
)

var _ vknode.PodLifecycleHandler = (*Provider)(nil)

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
	if namespace != "jenkins" {
		return nil, nil
	}
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
