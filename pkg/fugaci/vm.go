package fugaci

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Puller interface {
	Pull(ctx context.Context, image string, pullPolicy v1.PullPolicy, cb func(st v1.ContainerStateWaiting)) (imageID string, err error)
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
	pod            *v1.Pod
	containerIndex int

	lifetimeCtx context.Context
	cancelFunc  context.CancelCauseFunc

	mu           sync.Mutex
	wg           sync.WaitGroup
	containerPID atomic.Int64
}

func NewVM(virt Virtualization, puller Puller, pod *v1.Pod, containerIndex int) (*VM, error) {
	if containerIndex < 0 || containerIndex >= len(pod.Spec.Containers) {
		return nil, errors.New("invalid container index")
	}
	lifetimeCtx, cancelFunc := context.WithCancelCause(context.Background())
	if pod.Status.ContainerStatuses == nil {
		pod.Status.ContainerStatuses = make([]v1.ContainerStatus, len(pod.Spec.Containers))
	}
	now := metav1.Now()
	pod.Status.Phase = v1.PodRunning
	pod.Status.StartTime = &now

	cst := &pod.Status.ContainerStatuses[containerIndex]
	cst.Name = pod.Spec.Containers[containerIndex].Name
	cst.State = v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Creating", Message: "Just initialized"}}

	return &VM{
		virt:   virt,
		puller: puller,

		pod:            pod,
		containerIndex: containerIndex,

		lifetimeCtx: lifetimeCtx,
		cancelFunc:  cancelFunc,
	}, nil
}

func (s *VM) LifetimeContext() context.Context {
	return s.lifetimeCtx
}

func (s *VM) updateState(state v1.ContainerState) {
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

func (s *VM) terminate(reason string, err error) {
	log.Printf("vm terminated because '%s': %v", reason, err)
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
	s.updateState(st)
	s.cancelFunc(fmt.Errorf("terminated because of '%s': %s", reason, err.Error()))
}

func (s *VM) updateStatus(f func(s *v1.ContainerStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f(&s.pod.Status.ContainerStatuses[s.containerIndex])
}

func (s *VM) updatePodIP(ip net.IP) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pod.Status.PodIP = ip.String()
	//s.pod.Status.Phase = v1.PodSucceeded
}

func (s *VM) containerSpec() v1.Container {
	return s.pod.Spec.Containers[s.containerIndex]
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
		s.updateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Pulling"}})
		imageID, err := s.puller.Pull(s.lifetimeCtx, s.containerSpec().Image, s.containerSpec().ImagePullPolicy, func(st v1.ContainerStateWaiting) {
			s.updateState(v1.ContainerState{
				Waiting: &st,
			})
		})
		if err != nil {
			s.terminate("unable to pull image", err)
			return
		}
		log.Printf("pulled image: %v (%v)", s.containerSpec().Image, imageID)
		s.updateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Pulled"}})

		containerID, err := s.virt.Create(s.lifetimeCtx, *s.pod.DeepCopy(), 0)
		if err != nil {
			s.terminate("failed to create container", err)
			return
		}
		log.Printf("created container from image '%v': %v", s.containerSpec().Image, containerID)
		s.updateStatus(func(st *v1.ContainerStatus) {
			st.ContainerID = containerID
			st.Image = s.containerSpec().Image
			st.ImageID = imageID
		})
		s.updateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Created"}})
	} else {
		log.Printf("container '%s' already exists", s.GetContainerStatus().ContainerID)
		s.updateStatus(func(st *v1.ContainerStatus) {
			st.RestartCount += 1
		})
	}
	s.updateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "starting"}})
	containerID := s.GetContainerStatus().ContainerID
	runCmd, err := s.virt.Start(s.lifetimeCtx, containerID)
	if err != nil {
		err2 := s.virt.Destroy(s.lifetimeCtx, containerID)
		if err2 != nil {
			log.Printf("failed to destroy container: %v", err2)
		}
		s.terminate("unable to start process",
			fmt.Errorf("failed to start container '%v' with: %v", containerID, err))
		return
	}
	startedAt := metav1.Now()
	s.updateState(v1.ContainerState{Running: &v1.ContainerStateRunning{StartedAt: startedAt}})
	log.Printf("started container '%v': %v", containerID, runCmd)

	// TODO:
	go s.observeIP(s.lifetimeCtx, containerID)

	s.containerPID.Store(int64(runCmd.Process.Pid))

	err = runCmd.Wait()
	if err != nil {
		log.Printf("ProcessState at exit: %v, code=%d", runCmd.ProcessState.String(), runCmd.ProcessState.ExitCode())
		s.terminate("error while running container", fmt.Errorf("'%s' command failed: %w", runCmd, err))
		return
	}

	log.Printf("container '%v' finished successfully: %v, exit code=%d\n", containerID, runCmd, runCmd.ProcessState.ExitCode())
	s.updateState(v1.ContainerState{Terminated: &v1.ContainerStateTerminated{
		ExitCode:    int32(runCmd.ProcessState.ExitCode()),
		Reason:      "exited successfully",
		Message:     runCmd.ProcessState.String(),
		StartedAt:   startedAt,
		FinishedAt:  metav1.Now(),
		ContainerID: s.GetContainerStatus().ContainerID,
	}})
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
		log.Printf("%v: stopped VM gracefully", s.PrettyName())
	} else {
		log.Printf("%v: failed to stop VM gracefully: %v\n", s.PrettyName(), err)
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
	log.Printf("%v: waiting for vm.Run() to complete its operations\n", s.PrettyName())
	s.wg.Wait()
	st := s.Status()
	if len(st.ContainerID) > 0 {
		log.Printf("cleaning up ephemeral container %v", st.ContainerID)
		return s.virt.Destroy(context.Background(), st.ContainerID)
	}
	return nil
}

func (s *VM) observeIP(ctx context.Context, containerID string) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Exit if the context is canceled or the deadline is exceeded
			return
		case <-ticker.C:
			// Run the IP check synchronously within the select block
			ip, err := s.virt.IP(ctx, containerID)
			if err != nil {
				continue
			}
			s.updatePodIP(ip)
			s.updateStatus(func(st *v1.ContainerStatus) {
				st.Ready = true
				v := true
				st.Started = &v
			})
		}
	}
}

func (s *VM) Matches(namespace, name string) bool {
	return s.pod.Namespace == namespace && s.pod.Name == name
}

func (s *VM) GetPod() *v1.Pod {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pod.DeepCopy()
}

// env returns a map for each non-empty env variable
func (s *VM) env() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make(map[string]string)

	containerSpec := s.pod.Spec.Containers[s.containerIndex]
	// Iterate over the env field in the container to find the matching env vars
	for _, envVar := range containerSpec.Env {
		if len(envVar.Value) > 0 {
			res[envVar.Name] = envVar.Value
		}
	}
	return res
}

func (s *VM) getSSHConfig() (*ssh.ClientConfig, error) {
	env := s.env()
	username, ok := env[FUGACI_SSH_USERNAME_ENVVAR]
	if !ok {
		return nil, fmt.Errorf("%v: %v env var not found", FUGACI_SSH_USERNAME_ENVVAR, s.PrettyName())
	}
	password, ok := env[FUGACI_SSH_PASSWORD_ENVVAR]
	if !ok {
		return nil, fmt.Errorf("%v: %v env var not found", FUGACI_SSH_PASSWORD_ENVVAR, s.PrettyName())
	}

	return &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}, nil
}

func (s *VM) isEnvVarSensitive(name string) bool {
	sensitiveNames := []string{
		FUGACI_SSH_PASSWORD_ENVVAR,
	}
	for _, sensitiveName := range sensitiveNames {
		if strings.ToUpper(name) == sensitiveName {
			return true
		}
	}
	return false
}

func (s *VM) Exec(cmd []string, preConnection func(session *ssh.Session) error) error {
	if !s.IsReady() {
		return fmt.Errorf("%v is not ready yet", s.PrettyName())
	}
	config, err := s.getSSHConfig()
	if err != nil {
		return fmt.Errorf("failed to get SSH config for '%v': %w", s.PrettyName(), err)
	}
	pod := s.GetPod()
	address := fmt.Sprintf("%s:22", pod.Status.PodIP)
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return fmt.Errorf("%v: failed to connect to '%v': %w", s.PrettyName(), address, err)
	}
	defer client.Close()

	// Create a session for running the command
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("%v: failed to create SSH session: %w", s.PrettyName(), err)
	}
	defer session.Close()

	// Quote each argument to handle spaces and special characters
	for i, arg := range cmd {
		cmd[i] = shellQuote(arg)
	}

	// Join the command and arguments into a single string
	commandStr := strings.Join(cmd, " ")

	if err := preConnection(session); err != nil {
		return fmt.Errorf("%v: failed to apply pre conenction settings to '%v': %w", s.PrettyName(), commandStr, err)
	}

	env := s.env()

	// TODO: Use basic: conn, err := net.DialTimeout("tcp", address, timeout) to determine if VM is Ready (not IP only) (for bootstrap)
	// TODO: Add a file to /etc/ssh/sshd_config.d/* with "AcceptEnv KUBERNETES_* FUGACI_*"
	// TODO: Set env vars optionally (not during initial VM bootstrap)
	for name, value := range env {
		if s.isEnvVarSensitive(name) {
			continue
		}
		err = session.Setenv(name, value)
		if err != nil {
			log.Printf("%v: failed to set environment variable '%v': %v", s.PrettyName(), name, err)
		}
	}

	return session.Run(commandStr)
}

func (s *VM) IsReady() bool {
	st := s.GetContainerStatus()
	return st.Ready
}

func (s *VM) PrettyName() string {
	pod := s.GetPod()
	spec := s.GetContainerSpec()
	return fmt.Sprintf("vm '%s' @ pod %s/%s", spec.Name, pod.Namespace, pod.Name)
}
