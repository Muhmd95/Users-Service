package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"svc-users/internal/users"
	"svc-users/util/logger"
)

type shutdownTimeouts struct {
	http   time.Duration
	email  time.Duration
	cancel time.Duration
}

var defaultShutdownTimeouts = shutdownTimeouts{
	http:   10 * time.Second,
	email:  5 * time.Second,
	cancel: 5 * time.Second,
}

const dependencyShutdownTimeout = 5 * time.Second

func serve(ctx context.Context, server *http.Server, listener net.Listener, emailJobs chan users.EmailJob, worker func(context.Context), timeouts shutdownTimeouts) error {
	// Request cancellation is delayed until HTTP draining ends. The worker has
	// its own context so a termination signal does not discard queued jobs.
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	server.BaseContext = func(net.Listener) context.Context { return requestCtx }
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker(workerCtx)
	}()

	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()

	var result error
	// blocks until updates from the server
	select {
	case <-ctx.Done():
		logger.Log.Info().Msg("Shutting down svc-users...")
	case err := <-serverDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			result = fmt.Errorf("HTTP server: %w", err)
		}
	}

	httpCtx, cancelHTTP := context.WithTimeout(context.Background(), timeouts.http)
	// this shutdowns the server gracfully
	err := server.Shutdown(httpCtx)
	cancelHTTP()
	if err != nil {
		result = errors.Join(result, fmt.Errorf("drain HTTP requests: %w", err))
		logger.Log.Warn().Err(err).Msg("HTTP drain timed out; canceling requests and closing connections")
		cancelRequests()
		if closeErr := server.Close(); closeErr != nil {
			result = errors.Join(result, fmt.Errorf("close HTTP server: %w", closeErr))
		}
		// Close does not wait for handler goroutines. Keep the channel open so
		// a late registration cannot panic by sending to a closed queue.
		// 		cancelWorker()
		// Signals that the email worker should stop as well.
		// Notice that we do not close emailJobs in this branch.
		// server.Close() does not wait for every handler goroutine to return. A handler could still attempt to enqueue an email.
		// Sending into a closed channel causes a panic:
		cancelWorker()
	} else {
		// Shutdown has waited for handlers, so no request can enqueue more jobs.
		// closing up a buffered channel dont delete the jobs in it the worker can still read them until the chan is empty
		close(emailJobs)
		logger.Log.Info().Msg("HTTP requests drained; draining queued emails")
	}

	//waits for the worker to be done from the defer in the define of the worker up
	emailCtx, cancelEmail := context.WithTimeout(context.Background(), timeouts.email)
	select {
	case <-workerDone:
		cancelEmail()
		return result
	// the email context timed out so force its close
	case <-emailCtx.Done():
		result = errors.Join(result, fmt.Errorf("drain email worker: %w", emailCtx.Err()))
		cancelEmail()
		logger.Log.Warn().Msg("Email drain timed out; canceling remaining email work")
		cancelWorker()
	}

	// the worker didnt close properly
	// give it some time to stop then return
	cancelCtx, cancelWait := context.WithTimeout(context.Background(), timeouts.cancel)
	defer cancelWait()
	select {
	case <-workerDone:
	case <-cancelCtx.Done():
		result = errors.Join(result, fmt.Errorf("stop email worker: %w", cancelCtx.Err()))
	}
	return result
}

func closeWithTimeout(name string, closeResource func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), dependencyShutdownTimeout)
	defer cancel()
	if err := closeResource(ctx); err != nil {
		logger.Log.Error().Err(err).Str("dependency", name).Msg("Dependency shutdown failed")
	}
}
