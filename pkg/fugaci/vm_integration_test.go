//go:build integration
// +build integration

package fugaci

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/tomekjarosik/fugaci/pkg/curie"
	"github.com/tomekjarosik/fugaci/pkg/sshrunner"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"testing"
	"time"
)

func TestVMIntegration_StartAndCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Define a basic pod spec with a container for the VM
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "test-container",
					Image: "macvm.store/repo/base-runner:1.6",
					Env: []v1.EnvVar{
						{
							Name:  SshUsernameEnvVar,
							Value: "agent",
						},
						{
							Name:  SshPasswordEnvVar,
							Value: "password",
						},
					},
					ImagePullPolicy: v1.PullNever,
					Command:         []string{"echo", "test123"},
				},
			},
		},
	}

	// Create a new VM instance
	virt := curie.NewVirtualization("/Users/tomek/src/curie-main/.build/arm64-apple-macosx/debug/curie")
	puller := NewGeranosPuller("notimportant")
	sshRunner := sshrunner.NewRunner()
	vm, err := NewVM(ctx, virt, puller, sshRunner, pod, 0)
	if err != nil {
		t.Fatalf("Failed to create VM: %v", err)
	}

	// Start the VM in a separate goroutine
	go vm.Run()

	for {
		if vm.GetContainerStatus().Ready {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	log.Printf("VM assigned IP: %s", vm.GetPod().Status.PodIP)

	// Check the VM status to ensure it's running
	assert.True(t, vm.IsReady(), "VM should be marked as ready after IP assignment")

	// Perform cleanup of the VM
	err = vm.Cleanup()
	if err != nil {
		t.Fatalf("Failed to clean up VM: %v", err)
	}

	// Ensure VM has been cleaned up properly
	assert.False(t, vm.IsReady(), "VM should not be marked as ready after cleanup")
}
