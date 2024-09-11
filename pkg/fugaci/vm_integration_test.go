//go:build integration
// +build integration

package fugaci

import (
	"context"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"testing"
	"time"
)

func TestVMIntegration_StartAndCleanup(t *testing.T) {
	// Set up the context and cancel function for VM lifetime
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Assume you have real implementations of the interfaces
	var virt Virtualization
	var puller Puller
	var sshRunner SSHRunner

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
					Image: "your-image:latest", // Replace with your actual image
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
				},
			},
		},
	}

	// Create a new VM instance
	vm, err := NewVM(ctx, virt, puller, sshRunner, pod, 0)
	if err != nil {
		t.Fatalf("Failed to create VM: %v", err)
	}

	// Start the VM in a separate goroutine
	go vm.Run()

	// Wait for the VM to assign an IP address (this will block until it gets an IP or context timeout)
	ip := vm.WaitForIP(ctx, vm.GetContainerStatus().ContainerID)
	if ip == nil {
		t.Fatalf("Failed to assign IP to the VM")
	}

	log.Printf("VM assigned IP: %s", ip.String())

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
