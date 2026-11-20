package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// When kubelet rotates 0.log (rename + fresh file), the agent must finish the
// old file and then follow the new one from its start, not rewind the old
// handle because the new file is smaller than the saved offset.
func TestKubernetesFollowsRotatedLog(t *testing.T) {
	rcv := &receiver{}
	srv := httptest.NewServer(http.HandlerFunc(rcv.handler))
	defer srv.Close()

	root := t.TempDir()
	st := LoadState(filepath.Join(t.TempDir(), "state.json"))
	hub := NewHub(srv.URL, "key", nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runKubernetes(ctx, hub, st, root, nil); close(done) }()
	defer func() { cancel(); <-done }()
	time.Sleep(200 * time.Millisecond)

	dir := filepath.Join(root, "shop_worker-0_uid3", "main")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "0.log")
	line := func(msg string) string {
		return time.Now().UTC().Format(time.RFC3339Nano) + " stdout F " + msg + "\n"
	}
	// the old file is long, so the new (smaller) one is below the saved offset
	old := line("old one") + line("old two") + line(strings.Repeat("x", 400))
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "old lines", func() bool { return strings.Contains(rcv.joined(), "old two") })

	if err := os.Rename(path, path+".20261120-100000"); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("cannot rename an open file on Windows:", err)
		}
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(line("after rotation")), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "line from the new file", func() bool { return strings.Contains(rcv.joined(), "after rotation") })

	time.Sleep(1500 * time.Millisecond) // a re-read loop would resend the old lines by now
	if n := strings.Count(rcv.joined(), "old one"); n != 1 {
		t.Fatalf("old lines were re-read after rotation: seen %d times", n)
	}
}
