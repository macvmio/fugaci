package fugaci

import (
	"context"
	"github.com/pkg/errors"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"github.com/virtual-kubelet/virtual-kubelet/node/api/statsv1alpha1"
	"io"
	v1 "k8s.io/api/core/v1"
	"log"
)

var ErrNotImplemented = errors.New("not implemented")

// GetPods returns a list of dummy Pods to satisfy the provider interface
func (s *Provider) GetPods(ctx context.Context) ([]*v1.Pod, error) {
	log.Printf("Getting all Pods on node %s", s.cfg.NodeName)
	// Return a list of pods or empty list
	return []*v1.Pod{}, nil
}

func (s *Provider) ContainerExecHandler(ctx context.Context, namespace, podName, containerName string, cmd []string, attach api.AttachIO) error {
	return nil
}

func (s *Provider) ContainerAttachHandler(ctx context.Context, namespace, podName, containerName string, attach api.AttachIO) error {
	return ErrNotImplemented
}

func (s *Provider) PortForwardHandler(ctx context.Context, namespace, pod string, port int32, stream io.ReadWriteCloser) error {
	return ErrNotImplemented
}

func (s *Provider) ContainerLogsHandler(ctx context.Context, namespace, podName, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	return nil, ErrNotImplemented
}

func (s *Provider) PodStatsSummaryHandler(context.Context) (*statsv1alpha1.Summary, error) {
	return nil, ErrNotImplemented
}
