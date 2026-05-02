package watcher

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"
)

func TestStopDoesNotWaitForeverForReaderGoroutine(t *testing.T) {
	var logs bytes.Buffer
	w := NewNetworkWatcher("", nil, nil, log.New(&logs, "", 0))
	w.stopWait = 10 * time.Millisecond
	w.stopCh = make(chan struct{})
	done := make(chan struct{})
	w.done = done

	stopped := make(chan struct{})
	go func() {
		w.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Stop should not wait indefinitely for the reader goroutine")
	}
	if !strings.Contains(logs.String(), "stop wait timed out") {
		t.Fatalf("expected timeout log, got %q", logs.String())
	}

	close(done)
}

func TestStopReturnsWhenReaderGoroutineStops(t *testing.T) {
	var logs bytes.Buffer
	done := make(chan struct{})
	close(done)
	w := NewNetworkWatcher("", nil, nil, log.New(&logs, "", 0))
	w.stopWait = time.Second
	w.stopCh = make(chan struct{})
	w.done = done

	w.Stop()

	if !strings.Contains(logs.String(), "stopped") {
		t.Fatalf("expected stopped log, got %q", logs.String())
	}
}
