package fugaci

import (
	"context"
	"errors"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net"
	"os/exec"
	"testing"
	"time"
)

// MockPuller is a mock of the Puller interface.
type MockPuller struct {
	mock.Mock
}

func (m *MockPuller) Pull(ctx context.Context, image string, pullPolicy v1.PullPolicy) error {
	args := m.Called(ctx, image, pullPolicy)
	return args.Error(0)
}

// MockVirtualization is a mock of the Virtualization interface.
type MockVirtualization struct {
	mock.Mock
}

func (m *MockVirtualization) Create(ctx context.Context, pod v1.Pod, containerIndex int) (containerID string, err error) {
	args := m.Called(ctx, pod, containerIndex)
	return args.String(0), args.Error(1)
}

func (m *MockVirtualization) Start(ctx context.Context, containerID string) (*exec.Cmd, error) {
	args := m.Called(ctx, containerID)
	return args.Get(0).(*exec.Cmd), args.Error(1)
}

func (m *MockVirtualization) Stop(ctx context.Context, containerRunCmd *exec.Cmd) error {
	args := m.Called(ctx, containerRunCmd)
	return args.Error(0)
}

func (m *MockVirtualization) Destroy(ctx context.Context, containerID string) error {
	args := m.Called(ctx, containerID)
	return args.Error(0)
}

func (m *MockVirtualization) IP(ctx context.Context, containerID string) (net.IP, error) {
	args := m.Called(ctx, containerID)
	return args.Get(0).(net.IP), args.Error(1)
}

func (m *MockVirtualization) Exists(ctx context.Context, containerID string) (bool, error) {
	args := m.Called(ctx, containerID)
	return args.Bool(0), args.Error(1)
}

func noPodOverride(pod *v1.Pod) {
}

// setupCommonTestVM initializes the VM and sets up common mock behavior.
func setupCommonTestVM(t *testing.T, podOverride func(*v1.Pod)) (*VM, *MockVirtualization, *MockPuller, func()) {
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockVirt := new(MockVirtualization)
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-123", nil)
	mockVirt.On("Stop", mock.Anything, mock.Anything).Return(nil)
	mockVirt.On("Destroy", mock.Anything, "containerid-123").Return(nil)
	mockVirt.On("IP", mock.Anything, "containerid-123").Return(net.IPv4(1, 2, 3, 4), nil)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Name: "test-container", Image: "test-image", ImagePullPolicy: v1.PullIfNotPresent},
			},
		},
	}
	podOverride(pod)

	vm, err := NewVM(mockVirt, mockPuller, pod, 0)
	require.NoError(t, err)
	require.NotNil(t, vm)

	cleanup := func() {
		// TODO:
	}

	return vm, mockVirt, mockPuller, cleanup
}

func TestVM_Run_ErrorWhilePulling(t *testing.T) {
	vm, _, mockPuller, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Unset()
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("invalid image"))

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "unable to pull image", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "invalid image")
}

func TestVM_Run_CreateContainerFailed_InvalidBinary(t *testing.T) {
	vm, mockVirt, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Unset()
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("exec format error"))

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "failed to create container", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "exec format error")
}

func TestVM_Run_CreateContainerFailed_MissingBinary(t *testing.T) {
	vm, mockVirt, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Unset()
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("no such file or directory"))

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "failed to create container", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "no such file or directory")
}

func TestVM_Run_CreateContainerFailed_Crash(t *testing.T) {
	vm, mockVirt, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Unset()
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("exit status 13"))

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "failed to create container", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "exit status 13")
}

func TestVM_Run_StartContainerFailed_Crash(t *testing.T) {
	vm, mockVirt, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	mockVirt.On("Start", mock.Anything, "containerid-123").Unset()
	mockVirt.On("Start", mock.Anything, "containerid-123").Return(&exec.Cmd{}, errors.New("exit status 13"))

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "containerid-123", status.ContainerID)
	assert.Equal(t, int32(1), status.State.Terminated.ExitCode)
	assert.Equal(t, "unable to start process", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "exit status 13")
	assert.NotEmpty(t, status.State.Terminated.FinishedAt)
}

func TestVM_Run_Successful(t *testing.T) {
	vm, mockVirt, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Unset()
	mockVirt.On("Start", mock.Anything, "containerid-123").Unset()

	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-456", nil)
	cmd := exec.Command("/bin/bash")
	cmd.Start()
	mockVirt.On("Start", mock.Anything, "containerid-456").Return(cmd, nil)

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, int32(0), status.RestartCount)
	assert.Equal(t, "containerid-456", status.ContainerID)
	assert.Equal(t, int32(0), status.State.Terminated.ExitCode)
	assert.Equal(t, "exit status 0", status.State.Terminated.Message)
	assert.Equal(t, "exited successfully", status.State.Terminated.Reason)
	assert.NotEmpty(t, status.State.Terminated.StartedAt)
	assert.NotEmpty(t, status.State.Terminated.FinishedAt)
}

func TestVM_Run_Successful_ifContainerIDProvided_mustRestart(t *testing.T) {
	vm, mockVirt, _, cleanup := setupCommonTestVM(t, func(pod *v1.Pod) {
		pod.Status.ContainerStatuses = []v1.ContainerStatus{{
			ContainerID: "containerid-123",
		}}
	})
	defer cleanup()

	mockVirt.On("Start", mock.Anything, "containerid-123").Unset()
	cmd := exec.Command("/bin/bash")
	cmd.Start()
	mockVirt.On("Start", mock.Anything, "containerid-123").Return(cmd, nil)

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, int32(1), status.RestartCount)

	assert.Equal(t, "containerid-123", status.ContainerID)
	assert.Equal(t, int32(0), status.State.Terminated.ExitCode)
	assert.Equal(t, "exit status 0", status.State.Terminated.Message)
	assert.Equal(t, "exited successfully", status.State.Terminated.Reason)
	assert.NotEmpty(t, status.State.Terminated.StartedAt)
	assert.NotEmpty(t, status.State.Terminated.FinishedAt)
}

func TestVM_Run_Successful_mustBeReadyWithIPAddress(t *testing.T) {
	vm, mockVirt, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Unset()
	mockVirt.On("Start", mock.Anything, "containerid-123").Unset()

	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-123", nil)
	cmd := exec.Command("/usr/bin/sleep", "0.3")
	cmd.Start()
	mockVirt.On("Start", mock.Anything, "containerid-123").Return(cmd, nil)

	go vm.Run()

	for {
		status := vm.Status()
		if !status.Ready {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		assert.True(t, status.Ready)
		assert.True(t, *status.Started)
		assert.Equal(t, "1.2.3.4", vm.GetPod().Status.PodIP)
		assert.Equal(t, v1.PodRunning, vm.GetPod().Status.Phase)
		break
	}

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)

	assert.Equal(t, "containerid-123", status.ContainerID)
	assert.Equal(t, int32(0), status.State.Terminated.ExitCode)
	assert.Equal(t, "exit status 0", status.State.Terminated.Message)
	assert.Equal(t, "exited successfully", status.State.Terminated.Reason)
	assert.NotEmpty(t, status.State.Terminated.StartedAt)
	assert.NotEmpty(t, status.State.Terminated.FinishedAt)
}

func TestVM_Run_ProcessIsHanging(t *testing.T) {
	vm, mockVirt, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	mockVirt.On("IP", mock.Anything).Unset()
	mockVirt.On("IP", mock.Anything).Return(nil, errors.New("IP not found"))
	mockVirt.On("Stop", mock.Anything, mock.Anything, mock.Anything).Unset()

	cmd := exec.Command("/usr/bin/sleep", "30")
	cmd.Start()
	mockVirt.On("Start", mock.Anything, "containerid-123").Return(cmd, nil)
	mockVirt.On("Stop", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("unable to stop VM"))

	go vm.Run()

	for {
		status := vm.Status()
		if status.State.Running != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	status := vm.Status()
	require.NotNil(t, status.State.Running, "Container should still be running")
	assert.NotEmpty(t, status.State.Running.StartedAt, "Container should have a start time")
	assert.False(t, status.Ready, "Container should not be ready")

	err := vm.Cleanup()
	assert.NoError(t, err)

	for {
		status := vm.Status()
		if status.State.Terminated != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	status = vm.Status()
	require.NotNil(t, status.State.Terminated, "Container should still be running")
	assert.NotEmpty(t, status.State.Terminated.StartedAt, "Container should have a start time")
	assert.Equal(t, "error while running container", status.State.Terminated.Reason)
	assert.Equal(t, "'/usr/bin/sleep 30' command failed: signal: killed", status.State.Terminated.Message)
	assert.False(t, status.Ready, "Container should not be ready")
}

func TestVM_Run_Success_CleanupMustBeGraceful(t *testing.T) {
	vm, mockVirt, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	mockVirt.On("Start", mock.Anything, "containerid-123").Unset()
	cmd := exec.Command("/usr/bin/sleep", "0.1")
	cmd.Start()
	mockVirt.On("Start", mock.Anything, "containerid-123").Return(cmd, nil)

	go vm.Run()

	for {
		status := vm.Status()
		if status.State.Running != nil || status.State.Terminated != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	status := vm.Status()
	require.NotNil(t, status.State.Running, "Container should still be running")
	assert.NotEmpty(t, status.State.Running.StartedAt, "Container should have a start time")

	err := vm.Cleanup()
	assert.NoError(t, err)
}

func TestVM_Run_Successful_CleanupMustBeIdempotent(t *testing.T) {
	vm, mockVirt, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	cmd := exec.Command("/usr/bin/sleep", "0.1")
	cmd.Start()
	mockVirt.On("Start", mock.Anything, "containerid-123").Return(cmd, nil)

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "containerid-123", status.ContainerID)
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

func TestVM_Cleanup_CalledWhilePulling_mustExitQuickly(t *testing.T) {
	vm, _, mockPuller, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	mockPuller.On("Create", mock.Anything, mock.Anything, mock.Anything).Unset()
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Unset()
	// Simulate a Pull function that blocks, waiting on the context
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		// Extract the context from the arguments
		ctx := args.Get(0).(context.Context)

		// Wait until the context is canceled or done
		fmt.Printf("simulating that pull is waiting on context cancellation\n")
		<-ctx.Done()
		fmt.Printf("simulating that pull is waiting on context cancellation: finished\n")
	}).Return(errors.New("context cancelled"))

	go vm.Run()
	time.Sleep(20 * time.Millisecond)

	err := vm.Cleanup()
	assert.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "unable to pull image", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "context cancelled")
}
