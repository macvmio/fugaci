package fugaci

import (
	"context"
	"errors"
	"fmt"
	regv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/macvmio/fugaci/pkg/sshrunner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net"
	"os/exec"
	"slices"
	"testing"
	"time"
)

// MockPuller is a mock of the Puller interface.
type MockPuller struct {
	mock.Mock
}

func (m *MockPuller) Pull(ctx context.Context, image string, pullPolicy v1.PullPolicy, cb func(st v1.ContainerStateWaiting)) (regv1.Hash, *regv1.Manifest, error) {
	args := m.Called(ctx, image, pullPolicy)
	return args.Get(0).(regv1.Hash), args.Get(1).(*regv1.Manifest), args.Error(2)
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

func (m *MockVirtualization) Stop(ctx context.Context, containerPID int) error {
	args := m.Called(ctx, containerPID)
	return args.Error(0)
}

func (m *MockVirtualization) Destroy(ctx context.Context, containerID string) error {
	args := m.Called(ctx, containerID)
	return args.Error(0)
}

func (m *MockVirtualization) IP(ctx context.Context, containerID string) (net.IP, error) {
	args := m.Called(ctx, containerID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(net.IP), args.Error(1)
}

func (m *MockVirtualization) Exists(ctx context.Context, containerID string) (bool, error) {
	args := m.Called(ctx, containerID)
	return args.Bool(0), args.Error(1)
}

type MockSSHRunner struct {
	mock.Mock
}

func (m *MockSSHRunner) Run(ctx context.Context, dialInfo sshrunner.DialInfo, cmd []string, opts ...sshrunner.Option) error {
	args := m.Called(ctx, dialInfo, cmd, opts)
	return args.Error(0)
}

func noPodOverride(pod *v1.Pod) {
}

func setupCommonMockVirt(mockPuller *MockPuller, mockVirt *MockVirtualization, mockSSHRunner *MockSSHRunner, methods []string) {
	if slices.Contains(methods, "Pull") {

		h, _ := regv1.NewHash("sha256:123")
		mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(
			h,                      // Return a valid regv1.Hash
			(*regv1.Manifest)(nil), // Return a nil Manifest
			nil,                    // Return a nil error
		)
	}
	if slices.Contains(methods, "Create") {
		mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-123", nil)
	}
	if slices.Contains(methods, "Stop") {
		mockVirt.On("Stop", mock.Anything, mock.Anything).Return(nil)
	}
	if slices.Contains(methods, "Destroy") {
		mockVirt.On("Destroy", mock.Anything, "containerid-123").Return(nil)
	}
	if slices.Contains(methods, "IP") {
		mockVirt.On("IP", mock.Anything, "containerid-123").Return(net.IPv4(1, 2, 3, 4), nil)
	}
	if slices.Contains(methods, "Run") {
		mockSSHRunner.On("Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	}
}

// setupCommonTestVM initializes the VM and sets up common mock behavior.
func setupCommonTestVM(t *testing.T, podOverride func(*v1.Pod)) (*VM, *MockVirtualization, *MockPuller, *MockSSHRunner, func()) {
	mockPuller := new(MockPuller)
	mockVirt := new(MockVirtualization)
	mockSSHRunner := new(MockSSHRunner)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "testnamespace",
			Name:      "test-pod",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:            "test-container",
					Image:           "test-image",
					ImagePullPolicy: v1.PullIfNotPresent,
					Env: []v1.EnvVar{
						{Name: SshUsernameEnvVar, Value: "testuser"},
						{Name: SshPasswordEnvVar, Value: "testpassword"},
					},
					Command: []string{"sh", "-c", "test"},
				},
			},
		},
	}
	podOverride(pod)

	vm, err := NewVM(context.Background(), mockVirt, mockPuller, mockSSHRunner, pod, 0)
	require.NoError(t, err)
	require.NotNil(t, vm)

	cleanup := func() {
		// TODO:
	}

	return vm, mockVirt, mockPuller, mockSSHRunner, cleanup
}

func TestVM_Run_ErrorWhilePulling(t *testing.T) {
	vm, _, mockPuller, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	h, _ := regv1.NewHash("sha256:123")
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(h, (*regv1.Manifest)(nil), errors.New("invalid image"))

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "unable to pull image", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "invalid image")
	mockPuller.AssertExpectations(t)
}

func TestVM_Run_CreateContainerFailed_InvalidBinary(t *testing.T) {
	vm, mockVirt, mockPuller, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	setupCommonMockVirt(mockPuller, nil, nil, []string{"Pull"})
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("exec format error"))

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "failed to create container", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "exec format error")
	mockPuller.AssertExpectations(t)
	mockVirt.AssertExpectations(t)
}

func TestVM_Run_CreateContainerFailed_MissingBinary(t *testing.T) {
	vm, mockVirt, mockPuller, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	setupCommonMockVirt(mockPuller, nil, nil, []string{"Pull"})
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("no such file or directory"))

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "failed to create container", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "no such file or directory")
	mockPuller.AssertExpectations(t)
	mockVirt.AssertExpectations(t)
}

func TestVM_Run_CreateContainerFailed_Crash(t *testing.T) {
	vm, mockVirt, mockPuller, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	setupCommonMockVirt(mockPuller, nil, nil, []string{"Pull"})
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("exit status 13"))

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "failed to create container", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "exit status 13")
	mockPuller.AssertExpectations(t)
	mockVirt.AssertExpectations(t)
}

func TestVM_Run_StartContainerFailed_Crash(t *testing.T) {
	vm, mockVirt, mockPuller, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	setupCommonMockVirt(mockPuller, mockVirt, nil, []string{"Pull", "Create", "Destroy"})
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

	mockPuller.AssertExpectations(t)
	mockVirt.AssertExpectations(t)
}

func TestVM_Run_Successful(t *testing.T) {
	vm, mockVirt, mockPuller, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	setupCommonMockVirt(mockPuller, mockVirt, nil, []string{"Pull"})
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

	mockPuller.AssertExpectations(t)
	mockVirt.AssertExpectations(t)
}

func TestVM_Run_Successful_ifContainerIDProvided_mustRestart(t *testing.T) {
	vm, mockVirt, mockPuller, _, cleanup := setupCommonTestVM(t, func(pod *v1.Pod) {
		pod.Status.ContainerStatuses = []v1.ContainerStatus{{
			ContainerID: "containerid-123",
		}}
	})
	defer cleanup()

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

	mockPuller.AssertExpectations(t)
	mockVirt.AssertExpectations(t)
}

func TestVM_Run_Successful_mustBeReadyWithIPAddress(t *testing.T) {
	vm, mockVirt, mockPuller, mockSSHRunner, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()
	setupCommonMockVirt(mockPuller, mockVirt, mockSSHRunner, []string{"Pull", "IP", "Run"})
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-123", nil)
	cmd := exec.Command("/bin/sleep", "0.3")
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

	mockPuller.AssertExpectations(t)
	mockVirt.AssertExpectations(t)
}

func TestVM_Run_Successful_mustRunContainerCommandThroughSSH(t *testing.T) {
	vm, mockVirt, mockPuller, mockSSHRunner, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	setupCommonMockVirt(mockPuller, mockVirt, mockSSHRunner, []string{"Pull", "IP", "Run"})
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-123", nil)
	cmd := exec.Command("/bin/sleep", "0.3")
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
		lastCall := mockSSHRunner.Calls[len(mockSSHRunner.Calls)-1]
		assert.Equal(t, "Run", lastCall.Method)
		assert.Equal(t, sshrunner.DialInfo{Address: "1.2.3.4:22", Username: "testuser", Password: "testpassword"}, lastCall.Arguments.Get(1)) // SSH address
		assert.Equal(t, []string{"sh", "-c", "test"}, lastCall.Arguments.Get(2))                                                              // Command
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

	mockPuller.AssertExpectations(t)
	mockVirt.AssertExpectations(t)
}

func TestVM_Run_ProcessIsHanging(t *testing.T) {
	vm, mockVirt, mockPuller, mockSSHRunner, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	setupCommonMockVirt(mockPuller, mockVirt, mockSSHRunner, []string{"Pull", "Create", "Run", "Destroy"})
	mockVirt.On("IP", mock.Anything, mock.Anything).Return(nil, errors.New("IP not found"))

	cmd := exec.Command("/bin/sleep", "30")
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
	assert.Equal(t, "error from runCmd.Wait()", status.State.Terminated.Reason)
	assert.Equal(t, "'/bin/sleep 30' command failed: signal: killed", status.State.Terminated.Message)
	assert.False(t, status.Ready, "Container should not be ready")
}

func TestVM_Run_Success_CleanupMustBeGraceful(t *testing.T) {
	vm, mockVirt, mockPuller, mockSSHRunner, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	setupCommonMockVirt(mockPuller, mockVirt, mockSSHRunner, []string{"Pull", "Create", "Run", "Destroy", "Stop"})
	cmd := exec.Command("/bin/sleep", "0.1")
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

	mockPuller.AssertExpectations(t)
	mockVirt.AssertExpectations(t)
}

func TestVM_Run_Successful_CleanupMustBeIdempotent(t *testing.T) {
	vm, mockVirt, mockPuller, mockSSHRunner, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	setupCommonMockVirt(mockPuller, mockVirt, mockSSHRunner, []string{"Pull", "Create", "Run", "IP", "Destroy", "Stop"})
	cmd := exec.Command("/bin/sleep", "0.2")
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

	mockPuller.AssertExpectations(t)
	mockVirt.AssertExpectations(t)
}

func TestVM_Cleanup_CalledWhilePulling_mustExitQuickly(t *testing.T) {
	vm, mockVirt, mockPuller, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	h, _ := regv1.NewHash("sha256:123")
	setupCommonMockVirt(mockPuller, mockVirt, nil, []string{"Stop"})
	// Simulate a Pull function that blocks, waiting on the context
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		// Extract the context from the arguments
		ctx := args.Get(0).(context.Context)

		// Wait until the context is canceled or done
		fmt.Printf("simulating that pull is waiting on context cancellation\n")
		<-ctx.Done()
		fmt.Printf("simulating that pull is waiting on context cancellation: finished\n")
	}).Return(h, (*regv1.Manifest)(nil), errors.New("context cancelled"))

	go vm.Run()
	time.Sleep(20 * time.Millisecond)

	err := vm.Cleanup()
	assert.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "unable to pull image", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "context cancelled")

	mockPuller.AssertExpectations(t)
	mockVirt.AssertExpectations(t)
}

func TestVM_RunCommand(t *testing.T) {

	t.Run("pod ready with SSH session running", func(t *testing.T) {
		vm, _, _, mockSSHRunner, _ := setupCommonTestVM(t, func(pod *v1.Pod) {
			pod.Spec.Containers = []v1.Container{
				{Name: "test123", Env: []v1.EnvVar{
					{Name: SshUsernameEnvVar, Value: "u"},
					{Name: SshPasswordEnvVar, Value: "p"},
				}},
			}
		})
		setupCommonMockVirt(nil, nil, mockSSHRunner, []string{"Run"})
		vm.safeUpdatePodIP(net.IP{1, 2, 3, 4})
		err := vm.RunCommand(context.Background(), []string{"echo", "123"})
		assert.NoError(t, err)
		mockSSHRunner.AssertCalled(t, "Run",
			mock.Anything,
			sshrunner.DialInfo{
				Address: "1.2.3.4:22", Username: "u", Password: "p"},
			[]string{"echo", "123"},
			mock.Anything)
	})
}

func TestVM_WaitForIP(t *testing.T) {

	t.Run("Success", func(t *testing.T) {
		vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
		defer cleanup()
		// Mock a successful IP retrieval after a few ticks
		mockVirt.On("IP", mock.Anything, "containerid-123").Return(nil, errors.New("no IP yet")).Once()
		mockVirt.On("IP", mock.Anything, "containerid-123").Return(net.IPv4(1, 2, 3, 4), nil)

		ip := vm.waitForIP(context.Background(), "containerid-123")
		require.NotNil(t, ip)
		assert.Equal(t, "1.2.3.4", ip.String())
		mockVirt.AssertExpectations(t)
	})

	t.Run("Timeout", func(t *testing.T) {
		vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
		defer cleanup()
		// Mock the case where IP never becomes available
		mockVirt.On("IP", mock.Anything, "containerid-123").Return(nil, errors.New("no IP yet"))

		// Use a short timeout to test timeout behavior
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		ip := vm.waitForIP(ctx, "containerid-123")
		assert.Nil(t, ip)
		mockVirt.AssertExpectations(t)
	})

	t.Run("Cancellation", func(t *testing.T) {
		vm, _, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
		defer cleanup()
		// Cancel the context immediately to simulate cancellation
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		ip := vm.waitForIP(ctx, "containerid-123")
		assert.Nil(t, ip)
	})

	t.Run("MultipleErrorsBeforeSuccess", func(t *testing.T) {
		vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
		defer cleanup()
		// Simulate errors returned for the first few calls, then success
		mockVirt.On("IP", mock.Anything, "containerid-123").Return(nil, errors.New("no IP yet")).Twice()
		mockVirt.On("IP", mock.Anything, "containerid-123").Return(net.IPv4(1, 2, 3, 4), nil)

		ip := vm.waitForIP(context.Background(), "containerid-123")
		require.NotNil(t, ip)
		assert.Equal(t, "1.2.3.4", ip.String())
		mockVirt.AssertExpectations(t)
	})
}

func TestVM_WaitAndRunCommandInside(t *testing.T) {

	t.Run("Successful execution", func(t *testing.T) {
		vm, mockVirt, _, mockSSHRunner, cleanup := setupCommonTestVM(t, noPodOverride)
		defer cleanup()
		// Mock IP retrieval
		mockVirt.On("IP", mock.Anything, "containerid-123").Return(net.IPv4(2, 3, 4, 5), nil)

		// Mock successful SSH command execution
		mockSSHRunner.On("Run", mock.Anything,
			sshrunner.DialInfo{Address: "2.3.4.5:22", Username: "testuser", Password: "testpassword"},
			mock.Anything, mock.Anything).Return(nil)

		startTime := time.Now()

		vm.waitAndRunCommandInside(context.Background(), startTime, "containerid-123")

		// Check if the pod IP was updated and status was set to Ready
		assert.Equal(t, "2.3.4.5", vm.GetPod().Status.PodIP)
		assert.True(t, vm.Status().Ready)

		// Ensure SSH command was executed
		mockSSHRunner.AssertCalled(t, "Run", mock.Anything, mock.Anything, []string{"echo", "hello"}, mock.Anything)
		mockVirt.AssertExpectations(t)
	})

	t.Run("Failure to retrieve IP", func(t *testing.T) {
		vm, mockVirt, _, mockSSHRunner, cleanup := setupCommonTestVM(t, noPodOverride)
		defer cleanup()
		// Mock failure to retrieve IP (nil return)
		mockVirt.On("IP", mock.Anything, "containerid-123").Return(nil, errors.New("no IP"))

		startTime := time.Now()
		ctx, cancelFunc := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancelFunc()
		vm.waitAndRunCommandInside(ctx, startTime, "containerid-123")

		// Since IP is nil, the function should exit early and not proceed further
		assert.Equal(t, "", vm.GetPod().Status.PodIP) // No IP should be set
		assert.False(t, vm.Status().Ready)

		mockVirt.AssertExpectations(t)
		mockSSHRunner.AssertNotCalled(t, "Run", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("SSH not ready", func(t *testing.T) {
		vm, mockVirt, _, mockSSHRunner, cleanup := setupCommonTestVM(t, noPodOverride)
		defer cleanup()
		// Mock successful IP retrieval
		mockVirt.On("IP", mock.Anything, "containerid-123").Return(net.IPv4(1, 9, 13, 64), nil)

		// Mock SSH command failing repeatedly
		mockSSHRunner.On("Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("SSH not ready"))

		startTime := time.Now()
		ctx, cancelFunc := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelFunc()

		vm.waitAndRunCommandInside(ctx, startTime, "containerid-123")

		// Check that after retries, the status is not Ready
		assert.False(t, vm.Status().Ready)

		// Ensure multiple SSH attempts were made
		assert.GreaterOrEqual(t, len(mockSSHRunner.Calls), 1)
		//mockVirt.AssertExpectations(t)
		//mockSSHRunner.AssertExpectations(t)
	})
}
