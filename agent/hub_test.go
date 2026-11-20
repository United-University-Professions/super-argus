package main

import (
	"testing"
	"time"
)

func TestHubEvictsIdleShippers(t *testing.T) {
	h := NewHub("http://127.0.0.1:1", "k", nil)
	defer h.Close(t.Context())
	h.Send(map[string]string{"service": "a", "pod": "a-1"}, time.Now(), "x")
	h.Send(map[string]string{"service": "b", "pod": "b-1"}, time.Now(), "y")
	if n := len(h.ships); n != 2 {
		t.Fatalf("want 2 shippers, got %d", n)
	}
	if got := h.evictIdle(time.Now(), time.Hour); got != 0 {
		t.Fatalf("nothing is idle yet, evicted %d", got)
	}
	if got := h.evictIdle(time.Now().Add(2*time.Hour), time.Hour); got != 2 {
		t.Fatalf("want 2 evicted, got %d", got)
	}
	if n := len(h.ships) + len(h.last); n != 0 {
		t.Fatalf("maps not cleaned: %d", n)
	}
	// a stream that comes back gets a fresh shipper
	h.Send(map[string]string{"service": "a", "pod": "a-1"}, time.Now(), "z")
	if len(h.ships) != 1 {
		t.Fatalf("want shipper recreated, got %d", len(h.ships))
	}
}

func TestHubSkipsLinesOlderThanServerAccepts(t *testing.T) {
	h := NewHub("http://127.0.0.1:1", "k", nil)
	defer h.Close(t.Context())
	h.Send(map[string]string{"service": "a"}, time.Now().Add(-48*time.Hour), "old")
	if len(h.ships) != 0 {
		t.Fatal("stale line must not create a shipper or be queued")
	}
	h.Send(map[string]string{"service": "a"}, time.Now().Add(-time.Hour), "fresh")
	if len(h.ships) != 1 {
		t.Fatal("recent line should be shipped")
	}
}
