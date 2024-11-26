package ctxio

import (
	"context"
	"io"
	"time"
)

// NewContextPeriodicReader Creates Reader that reads until context is cancelled,
// and resumes after 'period' if underlying reader returns EOF
func NewContextPeriodicReader(ctx context.Context, period time.Duration, r io.Reader) *ContextReader {
	return &ContextReader{
		ctx:    ctx,
		reader: r,
		period: period,
	}
}

// ContextReader is a custom reader that checks for context cancellation
// and handles EOF without stopping the copy operation.
type ContextReader struct {
	ctx    context.Context
	reader io.Reader
	period time.Duration
}

func (cr *ContextReader) Read(p []byte) (int, error) {
	select {
	case <-cr.ctx.Done():
		// Context has been canceled
		return 0, cr.ctx.Err()
	default:
		n, err := cr.reader.Read(p)
		if err == io.EOF {
			// EOF encountered, but we want to keep reading
			time.Sleep(cr.period)
			return 0, nil // Return no data, no error to continue io.Copy
		}
		return n, err
	}
}
