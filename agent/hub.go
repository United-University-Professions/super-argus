package main

import (
	"context"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cragone/Super-Argus/sdk/go/argus"
)

// maxShippers bounds memory if something produces unbounded distinct label sets.
const maxShippers = 500

const (
	// shipperQueueLines and shipperMaxLineBytes bound a stream's buffered memory
	// to ~8 MiB worst case (500 x 16 KiB), instead of 2000 x 64 KiB = 128 MiB.
	shipperQueueLines   = 500
	shipperMaxLineBytes = 16 << 10
	// shipperIdle is how long a stream may go without a line before its shipper
	// is flushed and freed. Pods come and go; their shippers must not pile up.
	shipperIdle = 10 * time.Minute
	// maxLineAge keeps lines the server would refuse anyway (it rejects entries
	// older than 24h) from being queued, e.g. when replaying a stale backlog.
	maxLineAge = 23 * time.Hour
)

// Hub fans lines out to one SDK shipper per distinct label set (a shipper
// carries a single stream's labels).
type Hub struct {
	url, key string
	extra    map[string]string // added to every stream, e.g. env, cluster

	client *http.Client // shared by every shipper so each stream doesn't hold its own transport

	mu      sync.Mutex
	ships   map[string]*argus.Shipper
	last    map[string]time.Time // last Send per stream, for idle eviction
	dropped int64
	stale   int64
	stop    chan struct{}
	once    sync.Once
}

func NewHub(url, key string, extra map[string]string) *Hub {
	h := &Hub{
		url: url, key: key, extra: extra,
		client: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConns:        4,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     60 * time.Second,
			TLSHandshakeTimeout: 5 * time.Second,
			ForceAttemptHTTP2:   true,
		}},
		ships: map[string]*argus.Shipper{},
		last:  map[string]time.Time{},
		stop:  make(chan struct{}),
	}
	go h.reap()
	return h
}

// reap periodically frees shippers whose stream has gone quiet.
func (h *Hub) reap() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-h.stop:
			return
		case now := <-t.C:
			h.evictIdle(now, shipperIdle)
		}
	}
}

// evictIdle flushes and removes shippers with no line for at least idle.
func (h *Hub) evictIdle(now time.Time, idle time.Duration) int {
	h.mu.Lock()
	var gone []*argus.Shipper
	for k, s := range h.ships {
		if now.Sub(h.last[k]) >= idle {
			gone = append(gone, s)
			delete(h.ships, k)
			delete(h.last, k)
		}
	}
	h.mu.Unlock()
	for _, s := range gone {
		go func(s *argus.Shipper) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s.Close(ctx)
		}(s)
	}
	return len(gone)
}

func labelKey(l map[string]string) string {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(l[k])
		b.WriteByte(',')
	}
	return b.String()
}

// Send ships one line under the given labels. It never blocks.
func (h *Hub) Send(labels map[string]string, ts time.Time, line string) {
	if !ts.IsZero() && time.Since(ts) > maxLineAge {
		h.mu.Lock()
		h.stale++
		n := h.stale
		h.mu.Unlock()
		if n == 1 || n%10000 == 0 {
			log.Printf("argus-agent: skipping lines older than %s the server would reject (%d skipped)", maxLineAge, n)
		}
		return
	}
	all := make(map[string]string, len(labels)+len(h.extra))
	for k, v := range h.extra {
		all[k] = v
	}
	for k, v := range labels {
		all[k] = v
	}
	key := labelKey(all)

	h.mu.Lock()
	s := h.ships[key]
	if s == nil {
		if len(h.ships) >= maxShippers {
			h.dropped++
			if h.dropped == 1 || h.dropped%10000 == 0 {
				log.Printf("argus-agent: %d label sets in use, dropping new ones (%d lines dropped)", maxShippers, h.dropped)
			}
			h.mu.Unlock()
			return
		}
		s = argus.New(argus.Config{URL: h.url, APIKey: h.key, Labels: all,
			QueueSize: shipperQueueLines, MaxLineBytes: shipperMaxLineBytes, Client: h.client})
		h.ships[key] = s
	}
	h.last[key] = time.Now()
	h.mu.Unlock()
	s.Send(ts, line)
}

// Close flushes every shipper, waiting up to ctx.
func (h *Hub) Close(ctx context.Context) {
	h.once.Do(func() { close(h.stop) })
	h.mu.Lock()
	ships := make([]*argus.Shipper, 0, len(h.ships))
	for _, s := range h.ships {
		ships = append(ships, s)
	}
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range ships {
		wg.Add(1)
		go func(s *argus.Shipper) { defer wg.Done(); s.Close(ctx) }(s)
	}
	wg.Wait()
}
