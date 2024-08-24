package fugaci

import (
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"runtime"
	"time"
)

type Node struct {
	name string
}

func (s *Node) Name() string {
	return s.name
}

// capacity returns the fake capacity of the node
// TODO:
func (s *Node) capacity() v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("16"),   // 16 CPUs
		v1.ResourceMemory: resource.MustParse("64Gi"), // 64 GB RAM
		v1.ResourcePods:   resource.MustParse("2"),    // 2 Pods
	}
}

// nodeConditions returns the fake node conditions
func (s *Node) conditions() []v1.NodeCondition {
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
func (s *Node) addresses() []v1.NodeAddress {
	return []v1.NodeAddress{
		{
			Type:    v1.NodeInternalIP,
			Address: "192.168.1.99",
		},
		{
			Type:    v1.NodeHostName,
			Address: "fugaci-node",
		},
	}
}

// nodeDaemonEndpoints returns the fake daemon endpoints
func (s *Node) daemonEndpoints() v1.NodeDaemonEndpoints {
	return v1.NodeDaemonEndpoints{
		KubeletEndpoint: v1.DaemonEndpoint{
			Port: 10250,
		},
	}
}

func (s *Node) OperatingSystem() string {
	return runtime.GOOS
}

func (s *Node) Configure(node *corev1.Node) {
	node.Spec.Taints = append(node.Spec.Taints, v1.Taint{
		Key:    "fugaci.virtual-kubelet.io",
		Value:  "true",
		Effect: v1.TaintEffectNoSchedule,
	})
	node.Status.Capacity = s.capacity()
	node.Status.Allocatable = s.capacity()
	node.Status.Conditions = s.conditions()
	node.Status.Addresses = s.addresses()
	node.Status.DaemonEndpoints = s.daemonEndpoints()
	node.Status.NodeInfo.OperatingSystem = s.OperatingSystem()
	node.ObjectMeta.Labels = make(map[string]string)
	node.ObjectMeta.Labels["kubernetes.io/os"] = s.OperatingSystem()
}

func NewNode(cfg Config) Node {
	return Node{
		name: cfg.NodeName,
	}
}
