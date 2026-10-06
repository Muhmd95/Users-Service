package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"svc-users/internal/users"
)

func testListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func await(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for shutdown test")
	}
}

func awaitResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server result")
		return nil
	}
}

func TestShutdownFinishesRequestsBeforeDrainingEmails(t *testing.T) {
	listener := testListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := make(chan users.EmailJob, 2)
	queue <- users.EmailJob{Token: "queued"}
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	allowWorker := make(chan struct{})
	draining := make(chan struct{})
	requestContext := make(chan error, 1)
	processed := make(chan string, 2)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseRequest
		requestContext <- r.Context().Err()
		queue <- users.EmailJob{Token: "last-request"}
		w.WriteHeader(http.StatusNoContent)
	})}
	server.RegisterOnShutdown(func() { close(draining) })
	worker := func(ctx context.Context) {
		select {
		case <-allowWorker:
		case <-ctx.Done():
			return
		}
		for job := range queue {
			processed <- job.Token
		}
	}
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, server, listener, queue, worker, shutdownTimeouts{time.Second, time.Second, time.Second})
	}()
	response := make(chan error, 1)
	go func() {
		resp, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + listener.Addr().String())
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				err = errors.New("unexpected HTTP status")
			}
		}
		response <- err
	}()
	await(t, requestStarted)
	cancel()
	await(t, draining)
	select {
	case err := <-result:
		t.Fatalf("shutdown returned while a request was active: %v", err)
	default:
	}
	close(releaseRequest)
	if err := awaitResult(t, response); err != nil {
		t.Fatal(err)
	}
	if err := <-requestContext; err != nil {
		t.Fatalf("graceful shutdown canceled an active request: %v", err)
	}
	close(allowWorker)
	if err := awaitResult(t, result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"queued", "last-request"} {
		select {
		case got := <-processed:
			if got != want {
				t.Fatalf("processed %q; want %q", got, want)
			}
		default:
			t.Fatalf("email %q was not drained", want)
		}
	}
}

func TestHTTPTimeoutCancelsRequestsWithoutClosingTheirQueue(t *testing.T) {
	listener := testListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := make(chan users.EmailJob, 1)
	started := make(chan struct{})
	requestCanceled := make(chan struct{})
	releaseLateSender := make(chan struct{})
	lateSend := make(chan any, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(requestCanceled)
		<-releaseLateSender
		defer func() { lateSend <- recover() }()
		queue <- users.EmailJob{Token: "late-registration"}
	})}
	workerStopped := make(chan struct{})
	worker := func(ctx context.Context) {
		defer close(workerStopped)
		<-ctx.Done()
	}
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, server, listener, queue, worker, shutdownTimeouts{20 * time.Millisecond, time.Second, time.Second})
	}()
	response := make(chan error, 1)
	go func() {
		resp, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + listener.Addr().String())
		if resp != nil {
			resp.Body.Close()
		}
		response <- err
	}()
	await(t, started)
	cancel()
	if err := awaitResult(t, result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v; want deadline exceeded", err)
	}
	await(t, requestCanceled)
	await(t, workerStopped)
	close(releaseLateSender)
	select {
	case panicValue := <-lateSend:
		if panicValue != nil {
			t.Fatalf("late queue send panicked: %v", panicValue)
		}
	case <-time.After(time.Second):
		t.Fatal("late queue send blocked")
	}
	if err := awaitResult(t, response); err == nil {
		t.Fatal("forced close left the HTTP request open")
	}
}

func TestEmailDrainTimeoutCancelsAndWaitsForWorker(t *testing.T) {
	listener := testListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stopped := make(chan struct{})
	worker := func(ctx context.Context) {
		<-ctx.Done()
		close(stopped)
	}
	err := serve(ctx, &http.Server{}, listener, make(chan users.EmailJob), worker,
		shutdownTimeouts{time.Second, 20 * time.Millisecond, time.Second})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v; want deadline exceeded", err)
	}
	await(t, stopped)
}

func TestHTTPServerFailureStopsWorker(t *testing.T) {
	listener := testListener(t)
	_ = listener.Close()
	stopped := make(chan struct{})
	queue := make(chan users.EmailJob)
	worker := func(ctx context.Context) {
		for range queue {
		}
		close(stopped)
	}
	err := serve(context.Background(), &http.Server{}, listener, queue, worker,
		shutdownTimeouts{time.Second, time.Second, time.Second})
	if err == nil {
		t.Fatal("server failure was not returned")
	}
	await(t, stopped)
}
