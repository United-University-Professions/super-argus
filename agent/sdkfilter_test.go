package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSDKFilterSkipsOnlyWhatTheSDKShips(t *testing.T) {
	f := newSDKFilter(map[string]bool{"prod-ticketing": true}, "")
	cases := []struct {
		service, line string
		skip          bool
	}{
		{"prod-ticketing", `{"level":"error","msg":"request failed","status":500}`, true},
		{"prod-ticketing", `{"level":"warning","msg":"request rejected"}`, true}, // logrus spells warn "warning"
		{"prod-ticketing", `{"level":"fatal","msg":"boom"}`, true},
		{"prod-ticketing", `{"level":"info","msg":"request"}`, false}, // the SDK does not ship info
		{"prod-ticketing", `panic: runtime error`, false},             // not structured: only the agent sees it
		{"prod-ticketing", `{"level":"error"`, false},                 // truncated JSON is kept
		{"prod-ticketing", `{"msg":"no level"}`, false},
		{"prod-membership", `{"level":"error","msg":"x"}`, false}, // service not hooked up: keep
	}
	for _, c := range cases {
		if got := f.skip(c.service, c.line); got != c.skip {
			t.Errorf("skip(%q, %q) = %v, want %v", c.service, c.line, got, c.skip)
		}
	}
	var off *sdkFilter
	if off.skip("prod-ticketing", `{"level":"error"}`) {
		t.Error("a nil filter must skip nothing")
	}
	if newSDKFilter(nil, "") != nil {
		t.Error("no services listed means the filter is off")
	}
}

func TestSDKFilterHonoursMinLevel(t *testing.T) {
	f := newSDKFilter(map[string]bool{"svc": true}, "error")
	if f.skip("svc", `{"level":"warning"}`) {
		t.Error("warn is below the SDK's configured minimum, so the agent must keep it")
	}
	if !f.skip("svc", `{"level":"error"}`) {
		t.Error("error is shipped by the SDK, so the agent skips it")
	}
}

func TestHubDropsSDKDuplicatesButKeepsTheRest(t *testing.T) {
	rcv := &receiver{}
	srv := httptest.NewServer(http.HandlerFunc(rcv.handler))
	defer srv.Close()
	h := NewHub(srv.URL, "k", nil)
	h.sdk = newSDKFilter(map[string]bool{"prod-ticketing": true}, "")
	labels := map[string]string{"service": "prod-ticketing"}
	now := time.Now()
	h.Send(labels, now, `{"level":"error","msg":"duplicate-of-sdk"}`)
	h.Send(labels, now, `{"level":"info","msg":"only-the-agent-has-this"}`)
	h.Send(map[string]string{"service": "other"}, now, `{"level":"error","msg":"unhooked-service"}`)

	waitFor(t, "kept lines", func() bool {
		s := rcv.joined()
		return strings.Contains(s, "only-the-agent-has-this") && strings.Contains(s, "unhooked-service")
	})
	if strings.Contains(rcv.joined(), "duplicate-of-sdk") {
		t.Fatalf("an SDK-shipped line leaked through the agent:\n%s", rcv.joined())
	}
}
