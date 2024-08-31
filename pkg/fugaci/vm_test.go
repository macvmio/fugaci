package fugaci

import (
	"context"
	"errors"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tomekjarosik/fugaci/pkg/curie"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// MockPuller is a mock of the Puller interface.
type MockPuller struct {
	mock.Mock
}

func (m *MockPuller) Pull(ctx context.Context, image string) error {
	args := m.Called(ctx, image)
	return args.Error(0)
}

func createTempScript(content string) (string, error) {
	tempDir, err := os.MkdirTemp("", "temp-script")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %v", err)
	}

	scriptPath := filepath.Join(tempDir, "fakecurie")
	file, err := os.OpenFile(scriptPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return "", fmt.Errorf("failed to create temp script file: %v", err)
	}
	defer file.Close()

	_, err = file.WriteString(content)
	if err != nil {
		return "", fmt.Errorf("failed to write to temp script file: %v", err)
	}

	return scriptPath, nil
}

func setupTestVM(t *testing.T, scriptContent string) (*VM, func()) {
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything).Return(nil)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		}, Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Name: "test-container", Image: "test-image", ImagePullPolicy: v1.PullIfNotPresent},
			},
		}}

	script, err := createTempScript(scriptContent)
	require.NoError(t, err)

	virt := curie.NewVirtualization(script)
	vm, err := NewVM(virt, mockPuller, pod, 0)
	require.NoError(t, err)
	require.NotNil(t, vm)
	return vm, func() { os.Remove(script) }
}

func TestVM_Run_ErrorWhilePulling(t *testing.T) {
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything).Return(errors.New("invalid image"))

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		}, Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Name: "test-container", Image: "test-image", ImagePullPolicy: v1.PullIfNotPresent},
			},
		}}

	script, err := createTempScript("x")
	require.NoError(t, err)

	defer os.Remove(script)

	virt := curie.NewVirtualization(script)
	vm, err := NewVM(virt, mockPuller, pod, 0)
	require.NoError(t, err)
	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "unable to pull image", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "invalid image")
}

func TestVM_Run_CreateContainerFailed_InvalidBinary(t *testing.T) {
	vm, cleanup := setupTestVM(t, "")
	defer cleanup()

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "failed to create container", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "exec format error")
}

func TestVM_Run_CreateContainerFailed_MissingBinary(t *testing.T) {
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything).Return(nil)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		}, Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Name: "test-container", Image: "test-image", ImagePullPolicy: v1.PullIfNotPresent},
			},
		}}

	virt := curie.NewVirtualization("test/123")
	vm, err := NewVM(virt, mockPuller, pod, 0)
	require.NoError(t, err)

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	assert.Equal(t, *status.State.Terminated, v1.ContainerStateTerminated{
		ExitCode: 1,
		Reason:   "failed to create container",
		Message:  "failed to create container: fork/exec test/123: no such file or directory",
	})
}

func TestVM_Run_CreateContainerFailed_Crash(t *testing.T) {
	vm, cleanup := setupTestVM(t, `#!/bin/bash
	exit 13
`)
	defer cleanup()

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	assert.Equal(t, *status.State.Terminated, v1.ContainerStateTerminated{
		ExitCode: 1,
		Reason:   "failed to create container",
		Message:  "failed to create container: exit status 13",
	})
}

func TestVM_Run_StartContainerFailed_Crash(t *testing.T) {
	vm, cleanup := setupTestVM(t, `#!/bin/bash
	if [[ "$1" == "create" ]]; then
		echo "containerid-123"
		exit 0
	fi
	exit 13
`)
	defer cleanup()

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "containerid-123", status.ContainerID)
	assert.Equal(t, int32(1), status.State.Terminated.ExitCode)
	assert.Equal(t, "unable to start process", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "fakecurie start containerid-123' command failed with error 'exit status 13'")
	assert.NotEmpty(t, status.State.Terminated.StartedAt)
}

func TestVM_Run_Successful(t *testing.T) {
	vm, cleanup := setupTestVM(t, `#!/bin/bash
	if [[ "$1" == "create" ]]; then
		echo "containerid-456"
		exit 0
	fi
	exit 0
`)
	defer cleanup()

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "containerid-456", status.ContainerID)
	assert.Equal(t, int32(0), status.State.Terminated.ExitCode)
	assert.Equal(t, "exit status 0", status.State.Terminated.Message)
	assert.Equal(t, "exited successfully", status.State.Terminated.Reason)
	assert.NotEmpty(t, status.State.Terminated.StartedAt)
	assert.NotEmpty(t, status.State.Terminated.FinishedAt)
}

func TestVM_Run_Successful_mustBeReadyWithIPAddress(t *testing.T) {
	vm, cleanup := setupTestVM(t, `#!/bin/bash
	if [[ "$1" == "create" ]]; then
		echo "containerid-456"
		exit 0
	fi
	if [[ "$1" == "start" ]]; then
		
		sleep 1
	fi
	if [[ "$1" == "inspect" ]]; then
		echo '{"arp":[{"IP":"192.168.1.10"}]}'
	fi
	exit 0
`)
	defer cleanup()

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "containerid-456", status.ContainerID)
	assert.Equal(t, int32(0), status.State.Terminated.ExitCode)
	assert.Equal(t, "exit status 0", status.State.Terminated.Message)
	assert.Equal(t, "exited successfully", status.State.Terminated.Reason)
	assert.NotEmpty(t, status.State.Terminated.StartedAt)
	assert.NotEmpty(t, status.State.Terminated.FinishedAt)
}

func TestVM_Run_ProcessIsHanging(t *testing.T) {
	vm, cleanup := setupTestVM(t, `#!/bin/bash
	if [[ "$1" == "create" ]]; then
		echo "containerid-123"
		exit 0
	fi
	if [[ "$1" == "rm" ]]; then
		exit 0
	fi
	sleep 1000000000
`)
	defer cleanup()

	go vm.Run()

	for {
		status := vm.Status()
		if status.State.Running != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check the status while the process is still running
	status := vm.Status()
	require.NotNil(t, status.State.Running, "Container should still be running")
	assert.NotEmpty(t, status.State.Running.StartedAt, "Container should have a start time")
	assert.True(t, status.Ready, "Container should be ready")

	err := vm.Cleanup()
	assert.NoError(t, err)
}

func TestVM_Run_CleanupMustBeGraceful(t *testing.T) {
	vm, cleanup := setupTestVM(t, `#!/bin/bash
		# Function to handle the interrupt signal
		cleanup() {
			echo "Received Interrupt signal, exiting..."
			exit 0
		}
		
		# Simulate some work or a long-running process
		if [[ "$1" == "create" ]]; then
			echo "containerid-123"
			exit 0
		fi
		if [[ "$1" == "start" ]]; then
			trap cleanup EXIT
			trap cleanup SIGINT
			# Simulate a long-running process that would otherwise hang
			sleep 10000 &
			wait $!
			exit 0
		fi
		if [[ "$1" == "rm" ]]; then
			exit 0
		fi
		exit 1
`)
	defer cleanup()

	go vm.Run()

	for {
		status := vm.Status()
		if status.State.Running != nil || status.State.Terminated != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check the status while the process is still running
	status := vm.Status()
	require.NotNil(t, status.State.Running, "Container should still be running")
	assert.NotEmpty(t, status.State.Running.StartedAt, "Container should have a start time")

	err := vm.Cleanup()
	assert.NoError(t, err)
}

func TestVM_Run_Successful_CleanupMustBeIdempotent(t *testing.T) {
	vm, cleanup := setupTestVM(t, `#!/bin/bash
	if [[ "$1" == "create" ]]; then
		echo "containerid-456"
		exit 0
	fi
	exit 0
`)
	defer cleanup()

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "containerid-456", status.ContainerID)
	assert.Equal(t, int32(0), status.State.Terminated.ExitCode)
	assert.Equal(t, "exit status 0", status.State.Terminated.Message)
	assert.Equal(t, "exited successfully", status.State.Terminated.Reason)
	assert.NotEmpty(t, status.State.Terminated.StartedAt)
	assert.NotEmpty(t, status.State.Terminated.FinishedAt)

	err := vm.Cleanup()
	assert.NoError(t, err)
	err = vm.Cleanup()
	assert.NoError(t, err)
}
