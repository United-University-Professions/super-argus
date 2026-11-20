package main

import (
	"encoding/json"
	"strings"
)

// sdkFilter drops pod-log lines the service already ships to Argus itself.
//
// Services using life-insurance's logging-library send their Warn-and-above log
// entries straight to Argus (the SDK hook), while this agent also tails the same
// stdout. Without a filter every such line is stored twice and error counts and
// alerts double. For the services named in ARGUS_SDK_SERVICES the agent therefore
// skips lines at or above the level the SDK ships (ARGUS_SDK_MIN_LEVEL, default
// warn) and keeps shipping everything else (info logs, non-JSON output such as
// panic traces), which the SDK does not cover.
type sdkFilter struct {
	services map[string]bool
	min      int
}

// newSDKFilter returns nil when no services are listed, which Hub treats as off.
func newSDKFilter(services map[string]bool, minLevel string) *sdkFilter {
	if len(services) == 0 {
		return nil
	}
	min := severity(minLevel)
	if min < 0 {
		min = severity("warn")
	}
	return &sdkFilter{services: services, min: min}
}

// skip reports whether a line from service duplicates what the SDK ships.
func (f *sdkFilter) skip(service, line string) bool {
	if f == nil || !f.services[service] {
		return false
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return false // only structured logrus lines are mirrored by the SDK
	}
	var e struct {
		Level string `json:"level"`
	}
	if json.Unmarshal([]byte(line), &e) != nil {
		return false
	}
	sev := severity(e.Level)
	return sev >= 0 && sev >= f.min
}

// severity orders log levels (logrus and slog spellings); -1 means unknown.
func severity(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "trace", "debug":
		return 0
	case "info":
		return 1
	case "warn", "warning":
		return 2
	case "error":
		return 3
	case "fatal":
		return 4
	case "panic":
		return 5
	}
	return -1
}
