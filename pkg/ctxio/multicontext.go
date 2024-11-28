package ctxio

import (
	"context"
	"sync"
)

// MultiContext returns a context that is canceled when any of the provided contexts are canceled.
func MultiContext(ctxs ...context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	var once sync.Once
	for _, c := range ctxs {
		c := c // Capture range variable
		go func() {
			select {
			case <-c.Done():
				once.Do(func() {
					cancel()
				})
			case <-ctx.Done():
				// The merged context was canceled elsewhere
			}
		}()
	}

	return ctx, cancel
}
