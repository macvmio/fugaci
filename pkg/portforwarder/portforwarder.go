package portforwarder

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type PortForwarder struct {
}

func NewPortForwarder() *PortForwarder {
	return &PortForwarder{}
}

func (s *PortForwarder) PortForward(ctx context.Context, address string, stream io.ReadWriteCloser) error {
	// Step 1: Establish a Connection to the VM
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", address, err)
	}
	defer conn.Close()

	// Step 2: Forward Data Between the Streams
	// Use a WaitGroup to wait for both directions to finish
	var wg sync.WaitGroup
	wg.Add(2)

	// Context to handle cancellation
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Function to copy data and handle errors
	copyFunc := func(dst io.Writer, src io.Reader) {
		defer wg.Done()
		_, err := io.Copy(dst, src)
		if err != nil && err != io.EOF {
			// Log the error, but don't terminate immediately
			fmt.Printf("Data copy error: %v\n", err)
		}
		// Cancel the context to stop the other copy
		cancel()
	}

	// Start copying data in both directions
	go copyFunc(conn, stream) // from Kubernetes client to VM
	go copyFunc(stream, conn) // from VM to Kubernetes client

	// Step 3: Wait for Completion or Context Cancellation
	doneChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-ctx.Done():
		// Context cancelled or timeout
		return ctx.Err()
	case <-doneChan:
		// Port forwarding completed successfully
	}
	return nil
}
