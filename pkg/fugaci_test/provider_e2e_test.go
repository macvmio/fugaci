//go:build e2e
// +build e2e

package fugaci_test

/*
	To run these tests you need to have full setup with at least real Mac node connected to Kubernetes cluster
	You also need fugaci-ssh-secret to be defined.
	You might need to modify VM image reference too.
*/

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"os"
	"strings"
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
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = clientcmd.RecommendedHomeFile
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
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
				assert.Contains(t, logs, "err=\"pull image: GET https://ghcr.io/token?scope=repository%3Ainvalid%2Fimage%3Apull&service=ghcr.io: DENIED: denied")

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
				assert.Contains(t, logs, "spec.image=ghcr.io/macvmio/macos-sonoma:14.5-agent-v1.7")
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
				assert.Contains(t, logs, "imageID=sha256:5e21ef1cd7e667ba8581f2df7bb292b7db23bc62df7137d3a1fa5790a57d3260")
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
		{
			name:       "TestAttachWithStdoutAndStderr",
			podFiles:   []string{"pod6-attach-stdout.yaml"},
			postCreate: waitForPodConditionReady,
			assertions: func(t *testing.T, clientset *kubernetes.Clientset, config *rest.Config, pods []*v1.Pod) {
				pod := pods[0]
				ctx, cancel := context.WithCancel(context.Background())
				stdoutReader, stdoutWriter := io.Pipe()
				defer stdoutWriter.Close()
				outputChan := make(chan string, 100)
				go func() {
					defer close(outputChan)
					scanner := bufio.NewScanner(stdoutReader)
					for scanner.Scan() {
						line := scanner.Text()
						outputChan <- line
						if strings.Contains(line, "counter-5") {
							cancel() // Cancel the context once the line is found
							return
						}
					}
					if err := scanner.Err(); err != nil {
						t.Errorf("Error scanning output: %v", err)
					}
				}()
				// This blocks until stream is completed - it's when context is cancelled
				err = attachStreamToPod(t, clientset, config, testNamespace, pod.Name,
					&v1.PodAttachOptions{
						Stdin:  false,
						Stdout: true,
						Stderr: true,
						TTY:    false,
					},
					ctx,
					remotecommand.StreamOptions{
						Stdout: stdoutWriter,
						Stderr: stdoutWriter,
						Tty:    false,
					})
				// Collect and validate output
				var capturedOutput []string
				for line := range outputChan {
					capturedOutput = append(capturedOutput, line)
				}

				// Assertions for stdout
				assert.Contains(t, strings.Join(capturedOutput, "\n"), "counter-1")
				assert.Contains(t, strings.Join(capturedOutput, "\n"), "counter-2")
				assert.Contains(t, strings.Join(capturedOutput, "\n"), "counter-3")
				assert.Contains(t, strings.Join(capturedOutput, "\n"), "counter-4")
				assert.Contains(t, strings.Join(capturedOutput, "\n"), "counter-5")
			},
		},
		{
			name:       "TestAttachWithStdinAndTTY",
			podFiles:   []string{"pod7-attach-stdin-auto.yaml"},
			postCreate: waitForPodConditionReady,
			assertions: func(t *testing.T, clientset *kubernetes.Clientset, config *rest.Config, pods []*v1.Pod) {
				pod := pods[0]
				// Use io.Pipe for stdin to control when to close
				stdinReader, stdinWriter := io.Pipe()
				stdout := &bytes.Buffer{}
				ctx := context.Background()

				// Prepare input
				input := "Hello, pod!"
				go func() {
					defer stdinWriter.Close()
					_, err := stdinWriter.Write([]byte(input))
					require.NoError(t, err)
					time.Sleep(200 * time.Millisecond)
					// Close stdinWriter to signal EOF to the remote process
				}()

				// This blocks until stream is completed. In this case when stdin is closed
				err = attachStreamToPod(t, clientset, config, testNamespace, pod.Name,
					&v1.PodAttachOptions{
						Stdin:  true,
						Stdout: true,
						Stderr: false, // Must be false when TTY is true
						TTY:    true,
					},
					ctx,
					remotecommand.StreamOptions{
						Stdin:  stdinReader,
						Stdout: stdout,
						Tty:    true,
					})
				require.NoError(t, err)

				output := stdout.String()
				assert.Contains(t, output, input, "The output should contain the input sent via stdin")
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
