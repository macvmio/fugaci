package fugaci

import (
	"context"
	"errors"
	"fmt"
	"github.com/tomekjarosik/fugaci/pkg/sshrunner"
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

const VmSshPort = 22

type Puller interface {
	Pull(ctx context.Context, image string, pullPolicy v1.PullPolicy, cb func(st v1.ContainerStateWaiting)) (imageID string, err error)
}

type SSHRunner interface {
	Run(address string, config *ssh.ClientConfig, cmd []string, preConnection ...func(session *ssh.Session) error) error
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
}

func NewVM(virt Virtualization, puller Puller, sshRunner SSHRunner, pod *v1.Pod, containerIndex int) (*VM, error) {
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
		virt:      virt,
		puller:    puller,
		sshRunner: sshRunner,

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
	// Must be safe to run from multiple goroutines
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

func (s *VM) waitAndRunCommandInside(ctx context.Context, container v1.Container, containerID string) {
	ip := s.waitForIP(ctx, containerID)
	if ip == nil {
		return
	}
	s.updatePodIP(ip)

	/*noOp := func(session *ssh.Session) error { return nil }
	err := s.RunCommand(container.Command, noOp)
	if err != nil {
		log.Printf("%v: vm failed to run command '%s': %v", s.PrettyName(), container.Command, err)
		s.terminate("ssh failed", err)
	}*/

	s.updateStatus(func(st *v1.ContainerStatus) {
		st.Ready = true
		v := true
		st.Started = &v
	})
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
	s.containerPID.Store(int64(runCmd.Process.Pid))

	s.wg.Add(1)
	go func(c v1.Container, cID string) {
		defer s.wg.Done()
		s.waitAndRunCommandInside(s.lifetimeCtx, c, cID)
	}(s.GetContainerSpec(), containerID)

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
		return nil, fmt.Errorf("env var not found: %v", FUGACI_SSH_USERNAME_ENVVAR)
	}
	password, ok := env[FUGACI_SSH_PASSWORD_ENVVAR]
	if !ok {
		return nil, fmt.Errorf("env var not found: %v", FUGACI_SSH_PASSWORD_ENVVAR)
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

// TODO: Use basic: conn, err := net.DialTimeout("tcp", address, timeout) to determine if VM is Ready (not IP only) (for bootstrap)
// TODO: Add a file to /etc/ssh/sshd_config.d/* with "AcceptEnv KUBERNETES_* FUGACI_*"
//
// TODO: Set env vars optionally (not during initial VM bootstrap)

// AcceptEnv KUBERNETES_* FUGACI_*
// ClientAliveInterval 10
// ClientAliveCountMax 5
func (s *VM) RunCommand(cmd []string, preConnection func(session *ssh.Session) error) error {
	if !s.IsReady() {
		return fmt.Errorf("%v is not ready yet", s.PrettyName())
	}
	config, err := s.getSSHConfig()
	if err != nil {
		return fmt.Errorf("failed to get SSH config: %w", err)
	}
	pod := s.GetPod()
	if len(pod.Status.PodIP) == 0 {
		return fmt.Errorf("no pod IP found")
	}
	address := fmt.Sprintf("%s:%d", pod.Status.PodIP, VmSshPort)

	preConnectionFuncs := []func(session *ssh.Session) error{
		func(session *ssh.Session) error {
			return sshrunner.SetEnvVars(session, s.env(), s.isEnvVarSensitive)
		},
		preConnection,
	}
	return s.sshRunner.Run(address, config, cmd, preConnectionFuncs...)
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
