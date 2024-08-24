package bootstrap

import (
	"context"
	"github.com/tomekjarosik/fugaci/pkg/fugaci"
	"github.com/virtual-kubelet/virtual-kubelet/node"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func NewNodeController(ctx context.Context, client *kubernetes.Clientset, cfg fugaci.Config) (*node.NodeController, error) {
	// TODO: Simplify node configs
	localNode := fugaci.NewNode(ctx, cfg)
	kubernetesNode := corev1.Node{} // TODO: There is "v1.Node()" which is declarative
	localNode.Configure(ctx, &kubernetesNode)

	leaseClient := client.CoordinationV1().Leases(corev1.NamespaceNodeLease)
	nodeController, err := node.NewNodeController(
		node.NewNaiveNodeProvider(),
		&kubernetesNode,
		client.CoreV1().Nodes(),
		node.WithNodeEnableLeaseV1(leaseClient, 60))

	return nodeController, err
}
