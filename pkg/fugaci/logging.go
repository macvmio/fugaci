package fugaci

import (
	"context"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"github.com/virtual-kubelet/virtual-kubelet/node/api/statsv1alpha1"
	"io"
	v1 "k8s.io/api/core/v1"
	"log"
)

// LoggingProvider wraps an existing Provider and logs the responses
type LoggingProvider struct {
	UnderlyingProvider *Provider
}

// NewLoggingProvider creates a new LoggingProvider
func NewLoggingProvider(underlyingProvider *Provider) *LoggingProvider {
	return &LoggingProvider{UnderlyingProvider: underlyingProvider}
}

func (lp *LoggingProvider) NodeName() string {
	nodeName := lp.UnderlyingProvider.NodeName()
	log.Printf("NodeName called: %s", nodeName)
	return nodeName
}

func (lp *LoggingProvider) CreatePod(ctx context.Context, pod *v1.Pod) error {
	err := lp.UnderlyingProvider.CreatePod(ctx, pod)
	if err != nil {
		log.Printf("CreatePod failed: %v", err)
	} else {
		log.Printf("CreatePod succeeded for Pod %s/%s", pod.Namespace, pod.Name)
	}
	return err
}

func (lp *LoggingProvider) GetPod(ctx context.Context, namespace, name string) (*v1.Pod, error) {
	pod, err := lp.UnderlyingProvider.GetPod(ctx, namespace, name)
	if err != nil {
		log.Printf("GetPod failed for Pod %s/%s: %v", namespace, name, err)
	} else {
		log.Printf("GetPod succeeded for Pod %s/%s: %#v", namespace, name, pod)
	}
	return pod, err
}

func (lp *LoggingProvider) UpdatePod(ctx context.Context, pod *v1.Pod) error {
	err := lp.UnderlyingProvider.UpdatePod(ctx, pod)
	if err != nil {
		log.Printf("UpdatePod failed: %v", err)
	} else {
		log.Printf("UpdatePod succeeded for Pod %s/%s", pod.Namespace, pod.Name)
	}
	return err
}

func (lp *LoggingProvider) DeletePod(ctx context.Context, pod *v1.Pod) error {
	err := lp.UnderlyingProvider.DeletePod(ctx, pod)
	if err != nil {
		log.Printf("DeletePod failed: %v", err)
	} else {
		log.Printf("DeletePod succeeded for Pod %s/%s", pod.Namespace, pod.Name)
	}
	return err
}

func (lp *LoggingProvider) GetPodStatus(ctx context.Context, namespace, name string) (*v1.PodStatus, error) {
	status, err := lp.UnderlyingProvider.GetPodStatus(ctx, namespace, name)
	if err != nil {
		log.Printf("GetPodStatus failed for Pod %s/%s: %v", namespace, name, err)
	} else {
		log.Printf("GetPodStatus succeeded for Pod %s/%s: %#v", namespace, name, status)
	}
	return status, err
}

func (lp *LoggingProvider) GetPods(ctx context.Context) ([]*v1.Pod, error) {
	pods, err := lp.UnderlyingProvider.GetPods(ctx)
	if err != nil {
		log.Printf("GetPods failed: %v", err)
	} else {
		log.Printf("GetPods succeeded: %#v", pods)
	}
	return pods, err
}

func (lp *LoggingProvider) ConfigureNode(ctx context.Context, fugaciVersion string, node *v1.Node) error {
	log.Printf("ConfigureNode called: %#v", node)

	return lp.UnderlyingProvider.ConfigureNode(ctx, fugaciVersion, node)
}

func (lp *LoggingProvider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	logs, err := lp.UnderlyingProvider.GetContainerLogs(ctx, namespace, podName, containerName, opts)
	if err != nil {
		log.Printf("GetContainerLogs failed for Pod %s/%s, Container %s: %v", namespace, podName, containerName, err)
	} else {
		log.Printf("GetContainerLogs succeeded for Pod %s/%s, Container %s", namespace, podName, containerName)
	}
	return logs, err
}

func (lp *LoggingProvider) RunInContainer(ctx context.Context, namespace, podName, containerName string, cmd []string, attach api.AttachIO) error {
	err := lp.UnderlyingProvider.RunInContainer(ctx, namespace, podName, containerName, cmd, attach)
	if err != nil {
		log.Printf("RunInContainer failed for Pod %s/%s, Container %s: %v", namespace, podName, containerName, err)
	} else {
		log.Printf("RunInContainer succeeded for Pod %s/%s, Container %s", namespace, podName, containerName)
	}
	return err
}

func (lp *LoggingProvider) AttachToContainer(ctx context.Context, namespace, podName, containerName string, attach api.AttachIO) error {
	err := lp.UnderlyingProvider.AttachToContainer(ctx, namespace, podName, containerName, attach)
	if err != nil {
		log.Printf("AttachToContainer failed for Pod %s/%s, Container %s: %v", namespace, podName, containerName, err)
	} else {
		log.Printf("AttachToContainer succeeded for Pod %s/%s, Container %s", namespace, podName, containerName)
	}
	return err
}

func (lp *LoggingProvider) GetStatsSummary(ctx context.Context) (*statsv1alpha1.Summary, error) {
	summary, err := lp.UnderlyingProvider.GetStatsSummary(ctx)
	if err != nil {
		log.Printf("GetStatsSummary failed: %v", err)
	} else {
		log.Printf("GetStatsSummary succeeded: %#v", summary)
	}
	return summary, err
}

func (lp *LoggingProvider) GetMetricsResource(ctx context.Context) ([]*io_prometheus_client.MetricFamily, error) {
	metrics, err := lp.UnderlyingProvider.GetMetricsResource(ctx)
	if err != nil {
		log.Printf("GetMetricsResource failed: %v", err)
	} else {
		log.Printf("GetMetricsResource succeeded: %#v", metrics)
	}
	return metrics, err
}

func (lp *LoggingProvider) PortForward(ctx context.Context, namespace, pod string, port int32, stream io.ReadWriteCloser) error {
	err := lp.UnderlyingProvider.PortForward(ctx, namespace, pod, port, stream)
	if err != nil {
		log.Printf("PortForward failed for Pod %s/%s, Port %d: %v", namespace, pod, port, err)
	} else {
		log.Printf("PortForward succeeded for Pod %s/%s, Port %d", namespace, pod, port)
	}
	return err
}
