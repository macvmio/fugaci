package fugaci

import (
	"context"
	"errors"
	"fmt"
	"github.com/tomekjarosik/fugaci/pkg/sshrunner"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const VmSshPort = 22

type Puller interface {
	Pull(ctx context.Context, image string, pullPolicy v1.PullPolicy, cb func(st v1.ContainerStateWaiting)) (imageID string, err error)
}

type SSHRunner interface {
	Run(ctx context.Context, dialInfo sshrunner.DialInfo, cmd []string, opts ...sshrunner.Option) error
}

type VirtualizationLifecycle interface {
	Create(ctx context.Context, pod v1.Pod, containerIndex int) (containerID string, err error)
	Start(ctx context.Context, containerID string) (runCommand *exec.Cmd, err error)
	Stop(ctx context.Context, containerPID int) error
	Destroy(ctx context.Context, containerID string) error
}

type VirtualizationStatus interface {
	IP(ctx context.Context, containerID string) (net.IP, error)
	Exists(ctx context.Context, containerID string) (bool, error)
}

type Virtualization interface {
	VirtualizationLifecycle
	VirtualizationStatus
}

type VM struct {
	virt           Virtualization
	puller         Puller
	sshRunner      SSHRunner
	pod            *v1.Pod
	containerIndex int

	lifetimeCtx context.Context
	cancelFunc  context.CancelCauseFunc

	mu           sync.Mutex
	wg           sync.WaitGroup
	containerPID atomic.Int64

	sshDialInfo sshrunner.DialInfo
	env         []v1.EnvVar

	logger *log.Logger
}

func NewVM(ctx context.Context, virt Virtualization, puller Puller, sshRunner SSHRunner, pod *v1.Pod, containerIndex int) (*VM, error) {
	if containerIndex < 0 || containerIndex >= len(pod.Spec.Containers) {
		return nil, errors.New("invalid container index")
	}
	lifetimeCtx, cancelFunc := context.WithCancelCause(ctx)
	if pod.Status.ContainerStatuses == nil {
		pod.Status.ContainerStatuses = make([]v1.ContainerStatus, len(pod.Spec.Containers))
	}
	now := metav1.Now()
	pod.Status.Phase = v1.PodRunning
	pod.Status.StartTime = &now

	cst := &pod.Status.ContainerStatuses[containerIndex]
	cst.Name = pod.Spec.Containers[containerIndex].Name
	cst.State = v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Creating", Message: "Just initialized"}}

	customLogger := log.New(os.Stdout,
		fmt.Sprintf("pod=%s/%s, ", pod.Namespace, pod.Name),
		log.LstdFlags|log.Lmsgprefix|log.Lshortfile)

	envVars := make([]v1.EnvVar, 0)
	username := ""
	password := ""
	for _, nameVal := range pod.Spec.Containers[containerIndex].Env {
		envVars = append(envVars, nameVal)
		if nameVal.Name == SshUsernameEnvVar {
			username = nameVal.Value
		}
		if nameVal.Name == SshPasswordEnvVar {
			password = nameVal.Value
		}
	}
	if len(username) == 0 {
		return nil, fmt.Errorf("env var not found: %v", SshUsernameEnvVar)
	}
	if len(password) == 0 {
		return nil, fmt.Errorf("env var not found: %v", SshPasswordEnvVar)
	}

	return &VM{
		virt:      virt,
		puller:    puller,
		sshRunner: sshRunner,

		pod:            pod,
		containerIndex: containerIndex,

		lifetimeCtx: lifetimeCtx,
		cancelFunc:  cancelFunc,
		sshDialInfo: sshrunner.DialInfo{
			Address:  "notset",
			Username: username,
			Password: password,
		},
		env:    envVars,
		logger: customLogger,
	}, nil
}

func (s *VM) LifetimeContext() context.Context {
	return s.lifetimeCtx
}

func (s *VM) terminateWithError(reason string, err error) {
	// Must be safe to run from multiple goroutines
	s.logger.Printf("vm terminated because '%s': %v", reason, err)
	prevStatus := s.GetContainerStatus()
	st := v1.ContainerState{Terminated: &v1.ContainerStateTerminated{
		ExitCode:    1,
		Reason:      reason,
		Message:     err.Error(),
		FinishedAt:  metav1.Now(),
		ContainerID: prevStatus.ContainerID,
	}}
	if prevStatus.State.Running != nil {
		st.Terminated.StartedAt = prevStatus.State.Running.StartedAt
	}
	s.safeUpdateState(st)
	s.safeUpdatePod(func(pod *v1.Pod) {
		pod.Status.Phase = v1.PodFailed
	})
	s.cancelFunc(fmt.Errorf("terminated because of '%s': %s", reason, err.Error()))
}

func (s *VM) updateStatus(f func(s *v1.ContainerStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f(&s.pod.Status.ContainerStatuses[s.containerIndex])
}

func (s *VM) safeUpdatePodIP(ip net.IP) {
	s.safeUpdatePod(func(pod *v1.Pod) {
		pod.Status.PodIP = ip.String()
		if pod.Status.Conditions == nil {
			pod.Status.Conditions = []v1.PodCondition{}
		}
		s.sshDialInfo.Address = fmt.Sprintf("%s:%d", ip, VmSshPort)

		pod.Status.Conditions = append(pod.Status.Conditions, v1.PodCondition{
			Type:          v1.PodReady,
			Status:        v1.ConditionTrue,
			LastProbeTime: metav1.Now(),
		})
		pod.Status.Conditions = append(pod.Status.Conditions, v1.PodCondition{
			Type:          v1.ContainersReady,
			Status:        v1.ConditionTrue,
			LastProbeTime: metav1.Now(),
		})
	})
}

func (s *VM) waitAndRunCommandInside(ctx context.Context, container v1.Container, containerID string) {
	ip := s.waitForIP(ctx, containerID)
	if ip == nil {
		return
	}
	s.safeUpdatePodIP(ip)

	retriesCount := 50
	var err error

	for i := 0; i < retriesCount; i++ {
		ctx2, cancel := context.WithTimeout(ctx, 1*time.Second)
		err = s.RunCommand(ctx2, []string{"echo", "hello"}, sshrunner.WithTimeout(900*time.Millisecond))
		if err == nil {
			cancel()
			break
		}
		cancel()
		s.logger.Printf("SSH not ready yet: %v", err)
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		s.logger.Printf("failed to establish SSH session")
		return
	}
	s.updateStatus(func(st *v1.ContainerStatus) {
		st.Ready = true
		v := true
		st.Started = &v
	})
	s.logger.Printf("successfully established SSH session")
	err = s.RunCommand(ctx, s.GetCommand(), sshrunner.WithEnv(s.GetEnvVars()))
	if err != nil {
		s.logger.Printf("command finished with error: %v", err)
	}
	s.logger.Printf("waitAndRunCommandInside has finished")
}

func (s *VM) Run() {
	s.wg.Add(1)
	defer s.wg.Done()

	defer func() {
		s.updateStatus(func(st *v1.ContainerStatus) {
			st.Ready = false
		})
	}()

	if len(s.GetContainerStatus().ContainerID) == 0 {
		s.safeUpdateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Pulling"}})
		spec := s.GetContainerSpec()
		imageID, err := s.puller.Pull(s.lifetimeCtx, spec.Image, spec.ImagePullPolicy, func(st v1.ContainerStateWaiting) {
			s.safeUpdateState(v1.ContainerState{
				Waiting: &st,
			})
		})
		if err != nil {
			s.terminateWithError("unable to pull image", err)
			return
		}
		s.logger.Printf("pulled image: %v (%v)", spec.Image, imageID)
		s.safeUpdateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Pulled"}})

		containerID, err := s.virt.Create(s.lifetimeCtx, *s.pod.DeepCopy(), 0)
		if err != nil {
			s.terminateWithError("failed to create container", err)
			return
		}
		s.logger.Printf("created container from image '%v': %v", spec.Image, containerID)
		s.updateStatus(func(st *v1.ContainerStatus) {
			st.ContainerID = containerID
			st.Image = spec.Image
			st.ImageID = imageID
		})
		s.safeUpdateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Created"}})
	} else {
		s.logger.Printf("container '%s' already exists", s.GetContainerStatus().ContainerID)
		s.updateStatus(func(st *v1.ContainerStatus) {
			st.RestartCount += 1
		})
	}
	s.safeUpdateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "starting"}})
	containerID := s.GetContainerStatus().ContainerID
	runCmd, err := s.virt.Start(s.lifetimeCtx, containerID)
	if err != nil {
		err2 := s.virt.Destroy(s.lifetimeCtx, containerID)
		if err2 != nil {
			s.logger.Printf("failed to destroy container: %v", err2)
		}
		s.terminateWithError("unable to start process",
			fmt.Errorf("failed to start container '%v' with: %v", containerID, err))
		return
	}
	startedAt := metav1.Now()
	s.safeUpdateState(v1.ContainerState{Running: &v1.ContainerStateRunning{StartedAt: startedAt}})
	s.logger.Printf("started container '%v': %v", containerID, runCmd)
	s.containerPID.Store(int64(runCmd.Process.Pid))

	s.wg.Add(1)
	go func(c v1.Container, cID string) {
		defer s.wg.Done()
		s.waitAndRunCommandInside(s.lifetimeCtx, c, cID)
		s.logger.Printf("goroutine finished running command inside container")
	}(s.GetContainerSpec(), containerID)

	err = runCmd.Wait()
	if err != nil {
		s.logger.Printf("ProcessState at exit: %v, code=%d", runCmd.ProcessState.String(), runCmd.ProcessState.ExitCode())
		s.terminateWithError("error while running container", fmt.Errorf("'%s' command failed: %w", runCmd, err))
		return
	}

	s.logger.Printf("container '%v' finished successfully: %v, exit code=%d\n", containerID, runCmd, runCmd.ProcessState.ExitCode())
	s.safeUpdateState(v1.ContainerState{Terminated: &v1.ContainerStateTerminated{
		ExitCode:    int32(runCmd.ProcessState.ExitCode()),
		Reason:      "exited successfully",
		Message:     runCmd.ProcessState.String(),
		StartedAt:   startedAt,
		FinishedAt:  metav1.Now(),
		ContainerID: s.GetContainerStatus().ContainerID,
	}})
	s.safeUpdatePod(func(pod *v1.Pod) {
		pod.Status.Phase = v1.PodSucceeded
	})
	s.cancelFunc(nil)
}

func (s *VM) Status() v1.ContainerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pod.Status.ContainerStatuses[s.containerIndex]
}

func (s *VM) Cleanup() error {
	stopCtx, cancelStopCtx := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStopCtx()

	err := s.virt.Stop(stopCtx, int(s.containerPID.Load()))
	if err == nil {
		s.logger.Printf("stopped VM gracefully")
	} else {
		s.logger.Printf("failed to stop VM gracefully: %v", err)
		p, err := os.FindProcess(int(s.containerPID.Load()))
		if err != nil {
			return fmt.Errorf("could not find process %d: %v", s.containerPID.Load(), err)
		}
		defer p.Release()
		err = p.Kill()
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}

	s.cancelFunc(errors.New("aborted by user"))
	s.logger.Printf("waiting for vm.Run() to complete its operations")
	s.wg.Wait()
	s.logger.Printf("vm.Run() has completed waiting")
	st := s.Status()
	if len(st.ContainerID) > 0 {
		s.logger.Printf("cleaning up ephemeral container %v", st.ContainerID)
		return s.virt.Destroy(context.Background(), st.ContainerID)
	}
	return nil
}

func (s *VM) waitForIP(ctx context.Context, containerID string) net.IP {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Exit if the context is canceled or the deadline is exceeded
			return nil
		case <-ticker.C:
			// Run the IP check synchronously within the select block
			ip, err := s.virt.IP(ctx, containerID)
			if err != nil {
				continue
			}
			return ip
		}
	}
}

func (s *VM) Matches(namespace, name string) bool {
	return s.pod.Namespace == namespace && s.pod.Name == name
}

// TODO: Use basic: conn, err := net.DialTimeout("tcp", address, timeout) to determine if VM is Ready (not IP only) (for bootstrap)
// TODO: Add a file to /etc/ssh/sshd_config.d/* with "AcceptEnv KUBERNETES_* FUGACI_*"

// AcceptEnv KUBERNETES_* FUGACI_*
// ClientAliveInterval 10
// ClientAliveCountMax 5
func (s *VM) RunCommand(ctx context.Context, cmd []string, opts ...sshrunner.Option) error {
	extOpts := make([]sshrunner.Option, 0)
	extOpts = append(extOpts, opts...)
	return s.sshRunner.Run(ctx, s.sshDialInfo, cmd, extOpts...)
}

// Below are functions which are safe to call in multiple goroutines

func (s *VM) IsReady() bool {
	st := s.GetContainerStatus()
	return st.Ready
}

func (s *VM) safeUpdateState(state v1.ContainerState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pod.Status.ContainerStatuses[s.containerIndex].State = state
}

func (s *VM) GetContainerStatus() v1.ContainerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pod.Status.ContainerStatuses[s.containerIndex]
}

func (s *VM) GetContainerSpec() v1.Container {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pod.Spec.Containers[s.containerIndex]
}
func (s *VM) GetPod() *v1.Pod {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pod.DeepCopy()
}

func (s *VM) GetCommand() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	original := s.pod.Spec.Containers[s.containerIndex].Command
	res := make([]string, len(original))
	copy(res, original)
	return res
}

func (s *VM) GetEnvVars() []v1.EnvVar {
	s.mu.Lock()
	defer s.mu.Unlock()

	res := make([]v1.EnvVar, len(s.env))
	copy(res, s.env)

	return res
}

func (s *VM) safeUpdatePod(update func(pod *v1.Pod)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	update(s.pod)
}
