package fugaci

import (
	"context"
	"errors"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tomekjarosik/fugaci/pkg/sshrunner"
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

func (m *MockPuller) Pull(ctx context.Context, image string, pullPolicy v1.PullPolicy, cb func(st v1.ContainerStateWaiting)) (imageID string, err error) {
	args := m.Called(ctx, image, pullPolicy)
	return args.Get(0).(string), args.Error(1)
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

// setupCommonTestVM initializes the VM and sets up common mock behavior.
func setupCommonTestVM(t *testing.T, podOverride func(*v1.Pod)) (*VM, *MockVirtualization, *MockPuller, *MockSSHRunner, func()) {
	mockPuller := new(MockPuller)
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("img1@sha256:123", nil)

	mockVirt := new(MockVirtualization)
	mockVirt.On("Create", mock.Anything, mock.Anything, mock.Anything).Return("containerid-123", nil)
	mockVirt.On("Stop", mock.Anything, mock.Anything).Return(nil)
	mockVirt.On("Destroy", mock.Anything, "containerid-123").Return(nil)
	mockVirt.On("IP", mock.Anything, "containerid-123").Return(net.IPv4(1, 2, 3, 4), nil)

	mockSSHRunner := new(MockSSHRunner)
	mockSSHRunner.On("Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

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

	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Unset()
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("invalid image"))

	go vm.Run()

	<-vm.LifetimeContext().Done()

	status := vm.Status()
	require.NotNil(t, status.State.Terminated)
	assert.Equal(t, "unable to pull image", status.State.Terminated.Reason)
	assert.Contains(t, status.State.Terminated.Message, "invalid image")
}

func TestVM_Run_CreateContainerFailed_InvalidBinary(t *testing.T) {
	vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
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
	vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
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
	vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
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
	vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
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
	vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
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
	vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, func(pod *v1.Pod) {
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
	vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
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

func TestVM_Run_Successful_mustRunContainerCommandThroughSSH(t *testing.T) {
	vm, mockVirt, _, mockSSHRunner, cleanup := setupCommonTestVM(t, noPodOverride)
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
}

func TestVM_Run_ProcessIsHanging(t *testing.T) {
	vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
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
	vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
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
	vm, mockVirt, _, _, cleanup := setupCommonTestVM(t, noPodOverride)
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
	vm, _, mockPuller, _, cleanup := setupCommonTestVM(t, noPodOverride)
	defer cleanup()

	mockPuller.On("Create", mock.Anything, mock.Anything, mock.Anything).Unset()
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Unset()
	// Simulate a Pull function that blocks, waiting on the context
	mockPuller.On("Pull", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		// Extract the context from the arguments
		ctx := args.Get(0).(context.Context)

		// Wait until the context is canceled or done
		fmt.Printf("simulating that pull is waiting on context cancellation\n")
		<-ctx.Done()
		fmt.Printf("simulating that pull is waiting on context cancellation: finished\n")
	}).Return("", errors.New("context cancelled"))

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

/*
func TestVM_Env(t *testing.T) {
	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Env: []v1.EnvVar{
						{Name: SshUsernameEnvVar, Value: "user1"},
						{Name: SshPasswordEnvVar, Value: "pass1"},
						{Name: "OTHER_ENV_VAR", Value: ""},
					},
				},
			},
		},
	}

	vm := NewVM()

	envVars := vm.env()

	assert.Equal(t, "user1", envVars[SshUsernameEnvVar], "Expected FUGACI_SSH_USERNAME_ENVVAR to be 'user1'")
	assert.Equal(t, "pass1", envVars[SshPasswordEnvVar], "Expected FUGACI_SSH_PASSWORD_ENVVAR to be 'pass1'")
	assert.NotContains(t, envVars, "OTHER_ENV_VAR", "OTHER_ENV_VAR should not be present because its value is empty")
}
*/

/*
func TestVM_GetSSHConfig(t *testing.T) {
	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Env: []v1.EnvVar{
						{Name: SshUsernameEnvVar, Value: "user1"},
						{Name: SshPasswordEnvVar, Value: "pass1"},
					},
				},
			},
		},
	}

	vm := &VM{
		pod:            pod,
		containerIndex: 0,
	}

	t.Run("valid ssh config", func(t *testing.T) {
		sshConfig, err := vm.getSSHConfig()
		assert.NoError(t, err, "Expected no error from getSSHConfig")
		assert.Equal(t, "user1", sshConfig.User, "Expected SSH username to be 'user1'")
		assert.Len(t, sshConfig.Auth, 1, "Expected one SSH auth method")
		assert.Implements(t, (*ssh.AuthMethod)(nil), sshConfig.Auth[0], "Expected password-based auth method")
	})

	t.Run("missing SSH username", func(t *testing.T) {
		// Test missing SSH username
		vm.pod.Spec.Containers[0].Env = []v1.EnvVar{
			{Name: SshPasswordEnvVar, Value: "pass1"},
		}
		_, err := vm.getSSHConfig()
		assert.Error(t, err, "Expected error when SSH username is missing")
	})

	t.Run("missing SSH password", func(t *testing.T) {
		vm.pod.Spec.Containers[0].Env = []v1.EnvVar{
			{Name: SshUsernameEnvVar, Value: "user1"},
		}
		_, err := vm.getSSHConfig()
		assert.Error(t, err, "Expected error when SSH password is missing")
	})
}
*/

func TestVM_RunCommand(t *testing.T) {

	// Helper function to run the command and check errors
	runAndCheckError := func(t *testing.T, podSetupFunc func(*v1.Pod), expectedError string) {
		ctx := context.Background()
		vm, _, _, _, _ := setupCommonTestVM(t, podSetupFunc)
		err := vm.RunCommand(ctx, []string{"echo", "123"})
		assert.ErrorContains(t, err, expectedError)
	}

	t.Run("pod missing IP address", func(t *testing.T) {
		runAndCheckError(t, func(pod *v1.Pod) {
			pod.Spec.Containers = []v1.Container{
				{Name: "test123", Env: []v1.EnvVar{
					{Name: SshUsernameEnvVar, Value: "u"},
					{Name: SshPasswordEnvVar, Value: "p"},
				}},
			}
		}, "no pod IP found")
	})

	t.Run("pod ready with SSH session running", func(t *testing.T) {
		vm, _, _, mockSSHRunner, _ := setupCommonTestVM(t, func(pod *v1.Pod) {
			pod.Spec.Containers = []v1.Container{
				{Name: "test123", Env: []v1.EnvVar{
					{Name: SshUsernameEnvVar, Value: "u"},
					{Name: SshPasswordEnvVar, Value: "p"},
				}},
			}
		})
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
