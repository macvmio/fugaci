package fugaci_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testNamespace = "default"

func createPodFromYAML(clientset *kubernetes.Clientset, fileName string) (*v1.Pod, error) {
	podYAML, err := os.ReadFile(filepath.Join("testdata", fileName))
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML file: %w", err)
	}

	// Use the Kubernetes scheme to decode the YAML
	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDeserializer()
	obj, _, err := decoder.Decode(podYAML, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decode YAML: %w", err)
	}

	// Assert that the object is a Pod
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return nil, fmt.Errorf("decoded object is not a Pod")
	}

	pod.Name = generateRandomName(pod.Name)
	pod.Namespace = testNamespace
	return clientset.CoreV1().Pods(testNamespace).Create(context.TODO(), pod, metav1.CreateOptions{})
}

func deletePod(clientset *kubernetes.Clientset, podName string) {
	clientset.CoreV1().Pods(testNamespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
}

func waitForPodPhase(clientset *kubernetes.Clientset, namespace, podName string, desiredPhase v1.PodPhase, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.Background(), 1*time.Second, timeout, true, func(ctx context.Context) (done bool, err error) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return pod.Status.Phase == desiredPhase, nil
	})
}

func waitForPodPhaseRunning(clientset *kubernetes.Clientset, namespace, podName string, timeout time.Duration) error {
	return waitForPodPhase(clientset, namespace, podName, v1.PodRunning, timeout)
}

func waitForPodPhaseFailed(clientset *kubernetes.Clientset, namespace, podName string, timeout time.Duration) error {
	return waitForPodPhase(clientset, namespace, podName, v1.PodFailed, timeout)
}

func waitForPodPhaseSucceeded(clientset *kubernetes.Clientset, namespace, podName string, timeout time.Duration) error {
	return waitForPodPhase(clientset, namespace, podName, v1.PodSucceeded, timeout)
}

func waitForPodConditionReady(clientset *kubernetes.Clientset, namespace, podName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.Background(), 1*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return isPodReady(pod), nil
	})
}

// Helper function for common assertions
func assertPodHasLabel(t *testing.T, pod *v1.Pod, key, expectedValue string) {
	value, exists := pod.Labels[key]
	assert.True(t, exists, "Pod %s should have label %s", pod.Name, key)
	assert.Equal(t, expectedValue, value, "Pod %s label %s should be %s", pod.Name, key, expectedValue)
}

func getContainerLogs(t *testing.T, clientset *kubernetes.Clientset, namespace, podName, containerName string) string {
	t.Helper()
	podLogOpts := v1.PodLogOptions{Container: containerName}
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &podLogOpts)
	podLogs, err := req.Stream(context.TODO())
	require.NoError(t, err, "error opening log stream for pod %s", podName)
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	require.NoError(t, err, "error copying logs")
	return buf.String()
}

func execCommandInPod(t *testing.T, clientset *kubernetes.Clientset, config *rest.Config, namespace, podName, containerName string, command []string) (string, string) {
	t.Helper()
	req := clientset.CoreV1().RESTClient().
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&v1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	require.NoError(t, err, "error creating executor")
	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	require.NoError(t, err, "error executing command: %v", command)
	return stdout.String(), stderr.String()
}
func attachStreamToPod(t *testing.T, clientset *kubernetes.Clientset, config *rest.Config, namespace, podName string,
	attachOptions *v1.PodAttachOptions, ctx context.Context, streamOptions remotecommand.StreamOptions) error {
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("attach").
		VersionedParams(attachOptions, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, http.MethodPost, req.URL())
	require.NoError(t, err)
	return exec.StreamWithContext(ctx, streamOptions)
}

func generateRandomName(baseName string) string {
	b := make([]byte, 4) // 4 bytes will give us 8 hex characters
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate random bytes")
	}
	return fmt.Sprintf("%s-%s", baseName, hex.EncodeToString(b))
}

func isPodReady(pod *v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady {
			return condition.Status == v1.ConditionTrue
		}
	}
	return false
}
