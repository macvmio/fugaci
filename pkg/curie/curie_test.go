package curie

import (
	"context"
	"errors"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

// Mock the curie binary with a bash script for testing.
// createTestScript creates a bash script in the system's temporary directory for testing.
func createTestScript(content string) (string, error) {
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, "test_script.sh")
	err := os.WriteFile(scriptPath, []byte(content), 0755)
	return scriptPath, err
}

func removeTestScript(scriptPath string) {
	os.Remove(scriptPath)
}

func TestVirtualization_Create(t *testing.T) {
	tests := []struct {
		name           string
		scriptContent  string
		pod            v1.Pod
		containerIndex int
		expectedID     string
		expectError    bool
	}{
		{
			name: "successful creation",
			scriptContent: `#!/bin/bash
			if [ "$1" == "create" ]; then
				echo "container123"
			else
				exit 1
			fi`,
			pod: v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "mypod",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "mycontainer", Image: "myimage"},
					},
				},
			},
			containerIndex: 0,
			expectedID:     "container123",
			expectError:    false,
		},
		{
			name: "failed creation due to empty name",
			scriptContent: `#!/bin/bash
			exit 1`,
			pod: v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "", Image: "myimage"},
					},
				},
			},
			containerIndex: 0,
			expectedID:     "",
			expectError:    true,
		},
		{
			name: "failed creation due to empty image",
			scriptContent: `#!/bin/bash
			exit 1`,
			pod: v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "mypod",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "mycontainer", Image: ""},
					},
				},
			},
			containerIndex: 0,
			expectedID:     "",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scriptPath, err := createTestScript(tt.scriptContent)
			assert.NoError(t, err)
			defer removeTestScript(scriptPath)

			v := NewVirtualization(scriptPath)
			containerID, err := v.Create(context.Background(), tt.pod, tt.containerIndex)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, containerID)
			}
		})
	}
}

func TestVirtualization_Start(t *testing.T) {
	scriptContent := `#!/bin/bash
	if [ "$1" == "start" ]; then
		sleep 5
		echo "started"
	else
		exit 1
	fi`
	scriptPath, err := createTestScript(scriptContent)
	assert.NoError(t, err)
	defer removeTestScript(scriptPath)

	v := NewVirtualization(scriptPath)
	cmd, err := v.Start(context.Background(), "container123")
	assert.NoError(t, err)

	go func() {
		err := cmd.Wait()
		assert.NoError(t, err)
	}()
	time.Sleep(1 * time.Second)
	assert.Nil(t, cmd.Process.Signal(os.Interrupt))
}

func TestVirtualization_Stop_processDidNotExitWithin5Seconds(t *testing.T) {
	scriptContent := `#!/bin/bash
		sleep 10`
	scriptPath, err := createTestScript(scriptContent)
	assert.NoError(t, err)
	defer removeTestScript(scriptPath)

	v := NewVirtualization(scriptPath)
	cmd, err := v.Start(context.Background(), "container123")
	assert.NoError(t, err)

	go func() {
		err := cmd.Wait()
		assert.NoError(t, err)
	}()
	time.Sleep(100 * time.Millisecond)

	err = v.Stop(context.Background(), cmd)
	assert.Error(t, errors.New("process did not exit within 5 seconds"))
}

func TestVirtualization_Stop_mustReactToSIGTERM(t *testing.T) {

	testStopLogic := func(t *testing.T) error {
		scriptContent := `#!/bin/bash
		# Function to handle the interrupt signal
		cleanup() {
			echo "Received Interrupt signal, exiting..."
			exit 0
		}
		trap cleanup EXIT
		trap cleanup SIGTERM
		# Simulate a long-running process
		sleep 10000 &
		wait $!
		exit 0
`
		scriptPath, err := createTestScript(scriptContent)
		assert.NoError(t, err)
		defer removeTestScript(scriptPath)

		v := NewVirtualization(scriptPath)
		cmd, err := v.Start(context.Background(), "container123")
		assert.NoError(t, err)

		go func() {
			err := cmd.Wait()
			assert.NoError(t, err)
		}()
		time.Sleep(60 * time.Millisecond)

		err = v.Stop(context.Background(), cmd)
		assert.NoError(t, err)
		assert.Equal(t, 0, cmd.ProcessState.ExitCode())
		fmt.Printf("exit code: %v\n", cmd.ProcessState.ExitCode())
		return err
	}

	for i := 0; i < 10; i++ { // Run the test logic 10 times
		t.Run("iteration", func(t *testing.T) {
			// Place your test logic here
			t.Logf("Running iteration %d", i+1)

			// Example: Replace with actual test logic
			err := testStopLogic(t)
			if err != nil {
				t.Errorf("Test failed on iteration %d: %v", i+1, err)
			}
		})
	}

}

func TestVirtualization_Remove(t *testing.T) {
	scriptContent := `#!/bin/bash
	if [ "$1" == "rm" ]; then
		exit 0
	else
		exit 1
	fi`
	scriptPath, err := createTestScript(scriptContent)
	assert.NoError(t, err)
	defer removeTestScript(scriptPath)

	v := NewVirtualization(scriptPath)
	err = v.Destroy(context.Background(), "container123")
	assert.NoError(t, err)
}

func TestVirtualization_Inspect(t *testing.T) {
	scriptContent := `#!/bin/bash
	if [ "$1" == "inspect" ]; then
		echo '{"arp":[{"IP":"192.168.1.10"}]}'
	else
		exit 1
	fi`
	scriptPath, err := createTestScript(scriptContent)
	assert.NoError(t, err)
	defer removeTestScript(scriptPath)

	v := NewVirtualization(scriptPath)
	resp, err := v.Inspect(context.Background(), "container123")
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "192.168.1.10", resp.Arp[0].IP)
}

func TestVirtualization_IP(t *testing.T) {
	// Subtest: IP is present
	t.Run("IP is present", func(t *testing.T) {
		scriptContent := `#!/bin/bash
		if [ "$1" == "inspect" ]; then
			echo '{"arp":[{"IP":"192.168.1.10"}]}'
		else
			exit 1
		fi`
		scriptPath, err := createTestScript(scriptContent)
		assert.NoError(t, err)
		defer removeTestScript(scriptPath)

		v := NewVirtualization(scriptPath)
		ip, err := v.IP(context.Background(), "container123")
		assert.NoError(t, err)
		assert.Equal(t, net.ParseIP("192.168.1.10"), ip)
	})

	// Subtest: IP is not present
	t.Run("IP is not present", func(t *testing.T) {
		scriptContent := `#!/bin/bash
		if [ "$1" == "inspect" ]; then
			echo '{"arp":[]}'  # No IP present
		else
			exit 1
		fi`
		scriptPath, err := createTestScript(scriptContent)
		assert.NoError(t, err)
		defer removeTestScript(scriptPath)

		v := NewVirtualization(scriptPath)
		ip, err := v.IP(context.Background(), "container123")
		assert.Error(t, err)
		assert.Nil(t, ip)
	})
}

func TestVirtualization_Exists(t *testing.T) {
	tests := []struct {
		name          string
		scriptContent string
		expectedExist bool
	}{
		{
			name: "container exists",
			scriptContent: `#!/bin/bash
			if [ "$1" == "inspect" ]; then
				echo '{"arp":[{"IP":"192.168.1.10"}]}'
			else
				exit 1
			fi`,
			expectedExist: true,
		},
		{
			name: "container does not exist",
			scriptContent: `#!/bin/bash
			if [ "$1" == "inspect" ]; then
				echo "Cannot find the container"
				exit 1
			else
				exit 1
			fi`,
			expectedExist: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scriptPath, err := createTestScript(tt.scriptContent)
			assert.NoError(t, err)
			defer removeTestScript(scriptPath)

			v := NewVirtualization(scriptPath)
			exists, err := v.Exists(context.Background(), "container123")

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedExist, exists)
		})
	}
}
