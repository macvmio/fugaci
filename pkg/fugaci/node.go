package fugaci

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Node struct {
	name                string
	curieVersion        string
	kubeletEndpointPort int32
}

func (s *Node) Name() string {
	return s.name
}

func parseSysctlIntOutput(out string) (int, error) {
	val := strings.TrimSpace(out)
	return strconv.Atoi(val)
}

func parseSysctlUint64Output(out string) (uint64, error) {
	val := strings.TrimSpace(out)
	return strconv.ParseUint(val, 10, 64)
}

// getCPUCores gets the number of logical CPU cores on macOS
func (s *Node) getCPUCores() (int, error) {
	out, err := sysctlN("hw.logicalcpu")
	if err != nil {
		return 0, err
	}
	return parseSysctlIntOutput(out)
}

// getMemoryBytes gets the total system memory in bytes on macOS
func (s *Node) getMemoryBytes() (uint64, error) {
	out, err := sysctlN("hw.memsize")
	if err != nil {
		return 0, err
	}
	return parseSysctlUint64Output(out)
}

func (s *Node) capacity() v1.ResourceList {
	cpuCount, err := s.getCPUCores()
	if err != nil {
		cpuCount = 8
	}

	memBytes, err := s.getMemoryBytes()
	if err != nil {
		memBytes = 16 * 1024 * 1024 * 1024
	}

	// Convert memory from bytes to a human-readable format for Kubernetes
	memQuantity := resource.NewQuantity(int64(memBytes), resource.BinarySI)

	return v1.ResourceList{
		v1.ResourceCPU:    *resource.NewQuantity(int64(cpuCount), resource.DecimalSI),
		v1.ResourceMemory: *memQuantity,
		v1.ResourcePods:   resource.MustParse("2"),
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
// TODO(tjarosik):
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
			Port: s.kubeletEndpointPort,
		},
	}
}

func (s *Node) OperatingSystem() string {
	return runtime.GOOS
}

func (s *Node) Architecture() string {
	return runtime.GOARCH
}

func (s *Node) GetKernelVersion() string {
	output, err := sysctlN("kern.osrelease")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(output)
}

func (s *Node) GetOSImage() string {
	// Check the operating system at runtime
	switch runtime.GOOS {
	case "darwin": // macOS
		return s.getMacOSImage()
	case "linux": // Linux
		return "TODO"
	default:
		return "unknown"
	}
}

// Get macOS image using sw_vers for product version and build version
func (s *Node) getMacOSImage() string {
	productVersion, err := exec.Command("sw_vers", "--productVersion").Output()
	if err != nil {
		return "unknown"
	}
	buildVersion, err := exec.Command("sw_vers", "--buildVersion").Output()
	if err != nil {
		return "unknown"
	}

	return fmt.Sprintf("macOS %s (Build %s)", strings.TrimSpace(string(productVersion)), strings.TrimSpace(string(buildVersion)))
}

// sysctlSettings is the list of sysctl parameters that we want to query
var sysctlSettings = []string{
	"kern.ostype",
	"kern.osrelease",
	"kern.osversion",
	"kern.hostname",
	"hw.machine",
	"hw.model",
	"hw.ncpu",
	"hw.physicalcpu",
	"hw.logicalcpu",
	"hw.memsize",
	"hw.cpufrequency",
	"hw.l1icachesize",
	"hw.l1dcachesize",
	"hw.l2cachesize",
	"hw.l3cachesize",
	"vm.swapusage",
	"net.inet.ip.forwarding",
	"net.inet.tcp.rfc1323",
	"net.inet.tcp.keepidle",
	"net.inet.tcp.sendspace",
	"net.inet.tcp.recvspace",
	"vfs.usermount",
	"vfs.generic.iosize",
	"kern.maxproc",
	"kern.maxfiles",
	"kern.maxfilesperproc",
	"machdep.cpu.brand_string",
	"machdep.cpu.features",
}

// sysctlN runs `sysctl -n <name>` and returns the result as a string
func sysctlN(name string) (string, error) {
	// Execute sysctl command
	output, err := exec.Command("sysctl", "-n", name).Output()
	if err != nil {
		return "", err
	}
	// Return the trimmed output to remove any extra whitespace
	return strings.TrimSpace(string(output)), nil
}

// getSysctlInfo returns a map where the key is "sysctl.<name>" and the value is the result of `sysctl -n <name>`
func addSysctlInfo(sysctlMap map[string]string) map[string]string {
	// Loop through the sysctlSettings array and call sysctlN for each
	for _, setting := range sysctlSettings {
		value, err := sysctlN(setting)
		if err == nil && len(value) > 0 {
			sysctlMap[setting] = value
		}
	}

	return sysctlMap
}

func (s *Node) Configure(node *corev1.Node) {
	node.Spec.Taints = append(node.Spec.Taints, v1.Taint{
		Key:    "fugaci.jarosik.online",
		Value:  "true",
		Effect: v1.TaintEffectNoSchedule,
	})
	// TODO(tjarosik): node.Status.NodeInfo.KubeletVersion = "" fugaci version
	node.Status.NodeInfo.ContainerRuntimeVersion = s.curieVersion
	node.Status.Capacity = s.capacity()
	node.Status.Allocatable = s.capacity()
	node.Status.Conditions = s.conditions()
	node.Status.Addresses = s.addresses()
	node.Status.DaemonEndpoints = s.daemonEndpoints()
	node.Status.NodeInfo.OperatingSystem = s.OperatingSystem()
	node.Status.NodeInfo.OSImage = s.GetOSImage()
	node.Status.NodeInfo.KernelVersion = s.GetKernelVersion()
	node.Status.NodeInfo.Architecture = s.Architecture()
	if node.ObjectMeta.Labels == nil {
		node.ObjectMeta.Labels = map[string]string{}
	}
	node.ObjectMeta.Labels["kubernetes.io/os"] = s.OperatingSystem()
	// some useful annotation, not needed for correctness
	if node.ObjectMeta.Annotations == nil {
		node.ObjectMeta.Annotations = map[string]string{}
	}
	addSysctlInfo(node.ObjectMeta.Annotations)
}

func NewNode(cfg Config) Node {
	out, err := exec.Command(cfg.CurieBinaryPath, "version").CombinedOutput()
	var curieVersion string
	if err != nil {
		curieVersion = err.Error()
	} else {
		curieVersion = strings.TrimSpace(string(out))
	}

	return Node{
		name:                cfg.NodeName,
		curieVersion:        curieVersion,
		kubeletEndpointPort: cfg.KubeletEndpointPort,
	}
}
