package streams

import (
	"context"
	"errors"
	"fmt"
	"github.com/macvmio/fugaci/pkg/ctxio"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"golang.org/x/sync/errgroup"
	"io"
	"os"
	"sync"
)

var _ api.AttachIO = (*FilesBasedStreams)(nil)

type FilesBasedStreams struct {
	stdoutFile *os.File
	stderrFile *os.File

	stdinReader *os.File
	stdinWriter *os.File

	allocateTTY bool
	termSizeCh  chan api.TermSize

	mu        sync.Mutex
	cleanupWG sync.WaitGroup
	cleanOnce sync.Once
}

func NewFilesBasedStreams(directory, prefix string, allocateStdin, allocateTTY bool) (*FilesBasedStreams, error) {
	var err error
	f := FilesBasedStreams{allocateTTY: allocateTTY}
	if allocateTTY {
		f.termSizeCh = make(chan api.TermSize)
	}
	f.stdoutFile, err = os.CreateTemp(directory, prefix+"_vm_stdout_*.log")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary stdout file: %v", err)
	}

	f.stderrFile, err = os.CreateTemp(directory, prefix+"_vm_stderr_*.log")
	if err != nil {
		f.stdoutFile.Close()
		return nil, fmt.Errorf("error creating temporary stderr file: %v", err)
	}

	if allocateStdin {
		f.stdinReader, f.stdinWriter, err = os.Pipe()
		if err != nil {
			return nil, fmt.Errorf("error creating stdin pipe: %v", err)
		}
	}
	return &f, nil
}

func (f *FilesBasedStreams) Stdin() io.Reader {
	return f.stdinReader
}

func (f *FilesBasedStreams) Stdout() io.WriteCloser {
	return f.stdoutFile
}

func (f *FilesBasedStreams) Stderr() io.WriteCloser {
	return f.stderrFile
}

func (f *FilesBasedStreams) TTY() bool {
	return f.allocateTTY
}

func (f *FilesBasedStreams) Resize() <-chan api.TermSize {
	return f.termSizeCh
}

// Close removes the temporary files created for stdin, stdout, and stderr.
// It is safe to call Close multiple times concurrently.
func (f *FilesBasedStreams) Close() error {
	var errs []error
	// Wait for any ongoing operations to finish
	f.cleanupWG.Wait()

	if err := f.stdoutFile.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close stdout file: %w", err))
	}
	if err := os.Remove(f.stdoutFile.Name()); err != nil {
		errs = append(errs, fmt.Errorf("failed to remove stdout file: %w", err))
	}

	if err := f.stderrFile.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close stderr file: %w", err))
	}
	if err := os.Remove(f.stderrFile.Name()); err != nil {
		errs = append(errs, fmt.Errorf("failed to remove stderr file: %w", err))
	}

	// Close termSizeCh
	if f.termSizeCh != nil {
		close(f.termSizeCh)
		f.termSizeCh = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup encountered errors: %v", errs)
	}
	return nil
}

func (f *FilesBasedStreams) Stream(ctx context.Context, attach api.AttachIO, loggerPrintf func(format string, v ...any)) error {
	// Create an errgroup with the provided context
	eg, ctx := errgroup.WithContext(ctx)

	// Start streaming stdout
	if attach.Stdout() != nil {
		f.cleanupWG.Add(1)
		eg.Go(func() error {
			defer f.cleanupWG.Done()
			err := followFileStream(ctx, attach.Stdout(), f.stdoutFile.Name(), loggerPrintf)
			loggerPrintf("stdout copy completed")
			return err
		})
	}

	// Start streaming stderr
	if attach.Stderr() != nil {
		f.cleanupWG.Add(1)
		eg.Go(func() error {
			defer f.cleanupWG.Done()
			err := followFileStream(ctx, attach.Stderr(), f.stderrFile.Name(), loggerPrintf)
			loggerPrintf("stderr copy completed")
			return err
		})
	}

	// Handle stdin
	if f.stdinWriter != nil && attach.Stdin() != nil {
		f.cleanupWG.Add(1)
		go func() {
			defer f.cleanupWG.Done()
			// TODO: This blocks until if stdin has no data, even if context is cancelled
			_, err := io.Copy(f.stdinWriter, attach.Stdin())
			loggerPrintf("stdin copy completed: %v", err)
		}()
	}

	// Handle TTY resize events
	if attach.TTY() {
		f.cleanupWG.Add(1)
		eg.Go(func() error {
			defer f.cleanupWG.Done()
			defer loggerPrintf("attach tty channel completed")
			for {
				select {
				case termSize, ok := <-attach.Resize():
					if !ok {
						// The attach.Resize() channel is closed
						return nil
					}
					select {
					case f.termSizeCh <- termSize:
					case <-ctx.Done():
						return ctx.Err()
					}
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		})
	}

	loggerPrintf("waiting for Stream() to finish")
	err := eg.Wait()
	loggerPrintf("Stream() has completed")
	return err
}

func followFileStream(ctx context.Context, writer io.WriteCloser, filename string, loggerPrintf func(format string, v ...any)) error {
	if writer == nil {
		return fmt.Errorf("writer cannot be nil")
	}
	defer writer.Close()

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
