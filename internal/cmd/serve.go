package cmd

import (
	"context"
	"errors"
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
	debugLog.Printf("Starting HTTP server: addr=%s, shutdownTimeout=%s", httpServer.Addr, timeout)
	serveErrCh := make(chan error, 1)
	go func() {
		err := serveFn()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			debugLog.Printf("HTTP server exited unexpectedly, triggering shutdown: %v", err)
			cancel()
		}
		serveErrCh <- err
	}()

	<-ctx.Done()
	debugLog.Print("Shutdown signal received, beginning graceful HTTP server shutdown")
	if onShutdownSignal != nil {
		onShutdownSignal()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
	defer shutdownCancel()

	shutdownErr := httpServer.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		debugLog.Printf("Graceful shutdown failed, forcing close: %v", shutdownErr)
		_ = httpServer.Close()
		select {
		case <-serveErrCh:
		case <-time.After(timeout):
		}
		return shutdownErr
	}

	debugLog.Print("HTTP server shut down gracefully")
	serveErr := <-serveErrCh
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return nil
}
