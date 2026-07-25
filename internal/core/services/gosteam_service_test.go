package services

import (
	"context"
	"testing"
	"time"
)

func TestRetryDotaHelloUntilReadyStopsWhenCanceled(t *testing.T) {
	dc := &dotaGCClient{ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		retryDotaHelloUntilReady(ctx, dc)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Dota hello retry did not stop after cancellation")
	}
}
