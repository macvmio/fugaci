//go:build e2e
// +build e2e

package fugaci_test

/*
	To run these tests you need to have full setup with at least real Mac node connected to Kubernetes cluster
	You also need fugaci-ssh-secret to be defined.
	You might need to modify VM image reference too.
*/

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type PodTestCase struct {
	name       string
	podFiles   []string
	timeout    time.Duration
	postCreate func(clientset *kubernetes.Clientset, namespace, podName string, timeout time.Duration) error
	assertions func(t *testing.T, clientset *kubernetes.Clientset, config *rest.Config, pods []*v1.Pod)
}

func TestProviderE2E(t *testing.T) {
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		t.Fatalf("Failed to build kubeconfig: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// TODO(tjarosik):
	// Create the namespace for testing
	//if err := createNamespace(clientset); err != nil {
	//	t.Fatalf("Failed to create test namespace: %v", err)
	//}
	//defer deleteNamespace(clientset)

	// Define test cases
	tests := []PodTestCase{
		{
			name:       "TestBasicPod_MustBeRunning",
			podFiles:   []string{"pod1-basic-running.yaml"},
			timeout:    15 * time.Second,
			postCreate: waitForPodPhaseRunning,
		},
		{
			name:       "TestPodWithInvalidImage_MustBeInFailedPhase",
			podFiles:   []string{"pod2-invalid-image.yaml"},
			timeout:    15 * time.Second,
			postCreate: waitForPodPhaseFailed,
			assertions: func(t *testing.T, clientset *kubernetes.Clientset, config *rest.Config, pods []*v1.Pod) {
				p := pods[0]
				logs := getContainerLogs(t, clientset, testNamespace, p.Name, "curie3")
				assert.Contains(t, logs, "err=\"pull image: GET https://ghcr.com/v2/invalid/image/manifests/123: unexpected status code 404 Not Found")

				containerStatus := p.Status.ContainerStatuses[0]
				assert.False(t, containerStatus.Ready)
				assert.Equal(t, "unable to pull image", containerStatus.State.Terminated.Reason)
			},
		},
		{
			name:       "TestBasicPod_MustReturnLogs",
			podFiles:   []string{"pod1-basic-running.yaml"},
			timeout:    20 * time.Second,
			postCreate: waitForPodConditionReady,
			assertions: func(t *testing.T, clientset *kubernetes.Clientset, config *rest.Config, pods []*v1.Pod) {
				p := pods[0]
				logs := getContainerLogs(t, clientset, testNamespace, p.Name, "curie3")
				assert.Contains(t, logs, "spec.image=ghcr.io/macvmio/macos-sonoma:14.5-agent-v1.6")
				assert.Contains(t, logs, "action=pulling")
			},
		},
		{
			name:       "TestBasicPod_MustBeAbleToExecuteCommand",
			podFiles:   []string{"pod1-basic-running.yaml"},
			postCreate: waitForPodConditionReady,
			assertions: func(t *testing.T, clientset *kubernetes.Clientset, config *rest.Config, pods []*v1.Pod) {
				p := pods[0]
				command := []string{"sw_vers"}
				stdout, stderr := execCommandInPod(t, clientset, config, testNamespace, p.Name, "curie3", command)
				assert.Contains(t, stdout, "macOS")
				assert.Contains(t, stdout, "14.5")
				assert.Empty(t, stderr, "Stderr should be empty")
			},
		},
		{
			name:       "TestEphemeralPod_ShortLived",
			podFiles:   []string{"pod3-shortlived.yaml"},
			postCreate: waitForPodPhaseSucceeded,
			assertions: func(t *testing.T, clientset *kubernetes.Clientset, config *rest.Config, pods []*v1.Pod) {
				p := pods[0]
				logs := getContainerLogs(t, clientset, testNamespace, p.Name, "curie3")

				assert.Contains(t, logs, "spec.image=ghcr.io/macvmio/macos-sonoma:14.5-agent-v1.6")
				assert.Contains(t, logs, "action=pulling")
				assert.Contains(t, logs, "imageID=sha256:08bbe35549962a2aef9e79631f93c43a245aa662674cb88298be518aabbaed32")
				assert.Contains(t, logs, "state=created")
				assert.Contains(t, logs, "state=SSHReady")
				assert.Contains(t, logs, "action=stop")
				assert.Contains(t, logs, "container_command=\"[sleep 1]\"")
				assert.Contains(t, logs, "container_exitcode=0")
				//assert.NotContains(t, logs, "process still did not exit")
			},
		},
		{
			name:       "TestPodWithCustomEnvVars",
			podFiles:   []string{"pod4-custom-env-vars.yaml"},
			postCreate: waitForPodConditionReady,
			assertions: func(t *testing.T, clientset *kubernetes.Clientset, config *rest.Config, pods []*v1.Pod) {
				p := pods[0]
				command := []string{"env"}
				stdout, stderr := execCommandInPod(t, clientset, config, testNamespace, p.Name, "curie3", command)
				assert.Contains(t, stdout, "KUBERNETES_POD_NAME=testpod4")
				assert.Contains(t, stdout, "FUGACI_TESTKEY=some-test-value")
				assert.Contains(t, stdout, fmt.Sprintf("KUBERNETES_NODE_NAME=%v", p.Status.NominatedNodeName))
				assert.NotContains(t, stdout, "FUGACI_SSH_PASSWORD")
				assert.NotContains(t, stdout, "FUGACI_SSH_USERNAME")
				assert.Empty(t, stderr, "Stderr should be empty")
			},
		},
		//{
		//	// TODO: Figure out a way to delete test image
		//	name:       "TestPodPullStrategy_Always",
		//	podFiles:   []string{"pod5-pull-strategy-always.yaml"},
		//	timeout:    300 * time.Second,
		//	postCreate: waitForPodConditionReady,
		//},
		// TODO: Test all 3 pulling strategies
		// Add more test cases here
	}

	for _, tc := range tests {
		tc2 := tc // capture range variable
		t.Run(tc2.name, func(t *testing.T) {
			runPodTest(t, clientset, config, tc2)
		})
	}
}

func createNamespace(clientset *kubernetes.Clientset) error {
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	_, err := clientset.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	return err
}

func deleteNamespace(clientset *kubernetes.Clientset) {
	clientset.CoreV1().Namespaces().Delete(context.TODO(), testNamespace, metav1.DeleteOptions{})
}

func runPodTest(t *testing.T, clientset *kubernetes.Clientset, config *rest.Config, tc PodTestCase) {
	var pods []*v1.Pod
	var podNames []string

	// Create pods from YAML files
	for _, podFile := range tc.podFiles {
		pod, err := createPodFromYAML(clientset, podFile)
		require.NoError(t, err)
		pods = append(pods, pod)
		podNames = append(podNames, pod.Name)
	}

	defer func() {
		for _, podName := range podNames {
			deletePod(clientset, podName)
		}
	}()

	timeout := tc.timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	for _, pod := range pods {
		if err := tc.postCreate(clientset, pod.Namespace, pod.Name, timeout); err != nil {
			t.Fatalf("Pod %s did not reach Ready state: %v", pod.Name, err)
		}
	}

	for i, pod := range pods {
		updatedPod, err := clientset.CoreV1().Pods(testNamespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get updated pod %s: %v", pod.Name, err)
		}
		pods[i] = updatedPod
	}

	if tc.assertions != nil {
		tc.assertions(t, clientset, config, pods)
	}
}
