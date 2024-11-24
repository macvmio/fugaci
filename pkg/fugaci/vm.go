package fugaci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	regv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/macvmio/fugaci/pkg/sshrunner"
	"github.com/macvmio/fugaci/pkg/storyline"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const VmSshPort = 22

var ErrSSHNotReady = errors.New("ssh not ready")
var ErrIPNotAssigned = errors.New("ip not assigned")

type Puller interface {
	Pull(ctx context.Context, image string, pullPolicy v1.PullPolicy, cb func(st v1.ContainerStateWaiting)) (regv1.Image, error)
}

type SSHRunner interface {
	Run(ctx context.Context, dialInfo sshrunner.DialInfo, cmd []string, opts ...sshrunner.Option) error
}

type PortForwarder interface {
	PortForward(ctx context.Context, address string, stream io.ReadWriteCloser) error
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
	portForwarder  PortForwarder
	pod            *v1.Pod
	containerIndex atomic.Int32

	vmLifetimeCtx context.Context
	vmCancelFunc  context.CancelCauseFunc

	cmdLifetimeCtx context.Context
	cmdCancelFunc  context.CancelCauseFunc

	mu           sync.Mutex
	wg           sync.WaitGroup
	containerPID atomic.Int64

	sshDialInfo sshrunner.DialInfo
	env         []v1.EnvVar

	logger    *log.Logger
	storyLine *storyline.StoryLine
}

func NewVM(ctx context.Context, virt Virtualization, puller Puller, sshRunner SSHRunner, portForwarder PortForwarder, pod *v1.Pod, containerIndex int) (*VM, error) {
	if containerIndex < 0 || containerIndex >= len(pod.Spec.Containers) {
		return nil, errors.New("invalid container index")
	}
	lifetimeCtx, cancelFunc := context.WithCancelCause(ctx)
	cmdLifetimeCtx, cmdCancelFunc := context.WithCancelCause(lifetimeCtx)
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

	envVars, username, password, err := extractSSHEnvVars(pod.Spec.Containers[containerIndex])
	if err != nil {
		return nil, fmt.Errorf("failed to extract ssh env vars: %w", err)
	}

	vm := &VM{
		virt:          virt,
		puller:        puller,
		sshRunner:     sshRunner,
		portForwarder: portForwarder,

		pod:            pod,
		vmLifetimeCtx:  lifetimeCtx,
		vmCancelFunc:   cancelFunc,
		cmdLifetimeCtx: cmdLifetimeCtx,
		cmdCancelFunc:  cmdCancelFunc,
		sshDialInfo: sshrunner.DialInfo{
			Address:  "notset",
			Username: username,
			Password: password,
		},
		env:       envVars,
		logger:    customLogger,
		storyLine: storyline.New(),
	}
	vm.containerIndex.Store(int32(containerIndex))
	return vm, nil
}
func extractSSHEnvVars(container v1.Container) (envVars []v1.EnvVar, username, password string, err error) {
	for _, envVar := range container.Env {
		switch envVar.Name {
		case SshUsernameEnvVar:
			username = envVar.Value
		case SshPasswordEnvVar:
			password = envVar.Value
		default:
			envVars = append(envVars, envVar)
		}
	}
	if username == "" {
		err = fmt.Errorf("env var not found: %v", SshUsernameEnvVar)
	}
	if password == "" {
		err = fmt.Errorf("env var not found: %v", SshPasswordEnvVar)
	}
	return
}

func (s *VM) LifetimeContext() context.Context {
	return s.vmLifetimeCtx
}

func (s *VM) terminateWithError(reason string, err error) {
	// Must be safe to run from multiple goroutines
	s.storyLine.Add("action", "terminateWithError")
	s.storyLine.Add("reason", reason)
	s.storyLine.Add("error", err)

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
	s.vmCancelFunc(fmt.Errorf("terminated because of '%s': %s", reason, err.Error()))
}

func (s *VM) updateStatus(updateFunc func(s *v1.ContainerStatus)) {
	s.safeUpdatePod(func(pod *v1.Pod) {
		updateFunc(&pod.Status.ContainerStatuses[s.containerIndex.Load()])
	})
}

func (s *VM) safeUpdatePodIP(ip net.IP) {
	s.safeUpdatePod(func(pod *v1.Pod) {
		pod.Status.PodIP = ip.String()

		// TODO: Move conditions elsewhere
		if pod.Status.Conditions == nil {
			pod.Status.Conditions = []v1.PodCondition{}
		}
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
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sshDialInfo.Address = fmt.Sprintf("%s:%d", ip, VmSshPort)
}

func (s *VM) waitAndRunCommandInside(ctx context.Context, startedAt time.Time, containerID string) error {
	waitCtx, waitForIpCancelFunc := context.WithTimeout(ctx, 60*time.Second)
	defer waitForIpCancelFunc()
	ip := s.waitForIP(waitCtx, containerID)

	s.storyLine.Add("ip", ip)
	defer s.logger.Printf("waitAndRunCommandInside has finished")

	if ip == nil {
		return ErrIPNotAssigned
	}
	s.safeUpdatePodIP(ip)

	err := s.waitForSSHReady(ctx, startedAt)
	// tried too many times and there is still error
	if err != nil {
		s.storyLine.Add("state", "SSHNotReady")
		s.storyLine.AddElapsedTimeSince("SSHNotReady", startedAt)
		s.logger.Printf("failed to establish SSH session")
		return ErrSSHNotReady
	}
	s.updateStatus(func(st *v1.ContainerStatus) {
		st.Ready = true
		v := true
		st.Started = &v
	})
	s.logger.Printf("successfully established SSH session")
	command := s.GetCommand()
	s.storyLine.Add("container_command", command)
	err = s.RunCommand(ctx, command, sshrunner.WithEnv(s.GetEnvVars()))
	if err != nil {
		s.storyLine.Add("container_command_run_err", err)
		s.logger.Printf("command '%v' finished with error: %v", s.GetCommand(), err)
	}
	s.storyLine.AddElapsedTimeSince("container_command_finished", startedAt)
	return nil
}

func (s *VM) waitForSSHReady(ctx context.Context, startedAt time.Time) error {
	retriesCount := 50
	var err error

	for i := 0; i < retriesCount; i++ {
		if ctx.Err() != nil {
			err = ctx.Err()
			break
		}
		ctx2, cancel := context.WithTimeout(ctx, 1*time.Second)
		err = s.RunCommand(ctx2, []string{"echo", "hello"}, sshrunner.WithTimeout(900*time.Millisecond))
		if err == nil {
			s.storyLine.Add("state", "SSHReady")
			s.storyLine.AddElapsedTimeSince("SSHReady", startedAt)
			cancel()
			break
		}
		cancel()
		s.logger.Printf("SSH not ready yet: %v", err)
		time.Sleep(500 * time.Millisecond)
	}
	return err
}

func (s *VM) ensureContainerExists(spec v1.Container, initTime time.Time) (reason string, err error) {
	if len(s.GetContainerStatus().ContainerID) != 0 {
		s.logger.Printf("container '%s' already exists", s.GetContainerStatus().ContainerID)
		s.updateStatus(func(st *v1.ContainerStatus) {
			st.RestartCount += 1
		})
		return "ok", nil
	}

	imageID, reason, err := s.pullContainerImage(spec, initTime)
	if err != nil {
		return reason, err
	}

	containerID, err := s.virt.Create(s.vmLifetimeCtx, *s.pod.DeepCopy(), 0)
	if err != nil {
		return "failed to create container", err
	}
	s.logger.Printf("created container from image '%v': %v", spec.Image, containerID)
	s.storyLine.Add("state", "created")
	s.storyLine.Add("containerID", containerID)
	s.storyLine.AddElapsedTimeSince("created", initTime)

	s.updateStatus(func(st *v1.ContainerStatus) {
		st.ContainerID = containerID
		st.Image = spec.Image
		st.ImageID = imageID
	})
	s.safeUpdateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Created"}})

	return "ok", nil
}

func (s *VM) pullContainerImage(spec v1.Container, initTime time.Time) (imageID string, reason string, err error) {
	s.safeUpdateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Pulling"}})
	// NOTE: We do have v1.Image here, so we can extract much more than just imageID if needed (cmd, env, labels...)
	pulledImg, err := s.puller.Pull(s.vmLifetimeCtx, spec.Image, spec.ImagePullPolicy, func(st v1.ContainerStateWaiting) {
		s.safeUpdateState(v1.ContainerState{
			Waiting: &st,
		})
	})
	s.storyLine.Add("action", "pulling")
	s.storyLine.Add("spec.image", spec.Image)

	if err != nil {
		return "", "unable to pull image", err
	}
	s.storyLine.Add("pulling", "success")
	hashImageID, err := pulledImg.Digest()
	if err != nil {
		return "", "unable to obtain image digest", err
	}
	s.storyLine.Add("imageID", hashImageID)
	s.storyLine.AddElapsedTimeSince("pulling", initTime)
	s.logger.Printf("pulled image: %v (ID: %v)", spec.Image, hashImageID)

	s.safeUpdateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Pulled"}})
	return hashImageID.String(), "", nil
}

func (s *VM) Run() {
	s.wg.Add(1)
	defer s.wg.Done()

	defer s.updateContainerReadyStatus(false)

	initTime := time.Now()
	spec := s.GetContainerSpec()
	if reason, err := s.ensureContainerExists(spec, initTime); err != nil {
		s.storyLine.Add("err", err)
		s.terminateWithError(reason, err)
		return
	}
	containerID := s.GetContainerStatus().ContainerID
	runCmd, startedAt, err := s.startContainer(containerID, initTime)
	if err != nil {
		s.storyLine.Add("err", err)
		s.terminateWithError("unable to start process", err)
		return
	}

	err = s.waitAndRunCommandInside(s.cmdLifetimeCtx, startedAt.Time, containerID)

	s.wg.Add(1)
	// This needs to be done on separate thread, because otherwise will result in defunct process,
	// and Stop() method will keep running
	go s.stopContainer(containerID, startedAt.Time)

	err = runCmd.Wait()
	s.storyLine.Add("container_exitcode", runCmd.ProcessState.ExitCode())
	s.storyLine.Add("container_process_state", runCmd.ProcessState)
	if err != nil && runCmd.ProcessState.ExitCode() != 0 {
		s.storyLine.Add("container_exit_err", err)
		s.logger.Printf("ProcessState at exit: %v, code=%d", runCmd.ProcessState.String(), runCmd.ProcessState.ExitCode())
		s.terminateWithError("error from runCmd.Wait()", fmt.Errorf("'%s' command failed: %w", runCmd, err))
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
	s.vmCancelFunc(nil)
}

func (s *VM) startContainer(containerID string, initTime time.Time) (*exec.Cmd, metav1.Time, error) {
	s.safeUpdateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "starting"}})
	runCmd, err := s.virt.Start(s.vmLifetimeCtx, containerID)
	if err != nil {
		err2 := s.virt.Destroy(s.vmLifetimeCtx, containerID)
		if err2 != nil {
			s.logger.Printf("failed to destroy container: %v", err2)
		}
		return nil, metav1.Time{}, fmt.Errorf("failed to start container '%v' with: %v", containerID, err)
	}
	startedAt := metav1.Now()
	s.safeUpdateState(v1.ContainerState{Running: &v1.ContainerStateRunning{StartedAt: startedAt}})
	s.logger.Printf("started container '%v': %v", containerID, runCmd)
	s.storyLine.AddElapsedTimeSince("started", initTime)
	s.containerPID.Store(int64(runCmd.Process.Pid))
	return runCmd, startedAt, nil
}

func (s *VM) stopContainer(containerID string, startedAt time.Time) {
	defer s.wg.Done()
	s.storyLine.Add("action", "stop")
	err := s.virt.Stop(s.vmLifetimeCtx, int(s.containerPID.Load()))
	if err != nil {
		s.logger.Printf("failed to stop container '%v': %v", containerID, err)
		s.storyLine.Add("err", err)
	}
	s.storyLine.AddElapsedTimeSince("command_stopped", startedAt)
}

func (s *VM) updateContainerReadyStatus(status bool) {
	s.updateStatus(func(st *v1.ContainerStatus) {
		st.Ready = status
	})
}

func (s *VM) Cleanup() error {
	defer func() {
		s.logger.Println(s.storyLine.String())
	}()

	cleanupTimestamp := time.Now()
	stopCtx, cancelStopCtx := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancelStopCtx()

	s.storyLine.Add("action", "cleanup")
	defer s.storyLine.AddElapsedTimeSince("cleanup", cleanupTimestamp)

	s.cmdCancelFunc(nil)

	err := s.virt.Stop(stopCtx, int(s.containerPID.Load()))
	if err == nil {
		s.storyLine.Add("stop", "ok")
		s.logger.Printf("stopped VM gracefully")
	} else {
		s.storyLine.Add("stop", err)
		s.logger.Printf("failed to stop VM gracefully: %v", err)
		p, err := os.FindProcess(int(s.containerPID.Load()))
		if err != nil {
			s.storyLine.Add("noProcess", err)
			return fmt.Errorf("could not find process %d: %v", s.containerPID.Load(), err)
		}
		defer p.Release()
		err = p.Kill()
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			s.storyLine.Add("killError", err)
			return err
		}
	}

	s.vmCancelFunc(errors.New("aborted by user"))
	s.logger.Printf("waiting for vm.Run() to complete its operations")
	s.wg.Wait()

	s.logger.Printf("vm.Run() has completed waiting")
	st := s.Status()
	if len(st.ContainerID) > 0 {
		s.logger.Printf("cleaning up ephemeral container %v", st.ContainerID)
		err2 := s.virt.Destroy(context.Background(), st.ContainerID)
		if err2 != nil {
			s.logger.Printf("failed to destroy container: %v", err2)
			s.storyLine.Add("cleanUpError", err2)
		}
		return err2
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

// TODO: Add a file to /etc/ssh/sshd_config.d/* with "AcceptEnv KUBERNETES_* FUGACI_*"

// AcceptEnv KUBERNETES_* FUGACI_*
// ClientAliveInterval 10
// ClientAliveCountMax 5
func (s *VM) RunCommand(ctx context.Context, cmd []string, opts ...sshrunner.Option) error {
	extOpts := make([]sshrunner.Option, 0)
	extOpts = append(extOpts, opts...)
	return s.sshRunner.Run(ctx, s.sshDialInfo, cmd, extOpts...)
}

func (s *VM) PortForward(ctx context.Context, port int32, stream io.ReadWriteCloser) error {
	return s.portForwarder.PortForward(ctx, fmt.Sprintf("%s:%d", s.safeGetPod().Status.PodIP, port), stream)
}

// Below are functions which are safe to call in multiple goroutines

func (s *VM) IsReady() bool {
	st := s.GetContainerStatus()
	return st.Ready
}

func (s *VM) Status() v1.ContainerStatus {
	podCopy := s.safeGetPod()
	return podCopy.Status.ContainerStatuses[s.containerIndex.Load()]
}

func (s *VM) GetContainerStatus() v1.ContainerStatus {
	podCopy := s.safeGetPod()
	return podCopy.Status.ContainerStatuses[s.containerIndex.Load()]
}

func (s *VM) GetContainerSpec() v1.Container {
	podCopy := s.safeGetPod()
	return podCopy.Spec.Containers[s.containerIndex.Load()]
}
func (s *VM) GetPod() *v1.Pod {
	return s.safeGetPod()
}

func (s *VM) GetCommand() []string {
	podCopy := s.safeGetPod()
	return podCopy.Spec.Containers[s.containerIndex.Load()].Command
}

func (s *VM) GetEnvVars() []v1.EnvVar {
	s.mu.Lock()
	defer s.mu.Unlock()

	res := make([]v1.EnvVar, len(s.env))
	copy(res, s.env)

	return res
}

func (s *VM) safeUpdateState(state v1.ContainerState) {
	s.safeUpdatePod(func(pod *v1.Pod) {
		pod.Status.ContainerStatuses[s.containerIndex.Load()].State = state
	})
}

func (s *VM) safeGetPod() *v1.Pod {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pod.DeepCopy()
}

func (s *VM) safeUpdatePod(update func(pod *v1.Pod)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	update(s.pod)
}
