package cmd

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServeAndWait_GracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveAndWait(
			ctx,
			cancel,
			httpServer,
			500*time.Millisecond,
			nil,
			func() error {
				return httpServer.Serve(listener)
			},
		)
	}()

	require.Eventually(t, func() bool {
		conn, dialErr := net.DialTimeout("tcp", listener.Addr().String(), 50*time.Millisecond)
		if dialErr != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, time.Second, 20*time.Millisecond)

	cancel()
	require.NoError(t, <-errCh)
}
