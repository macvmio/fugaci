package fugaci

import (
	"context"
	"errors"
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
	return net.ParseIP(args.String(0)), args.Error(1)
}

func (m *MockVirtualization) Exists(ctx context.Context, containerID string) (bool, error) {
	args := m.Called(ctx, containerID)
	return args.Bool(0), args.Error(1)
}

func setupTestVM(t *testing.T, mockVirt *MockVirtualization, mockPuller *MockPuller) (*VM, func()) {
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

	vm, err := NewVM(mockVirt, mockPuller, pod, 0)
	require.NoError(t, err)
	require.NotNil(t, vm)
	return vm, func() {}
}

func TestVM_Run_ErrorWhilePulling(t *testing.T) {
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("invalid image"))

	mockVirt := new(MockVirtualization)

	vm, cleanup := setupTestVM(t, mockVirt, mockPuller)
	defer cleanup()

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "unable to pull image", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "invalid image")
}

func TestVM_Run_CreateContainerFailed_InvalidBinary(t *testing.T) {
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockVirt := new(MockVirtualization)
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("exec format error"))

	vm, cleanup := setupTestVM(t, mockVirt, mockPuller)
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
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockVirt := new(MockVirtualization)
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("no such file or directory"))

	vm, cleanup := setupTestVM(t, mockVirt, mockPuller)
	defer cleanup()

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "failed to create container", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "no such file or directory")
}

func TestVM_Run_CreateContainerFailed_Crash(t *testing.T) {
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockVirt := new(MockVirtualization)
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("exit status 13"))

	vm, cleanup := setupTestVM(t, mockVirt, mockPuller)
	defer cleanup()

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "failed to create container", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "exit status 13")
}

func TestVM_Run_StartContainerFailed_Crash(t *testing.T) {
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockVirt := new(MockVirtualization)
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-123", nil)
	mockVirt.On("Start", mock.Anything, "containerid-123").Return(&exec.Cmd{}, errors.New("exit status 13"))
	mockVirt.On("Destroy", mock.Anything, "containerid-123").Return(nil)

	vm, cleanup := setupTestVM(t, mockVirt, mockPuller)
	defer cleanup()

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
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockVirt := new(MockVirtualization)
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-456", nil)
	cmd := exec.Command("/bin/bash")
	cmd.Start()

	mockVirt.On("Start", mock.Anything, "containerid-456").Return(cmd, nil)
	// Mock the Stop method with a custom matcher for context and the command
	mockVirt.On("Stop", mock.MatchedBy(func(ctx context.Context) bool {
		return true
	}), mock.MatchedBy(func(c *exec.Cmd) bool {
		return c == cmd
	})).Return(nil)
	mockVirt.On("Destroy", mock.Anything, "containerid-456").Return(nil)
	mockVirt.On("IP", mock.Anything).Return(net.IPv4(1, 2, 3, 4))

	vm, cleanup := setupTestVM(t, mockVirt, mockPuller)
	defer cleanup()

	go vm.Run()
	time.Sleep(200 * time.Millisecond)

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "containerid-456", status.ContainerID)
	assert.Equal(t, int32(0), status.State.Terminated.ExitCode)
	assert.Equal(t, "exit status 0", status.State.Terminated.Message)
	assert.Equal(t, "exited successfully", status.State.Terminated.Reason)
	assert.NotEmpty(t, status.State.Terminated.StartedAt)
	assert.NotEmpty(t, status.State.Terminated.FinishedAt)

	// Assert that Stop and Remove (previously Destroy) were called
	//mockVirt.AssertCalled(t, "Stop", mock.Anything, cmd)
	mockVirt.AssertCalled(t, "Destroy", mock.Anything, "containerid-456")
}

func TestVM_Run_Successful_mustBeReadyWithIPAddress(t *testing.T) {
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockVirt := new(MockVirtualization)
	cmd := exec.Command("/usr/bin/sleep", "0.3")
	cmd.Start()
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-456", nil)
	mockVirt.On("Start", mock.Anything, "containerid-456").Return(cmd, nil)
	mockVirt.On("IP", mock.Anything, "containerid-456").Return("192.168.1.10", nil)
	mockVirt.On("Destroy", mock.Anything, "containerid-456").Return(nil)

	vm, cleanup := setupTestVM(t, mockVirt, mockPuller)
	defer cleanup()

	go vm.Run()

	for {
		status := vm.Status()
		if !status.Ready {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		assert.True(t, status.Ready)
		assert.Equal(t, "192.168.1.10", vm.GetPod().Status.PodIP)
		break
	}

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
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockVirt := new(MockVirtualization)
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-123", nil)
	cmd := exec.Command("/usr/bin/sleep", "1")
	cmd.Start()
	mockVirt.On("Start", mock.Anything, "containerid-123").Return(cmd, nil)
	mockVirt.On("Stop", mock.MatchedBy(func(ctx context.Context) bool {
		return true
	}), mock.MatchedBy(func(c *exec.Cmd) bool {
		return c == cmd
	})).Return(nil)
	mockVirt.On("IP", mock.Anything).Return(nil, errors.New("IP not found"))

	vm, cleanup := setupTestVM(t, mockVirt, mockPuller)
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
	assert.False(t, status.Ready, "Container should be ready")

	err := vm.Cleanup()
	assert.NoError(t, err)
}

func TestVM_Run_CleanupMustBeGraceful(t *testing.T) {
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockVirt := new(MockVirtualization)
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-123", nil)
	cmd := exec.Command("/usr/bin/sleep", "0.3")
	cmd.Start()
	mockVirt.On("Start", mock.Anything, "containerid-123").Return(cmd, nil)
	mockVirt.On("Stop", mock.MatchedBy(func(ctx context.Context) bool {
		return true
	}), mock.MatchedBy(func(c *exec.Cmd) bool {
		return c == cmd
	})).Return(nil)
	mockVirt.On("Destroy", mock.Anything, "containerid-123").Return(nil)
	mockVirt.On("IP", mock.Anything).Return(nil, errors.New("IP not found"))

	vm, cleanup := setupTestVM(t, mockVirt, mockPuller)
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
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockVirt := new(MockVirtualization)
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-456", nil)
	cmd := exec.Command("/usr/bin/sleep", "0.3")
	cmd.Start()
	mockVirt.On("Start", mock.Anything, "containerid-456").Return(cmd, nil)
	mockVirt.On("Stop", mock.MatchedBy(func(ctx context.Context) bool {
		return true
	}), mock.MatchedBy(func(c *exec.Cmd) bool {
		return c == cmd
	})).Return(nil)
	mockVirt.On("Destroy", mock.Anything, "containerid-456").Return(nil)
	mockVirt.On("IP", mock.Anything, "containerid-456").Return("", errors.New("no IP found"))

	vm, cleanup := setupTestVM(t, mockVirt, mockPuller)
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
