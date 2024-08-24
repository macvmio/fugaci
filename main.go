package main

import (
	"context"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"log"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/tomekjarosik/fugaci/pkg/fugaci"
	"github.com/virtual-kubelet/virtual-kubelet/node"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func main() {

	// vk_log.L = klogv2.New(nil)

	// Create a context that cancels on OS signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Initialize your custom FugaciProvider
	nodeName := os.Getenv("FUGACI_NODE_NAME")
	if nodeName == "" {
		log.Fatal("FUGACI_NODE_NAME environment variable not set")
		return
	}
	provider, err := fugaci.NewFugaciProvider(nodeName)
	if err != nil {
		log.Fatalf("Failed to initialize Fugaci provider: %v", err)
	}

	// Define the node name and other options
	pNode := corev1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
		Spec:   corev1.NodeSpec{},
		Status: corev1.NodeStatus{},
	}

	provider.ConfigureNode(ctx, &pNode)

	client, err := newClient("/Users/tomek/.kube/config")
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
		return
	}

	leaseClient := client.CoordinationV1().Leases(corev1.NamespaceNodeLease)
	nc, _ := node.NewNodeController(
		node.NewNaiveNodeProvider(),
		&pNode,
		client.CoreV1().Nodes(),
		node.WithNodeEnableLeaseV1(leaseClient, 60))

	// Create a shared informer factory with a default resync period
	informerFactory := informers.NewSharedInformerFactory(client, 30*time.Second)

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
	podController, err := node.NewPodController(node.PodControllerConfig{
		PodClient:         client.CoreV1(),
		PodInformer:       podInformer,
		EventRecorder:     eb.NewRecorder(scheme.Scheme, corev1.EventSource{Component: path.Join(pNode.Name, "pod-controller")}),
		Provider:          provider,
		ConfigMapInformer: configMapInformer,
		SecretInformer:    secretInformer,
		ServiceInformer:   serviceInformer,
		//SyncPodsFromKubernetesRateLimiter:        rateLimiter,
		//DeletePodsFromKubernetesRateLimiter:      rateLimiter,
		//SyncPodStatusFromProviderRateLimiter:     rateLimiter,
		PodEventFilterFunc: func(ctx context.Context, pod *v1.Pod) bool {
			return true // Process all pods; you can add filtering logic here
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize Pod controller: %v", err)
	}

	// Start the informers
	informerFactory.Start(ctx.Done())

	// Wait for informers to sync
	informerFactory.WaitForCacheSync(ctx.Done())

	// Run the controller
	go podController.Run(ctx, 1) // 1 worker
	go func() {
		if err := nc.Run(ctx); err != nil {
			log.Fatalf("Error running controller: %v", err)
		}
	}()

	// Block forever
	<-ctx.Done()
}

func newClient(configPath string) (*kubernetes.Clientset, error) {
	var config *rest.Config

	// Check if the kubeConfig file exists.
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		// Get the kubeconfig from the filepath.
		config, err = clientcmd.BuildConfigFromFlags("", configPath)
		if err != nil {
			return nil, errors.Wrap(err, "error building client config")
		}
	} else {
		// Set to in-cluster config.
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, errors.Wrap(err, "error building in cluster config")
		}
	}

	if masterURI := os.Getenv("MASTER_URI"); masterURI != "" {
		config.Host = masterURI
	}

	return kubernetes.NewForConfig(config)
}
