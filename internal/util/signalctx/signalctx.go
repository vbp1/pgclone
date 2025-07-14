package signalctx

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// WithSignals возвращает context, который отменяется при получении INT или TERM.
// Возвращает дочерний context с CancelFunc и отдельный channel, куда пишется полученный сигнал.
func WithSignals(parent context.Context) (ctx context.Context, cancel context.CancelFunc, sigCh <-chan os.Signal) {
	ctx, cancel = context.WithCancel(parent)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-ctx.Done():
			// already canceled
		case <-c:
			cancel()
		}
	}()

	return ctx, cancel, c
}
