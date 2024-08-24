package bootstrap

import (
	"context"
	"github.com/tomekjarosik/fugaci/pkg/fugaci"
	"github.com/virtual-kubelet/virtual-kubelet/node"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"path"
)

func NewPodController(ctx context.Context, informerFactory informers.SharedInformerFactory, client *kubernetes.Clientset, provider *fugaci.Provider) (*node.PodController, error) {
	// Create informers for Pods, ConfigMaps, Secrets, and Services
	podInformer := informerFactory.Core().V1().Pods()
	configMapInformer := informerFactory.Core().V1().ConfigMaps()
	secretInformer := informerFactory.Core().V1().Secrets()
	serviceInformer := informerFactory.Core().V1().Services()

	// Create a simple rate limiter
	//rateLimiter := workqueue.NewItemExponentialFailureRateLimiter(1*time.Second, 5*time.Minute)

	// Create a simple retry function
	//retryFunc := func(obj interface{}, err error, count int) (bool, time.Duration) {
	//	return true, 1 * time.Second
	//}

	eb := record.NewBroadcaster()
	//eb.StartLogging(log.G(ctx).Infof)
	//eb.StartRecordingToSink(&corev1client.EventSinkImpl{Interface: client.CoreV1().Events(c.KubeNamespace)})

	// Create the PodController
	return node.NewPodController(node.PodControllerConfig{
		PodClient:         client.CoreV1(),
		PodInformer:       podInformer,
		EventRecorder:     eb.NewRecorder(scheme.Scheme, corev1.EventSource{Component: path.Join(provider.NodeName(), "pod-controller")}),
		Provider:          provider,
		ConfigMapInformer: configMapInformer,
		SecretInformer:    secretInformer,
		ServiceInformer:   serviceInformer,
		//SyncPodsFromKubernetesRateLimiter:        rateLimiter,
		//DeletePodsFromKubernetesRateLimiter:      rateLimiter,
		//SyncPodStatusFromProviderRateLimiter:     rateLimiter,
		PodEventFilterFunc: func(ctx context.Context, pod *corev1.Pod) bool {
			return true // Process all pods; you can add filtering logic here
		},
	})
}
