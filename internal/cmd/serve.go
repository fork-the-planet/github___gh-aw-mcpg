package cmd

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"
)

const httpServerShutdownTimeout = 5 * time.Second

// serveAndWait starts a server function in the background, waits for ctx cancellation,
// and then performs a graceful HTTP server shutdown with the provided timeout.
// If serveFn returns an unexpected error, cancel is invoked to trigger shutdown.
func serveAndWait(
	ctx context.Context,
	cancel context.CancelFunc,
	httpServer *http.Server,
	timeout time.Duration,
	onShutdownSignal func(),
	serveFn func() error,
) error {
	go func() {
		if err := serveFn(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server error: %v", err)
			cancel()
		}
	}()

	<-ctx.Done()
	if onShutdownSignal != nil {
		onShutdownSignal()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
		return err
	}

	return nil
}
