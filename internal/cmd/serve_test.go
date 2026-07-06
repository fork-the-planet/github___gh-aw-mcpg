package cmd

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
		client := &http.Client{Timeout: 100 * time.Millisecond}
		resp, reqErr := client.Get("http://" + listener.Addr().String())
		if reqErr != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Second, 20*time.Millisecond)

	cancel()
	require.NoError(t, <-errCh)
}

// TestServeAndWait_OnShutdownSignalCalled verifies that the optional
// onShutdownSignal callback is invoked when the context is cancelled.
func TestServeAndWait_OnShutdownSignalCalled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}

	var signalCalled bool
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveAndWait(
			ctx,
			cancel,
			httpServer,
			500*time.Millisecond,
			func() { signalCalled = true },
			func() error {
				return httpServer.Serve(listener)
			},
		)
	}()

	require.Eventually(t, func() bool {
		client := &http.Client{Timeout: 100 * time.Millisecond}
		resp, reqErr := client.Get("http://" + listener.Addr().String())
		if reqErr != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Second, 20*time.Millisecond)

	cancel()
	require.NoError(t, <-errCh)
	assert.True(t, signalCalled, "onShutdownSignal should have been called on shutdown")
}

func TestServeAndWait_ShutdownTimesOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	requestStarted := make(chan struct{})
	handlerDone := make(chan struct{})
	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/hang" {
				w.WriteHeader(http.StatusOK)
				return
			}

			close(requestStarted)
			<-r.Context().Done()
			close(handlerDone)
		}),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveAndWait(
			ctx,
			cancel,
			httpServer,
			50*time.Millisecond,
			nil,
			func() error {
				return httpServer.Serve(listener)
			},
		)
	}()

	readyClient := &http.Client{Timeout: 100 * time.Millisecond}
	require.Eventually(t, func() bool {
		resp, reqErr := readyClient.Get("http://" + listener.Addr().String())
		if reqErr != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Second, 20*time.Millisecond)

	requestDone := make(chan struct{})
	hangClient := &http.Client{Timeout: 5 * time.Second}
	go func() {
		defer close(requestDone)

		resp, reqErr := hangClient.Get("http://" + listener.Addr().String() + "/hang")
		if resp != nil {
			_ = resp.Body.Close()
		}
		assert.Error(t, reqErr)
	}()

	require.Eventually(t, func() bool {
		select {
		case <-requestStarted:
			return true
		default:
			return false
		}
	}, time.Second, 20*time.Millisecond)

	cancel()

	require.ErrorIs(t, <-errCh, context.DeadlineExceeded)
	require.Eventually(t, func() bool {
		select {
		case <-handlerDone:
			return true
		default:
			return false
		}
	}, time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		select {
		case <-requestDone:
			return true
		default:
			return false
		}
	}, time.Second, 20*time.Millisecond)
}

// TestServeAndWait_ServeFnError verifies that when serveFn returns an unexpected
// error (not http.ErrServerClosed), serveAndWait triggers context cancellation
// and propagates the error to the caller.
func TestServeAndWait_ServeFnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	httpServer := &http.Server{}
	serveErrExpected := errors.New("unexpected serve error")

	result := serveAndWait(
		ctx,
		cancel,
		httpServer,
		500*time.Millisecond,
		nil,
		func() error {
			return serveErrExpected
		},
	)

	require.Error(t, result)
	assert.ErrorIs(t, result, serveErrExpected, "unexpected serve error should be propagated")
	assert.ErrorIs(t, ctx.Err(), context.Canceled, "serveAndWait should cancel the context on unexpected serve error")
}
