package streams

import (
	"bytes"
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/virtual-kubelet/virtual-kubelet/node/api"
)

func setupFilesBasedStreams(t *testing.T, allocateStdin, allocateTTY bool) *FilesBasedStreams {
	t.Helper()
	fbs, err := NewFilesBasedStreams(t.TempDir(), "test", allocateStdin, allocateTTY)
	if err != nil {
		t.Fatalf("Failed to create FilesBasedStreams: %v", err)
	}
	return fbs
}

func teardownFilesBasedStreams(t *testing.T, fbs *FilesBasedStreams) {
	t.Helper()
	if err := fbs.Close(); err != nil {
		t.Errorf("Failed to close FilesBasedStreams: %v", err)
	}
}

func TestNewFilesBasedStreams(t *testing.T) {
	fbs := setupFilesBasedStreams(t, true, true)
	defer teardownFilesBasedStreams(t, fbs)

	if fbs.stdoutFile == nil {
		t.Error("stdoutFile should not be nil")
	}
	if fbs.stderrFile == nil {
		t.Error("stderrFile should not be nil")
	}
	if fbs.stdinReader == nil || fbs.stdinWriter == nil {
		t.Error("stdinReader and stdinWriter should not be nil when allocateStdin is true")
	}
	if fbs.allocateTTY && fbs.termSizeCh == nil {
		t.Error("termSizeCh should not be nil when allocateTTY is true")
	}
}

func TestStreamStdout(t *testing.T) {
	tests := []struct {
		name           string
		writeData      bool
		expectedOutput string
	}{
		{
			name:           "WithData",
			writeData:      true,
			expectedOutput: "Hello, stdout!",
		},
		{
			name:           "WithoutData",
			writeData:      false,
			expectedOutput: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fbs := setupFilesBasedStreams(t, false, false)
			defer teardownFilesBasedStreams(t, fbs)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Prepare mock attachIO
			stdoutBuf := &bytes.Buffer{}
			attachIO := &MockAttachIO{
				stdout: stdoutBuf,
			}

			if tt.writeData {
				// Write data to stdoutFile if applicable
				_, err := fbs.stdoutFile.WriteString(tt.expectedOutput + "\n")
				if err != nil {
					t.Fatalf("Failed to write to stdoutFile: %v", err)
				}
			}

			// Start streaming
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := fbs.Stream(ctx, attachIO, t.Logf)
				require.NoError(t, err)
			}()

			// Give some time for the data to be streamed
			time.Sleep(25 * time.Millisecond)

			// Cancel context to stop streaming
			cancel()
			wg.Wait()

			// Verify the received data
			output := stdoutBuf.String()
			if tt.writeData {
				if !strings.Contains(output, tt.expectedOutput) {
					t.Errorf("Expected output %q in stdout, got %q", tt.expectedOutput, output)
				}
			} else {
				if output != "" {
					t.Errorf("Expected no output in stdout, got %q", output)
				}
			}
		})
	}
}

func TestStreamStderr(t *testing.T) {
	tests := []struct {
		name           string
		writeData      bool
		expectedOutput string
	}{
		{
			name:           "WithData",
			writeData:      true,
			expectedOutput: "Hello, stderr!",
		},
		{
			name:           "WithoutData",
			writeData:      false,
			expectedOutput: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fbs := setupFilesBasedStreams(t, false, false)
			defer teardownFilesBasedStreams(t, fbs)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Prepare mock attachIO
			stderrBuf := &bytes.Buffer{}
			attachIO := &MockAttachIO{
				stderr: stderrBuf,
			}

			if tt.writeData {
				// Write data to stderrFile if applicable
				_, err := fbs.stderrFile.WriteString(tt.expectedOutput + "\n")
				if err != nil {
					t.Fatalf("Failed to write to stderrFile: %v", err)
				}
			}

			// Start streaming
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := fbs.Stream(ctx, attachIO, t.Logf)
				require.NoError(t, err)
			}()

			// Give some time for the data to be streamed
			time.Sleep(25 * time.Millisecond)

			// Cancel context to stop streaming
			cancel()
			wg.Wait()

			// Verify the received data
			output := stderrBuf.String()
			if tt.writeData {
				if !strings.Contains(output, tt.expectedOutput) {
					t.Errorf("Expected output %q in stderr, got %q", tt.expectedOutput, output)
				}
			} else {
				if output != "" {
					t.Errorf("Expected no output in stderr, got %q", output)
				}
			}
		})
	}
}

func TestStreamStdin(t *testing.T) {
	fbs := setupFilesBasedStreams(t, true, false)
	defer teardownFilesBasedStreams(t, fbs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare mock attachIO
	stdinData := "Hello, stdin!\n"
	stdinBuf := bytes.NewBufferString(stdinData)
	attachIO := &MockAttachIO{
		stdin: stdinBuf,
	}

	// Start streaming
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := fbs.Stream(ctx, attachIO, t.Logf)
		require.NoError(t, err)
	}()

	// Read data from stdinReader
	receivedData := make([]byte, len(stdinData))
	_, err := io.ReadFull(fbs.stdinReader, receivedData)
	if err != nil {
		t.Fatalf("Failed to read from stdinReader: %v", err)
	}

	// Verify that data matches
	if string(receivedData) != stdinData {
		t.Errorf("Expected input %q, got %q", stdinData, string(receivedData))
	}

	// Cancel context to stop streaming
	cancel()
	wg.Wait()
}

func TestContextCancellation(t *testing.T) {
	fbs := setupFilesBasedStreams(t, true, false)
	defer teardownFilesBasedStreams(t, fbs)

	ctx, cancel := context.WithCancel(context.Background())

	// Prepare mock attachIO
	attachIO := &MockAttachIO{
		stdin:  &bytes.Buffer{},
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	}

	// Start streaming
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		err := fbs.Stream(ctx, attachIO, t.Logf)
		require.NoError(t, err)
	}()

	// Cancel context after a short delay
	time.Sleep(25 * time.Millisecond)
	cancel()

	// Wait for Stream to exit
	select {
	case <-doneCh:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Stream did not exit after context cancellation")
	}
}

func TestCloseWithoutAnyStreamCalls(t *testing.T) {
	fbs := setupFilesBasedStreams(t, true, false)

	err := fbs.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	// Verify that files are closed and removed
	if _, err := os.Stat(fbs.stdoutFile.Name()); !os.IsNotExist(err) {
		t.Errorf("stdoutFile was not removed")
	}
	if _, err := os.Stat(fbs.stderrFile.Name()); !os.IsNotExist(err) {
		t.Errorf("stderrFile was not removed")
	}
}

func TestTTYResizeEvents(t *testing.T) {
	fbs := setupFilesBasedStreams(t, false, true)
	defer teardownFilesBasedStreams(t, fbs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare mock attachIO
	resizeCh := make(chan api.TermSize, 1)
	attachIO := &MockAttachIO{
		stdout:   &bytes.Buffer{},
		resizeCh: resizeCh,
		tty:      true,
	}

	// Start streaming
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := fbs.Stream(ctx, attachIO, t.Logf)
		require.NoError(t, err)
	}()

	// Send a resize event
	expectedSize := api.TermSize{Width: 80, Height: 24}
	resizeCh <- expectedSize

	// Receive the resize event
	select {
	case receivedSize, ok := <-fbs.Resize():
		if !ok {
			t.Error("termSizeCh was closed unexpectedly")
		}
		if receivedSize != expectedSize {
			t.Errorf("Expected term size %v, got %v", expectedSize, receivedSize)
		}
	case <-time.After(1 * time.Second):
		t.Error("Did not receive term size event")
	}

	// Close resize channel to simulate end of events
	close(resizeCh)

	// Cancel context to stop streaming
	cancel()
	wg.Wait()
}

func TestGoroutinesExit(t *testing.T) {
	fbs := setupFilesBasedStreams(t, false, false)
	defer teardownFilesBasedStreams(t, fbs)

	ctx, cancel := context.WithCancel(context.Background())

	// Prepare mock attachIO
	attachIO := &MockAttachIO{
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	}

	// Start streaming
	go func() {
		_ = fbs.Stream(ctx, attachIO, t.Logf)
	}()

	// Cancel context to stop streaming
	cancel()
	// Will be closed by teardown
}

func TestStreamStdoutError(t *testing.T) {
	fbs := setupFilesBasedStreams(t, false, false)
	defer teardownFilesBasedStreams(t, fbs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare mock attachIO with an ErrorWriter
	errWriter := &ErrorWriter{Err: fmt.Errorf("write error")}
	attachIO := &MockAttachIO{
		stdout: errWriter,
		stderr: &bytes.Buffer{},
	}

	_, err := fbs.stdoutFile.Write([]byte("Hello, stdout!"))
	if err != nil {
		t.Errorf("Failed to write to stdoutFile: %v", err)
	}
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()
	// Start streaming
	err = fbs.Stream(ctx, attachIO, t.Logf)
	if err == nil {
		t.Error("Expected error from Stream, got nil")
	}
}

func TestStreamStderrError(t *testing.T) {
	fbs := setupFilesBasedStreams(t, false, false)
	defer teardownFilesBasedStreams(t, fbs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare mock attachIO with an ErrorWriter
	errWriter := &ErrorWriter{Err: fmt.Errorf("write error")}
	attachIO := &MockAttachIO{
		stdout: &bytes.Buffer{},
		stderr: errWriter,
	}

	_, err := fbs.stderrFile.Write([]byte("Hello, stdout!"))
	if err != nil {
		t.Errorf("Failed to write to stdoutFile: %v", err)
	}
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()
	// Start streaming
	err = fbs.Stream(ctx, attachIO, t.Logf)
	if err == nil {
		t.Error("Expected error from Stream, got nil")
	}
}

func TestStreamStdinDisabled(t *testing.T) {
	// Create FilesBasedStreams with allocateStdin set to false (stdin streaming disabled)
	fbs := setupFilesBasedStreams(t, false, false) // allocateStdin = false, allocateTTY = false
	defer teardownFilesBasedStreams(t, fbs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare mock attachIO with stdin provided
	stdinBuf := bytes.NewBufferString("Test input data")
	attachIO := &MockAttachIO{
		stdin: stdinBuf,
	}

	// Attempt to start streaming
	err := fbs.Stream(ctx, attachIO, t.Logf)

	// Check for the expected error
	if err == nil {
		t.Error("Expected error when stdin streaming is disabled, but got nil")
	} else if !strings.Contains(err.Error(), "stdin streaming is disabled") {
		t.Errorf("Expected error 'stdin streaming is disabled', but got: %v", err)
	} else {
		t.Logf("Received expected error: %v", err)
	}
}

func TestStreamTTYDisabled(t *testing.T) {
	// Create FilesBasedStreams with allocateTTY set to false (TTY is disabled)
	fbs := setupFilesBasedStreams(t, false, false) // allocateStdin = false, allocateTTY = false
	defer teardownFilesBasedStreams(t, fbs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare mock attachIO with TTY set to true
	attachIO := &MockAttachIO{
		tty: true,
	}

	// Attempt to start streaming
	err := fbs.Stream(ctx, attachIO, t.Logf)

	// Check for the expected error
	if err == nil {
		t.Error("Expected error when TTY is disabled, but got nil")
	} else if !strings.Contains(err.Error(), "TTY is disabled") {
		t.Errorf("Expected error 'TTY is disabled', but got: %v", err)
	} else {
		t.Logf("Received expected error: %v", err)
	}
}

// MockAttachIO implements api.AttachIO for testing purposes
type MockAttachIO struct {
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	resizeCh chan api.TermSize
	tty      bool
}

func (m *MockAttachIO) Stdin() io.Reader {
	return m.stdin
}

func (m *MockAttachIO) Stdout() io.WriteCloser {
	if wc, ok := m.stdout.(io.WriteCloser); ok {
		return wc
	}
	return nopWriteCloser{m.stdout}
}

func (m *MockAttachIO) Stderr() io.WriteCloser {
	if wc, ok := m.stderr.(io.WriteCloser); ok {
		return wc
	}
	return nopWriteCloser{m.stderr}
}

func (m *MockAttachIO) TTY() bool {
	return m.tty
}

func (m *MockAttachIO) Resize() <-chan api.TermSize {
	return m.resizeCh
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

// ErrorWriter is an io.Writer that returns an error on Write
type ErrorWriter struct {
	Err error
}

func (w *ErrorWriter) Write(p []byte) (int, error) {
	return 0, w.Err
}
