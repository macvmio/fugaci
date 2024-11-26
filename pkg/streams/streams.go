package streams

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/macvmio/fugaci/pkg/ctxio"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
)

var _ api.AttachIO = (*FilesBasedStreams)(nil)

type FilesBasedStreams struct {
	stdinFile  *os.File
	stdoutFile *os.File
	stderrFile *os.File

	mu        sync.Mutex
	cleanupWG sync.WaitGroup
	cleanOnce sync.Once
}

func NewFilesBasedStreams(directory, prefix string) (*FilesBasedStreams, error) {
	stdinFile, err := os.CreateTemp(directory, prefix+"_vm_stdin_*.log")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary stdin file: %v", err)
	}

	stdoutFile, err := os.CreateTemp(directory, prefix+"_vm_stdout_*.log")
	if err != nil {
		stdinFile.Close()
		return nil, fmt.Errorf("error creating temporary stdout file: %v", err)
	}

	stderrFile, err := os.CreateTemp(directory, prefix+"_vm_stderr_*.log")
	if err != nil {
		stdinFile.Close()
		stdoutFile.Close()
		return nil, fmt.Errorf("error creating temporary stderr file: %v", err)
	}

	return &FilesBasedStreams{
		stdinFile:  stdinFile,
		stdoutFile: stdoutFile,
		stderrFile: stderrFile,
	}, nil
}

func (f *FilesBasedStreams) Stdin() io.Reader {
	return f.stdinFile
}

func (f *FilesBasedStreams) Stdout() io.WriteCloser {
	return f.stdoutFile
}

func (f *FilesBasedStreams) Stderr() io.WriteCloser {
	return f.stderrFile
}

func (f *FilesBasedStreams) TTY() bool {
	return false
}

func (f *FilesBasedStreams) Resize() <-chan api.TermSize {
	// TODO: implement if needed
	return nil
}

// Cleanup removes the temporary files created for stdin, stdout, and stderr.
// It is safe to call Cleanup multiple times concurrently.
func (f *FilesBasedStreams) Cleanup() error {
	var errs []error

	f.cleanOnce.Do(func() {
		f.mu.Lock()
		defer f.mu.Unlock()

		// Wait for any ongoing operations to finish
		f.cleanupWG.Wait()

		// Close and remove stdinFile
		if f.stdinFile != nil {
			if err := f.stdinFile.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close stdin file: %w", err))
			}
			if err := os.Remove(f.stdinFile.Name()); err != nil {
				errs = append(errs, fmt.Errorf("failed to remove stdin file: %w", err))
			}
			f.stdinFile = nil
		}

		// Close and remove stdoutFile
		if f.stdoutFile != nil {
			if err := f.stdoutFile.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close stdout file: %w", err))
			}
			if err := os.Remove(f.stdoutFile.Name()); err != nil {
				errs = append(errs, fmt.Errorf("failed to remove stdout file: %w", err))
			}
			f.stdoutFile = nil
		}

		// Close and remove stderrFile
		if f.stderrFile != nil {
			if err := f.stderrFile.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close stderr file: %w", err))
			}
			if err := os.Remove(f.stderrFile.Name()); err != nil {
				errs = append(errs, fmt.Errorf("failed to remove stderr file: %w", err))
			}
			f.stderrFile = nil
		}
	})

	if len(errs) > 0 {
		return fmt.Errorf("cleanup encountered errors: %v", errs)
	}
	return nil
}

func (f *FilesBasedStreams) Stream(ctx context.Context, attach api.AttachIO, loggerPrintf func(format string, v ...any)) error {
	f.cleanupWG.Add(1)
	allowableError := func(err error) bool {
		if err == nil {
			return true
		}
		return errors.Is(err, context.Canceled) || errors.Is(err, io.EOF)
	}
	go func() {
		defer f.cleanupWG.Done()
		// Start streaming stdout
		if attach.Stdout() != nil {
			if err := followFileStream(ctx, attach.Stdout(), f.stdoutFile.Name(), loggerPrintf); !allowableError(err) {
				loggerPrintf("Error streaming stdout: %v", err)
			}
		}
	}()

	f.cleanupWG.Add(1)
	go func() {
		defer f.cleanupWG.Done()
		// Start streaming stderr
		if attach.Stderr() != nil {
			if err := followFileStream(ctx, attach.Stderr(), f.stderrFile.Name(), loggerPrintf); !allowableError(err) {
				loggerPrintf("Error streaming stderr: %v", err)
			}
		}
	}()

	// Handle stdin
	if attach.Stdin() != nil {
		f.cleanupWG.Add(1)
		go func() {
			defer f.cleanupWG.Done()
			_, err := io.Copy(f.stdinFile, ctxio.NewContextPeriodicReader(ctx, 200*time.Millisecond, attach.Stdin()))
			if !allowableError(err) {
				loggerPrintf("Error streaming stdin: %v", err)
			}
		}()
	}

	// Wait for context cancellation
	<-ctx.Done()

	return nil
}

func followFileStream(ctx context.Context, writer io.Writer, filename string, loggerPrintf func(format string, v ...any)) error {
	if writer == nil {
		return fmt.Errorf("writer cannot be nil")
	}

	tailReader, err := ctxio.NewTailReader(ctx, filename)
	if err != nil {
		return fmt.Errorf("error creating tail reader: %w", err)
	}
	defer tailReader.Close() // Close when function exits
	_, err = io.Copy(writer, tailReader)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		loggerPrintf("Error during copy: %v", err)
	}
	return err
}
