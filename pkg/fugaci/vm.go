package fugaci

import (
	"context"
	"errors"
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

type Puller interface {
	Pull(ctx context.Context, image string, policy v1.PullPolicy) error
}

type VirtualizationLifecycle interface {
	Create(ctx context.Context, pod v1.Pod, containerIndex int) (containerID string, err error)
	Start(ctx context.Context, containerID string) (runCommand *exec.Cmd, err error)
	Stop(ctx context.Context, containerRunCmd *exec.Cmd) error
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
	runCmd      *exec.Cmd

	mu sync.Mutex
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

func (s *VM) terminate(reason string, err error) {
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
}

func (s *VM) containerSpec() v1.Container {
	return s.pod.Spec.Containers[s.containerIndex]
}

func (s *VM) Run() {
	defer func() {
		s.updateStatus(func(st *v1.ContainerStatus) {
			st.Ready = false
		})
	}()

	s.updateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Pulling"}})
	if len(s.GetContainerStatus().ContainerID) == 0 {
		err := s.puller.Pull(s.lifetimeCtx, s.containerSpec().Image, s.containerSpec().ImagePullPolicy)
		if err != nil {
			s.terminate("unable to pull image", err)
			return
		}
		log.Printf("pulled image: %v", s.containerSpec().Image)
		s.updateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Pulled"}})

		containerID, err := s.virt.Create(s.lifetimeCtx, *s.pod, 0)
		if err != nil {
			s.terminate("failed to create container", err)
			return
		}
		log.Printf("created container from image '%v': %v", s.containerSpec().Image, containerID)
		s.updateStatus(func(st *v1.ContainerStatus) {
			st.ContainerID = containerID
			st.Image = s.containerSpec().Image
			st.ImageID = s.containerSpec().Image
		})
		s.updateState(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "Created"}})
	} else {
		log.Printf("container '%s' already exists", s.GetContainerStatus().ContainerID)
		s.updateStatus(func(st *v1.ContainerStatus) {
			st.RestartCount += 1
		})
	}
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
	s.runCmd = runCmd
	startedAt := metav1.Now()
	s.updateState(v1.ContainerState{Running: &v1.ContainerStateRunning{StartedAt: startedAt}})
	log.Printf("started container '%v': %v", containerID, runCmd)
	go s.observeIP(s.lifetimeCtx, containerID)
	err = runCmd.Wait()
	if err != nil {
		s.terminate("error while running container", fmt.Errorf("'%s' command failed: %w", runCmd, err))
		return
	}

	log.Printf("container '%v' finished successfully: %v", containerID, runCmd)
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
	s.cancelFunc(errors.New("aborted by user"))
	err := s.virt.Stop(context.Background(), s.runCmd)
	if err == nil {
		log.Printf("stopped VM gracefully")
		return nil
	}
	log.Printf("failed to stop container gracefully '%v': %v\n", s.runCmd, err)
	err = s.runCmd.Process.Kill()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	log.Printf("waiting for lifetimeCtx...\n")
	<-s.lifetimeCtx.Done()
	return s.virt.Destroy(context.Background(), s.Status().ContainerID)
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
			})
		}
	}
}

func (s *VM) Matches(namespace, name string) bool {
	return s.pod.Namespace == namespace && s.pod.Name == name
}

func (s *VM) GetPod() *v1.Pod {
	return s.pod
}
