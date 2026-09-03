package scripts

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// tpStub renders "key{k=v,...}" so assertions can check which key and which
// numbers the line was built from without a locale bundle.
func tpStub(key string, data map[string]any) string {
	parts := make([]string, 0, len(data))
	for k, v := range data {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return key + "{" + strings.Join(parts, ",") + "}"
}

// The warm-up line is minimal on purpose: the badge and the piece bar carry
// the swarm and the speed, so the line only says how long until a verdict
// (silent swarm) or how much of the warm-up range has arrived.
func TestFormatWarmupLine(t *testing.T) {
	const mb = 1024 * 1024
	cases := []struct {
		name          string
		bytes, target int64
		left          time.Duration
		want          string
	}{
		{"silent swarm counts down", 0, mb, 42 * time.Second, "job.warmupCountdown{Seconds=42}"},
		{"countdown over, nothing arrived → keep previous line", 0, mb, -3 * time.Second, ""},
		{"bytes flowing → percent of the warm-up range", 384 * 1024, mb, 0, "38%"},
		{"bytes flowing ignores a stale countdown", 384 * 1024, mb, 30 * time.Second, "38%"},
		{"never above 100%", 3 * mb, mb, 0, "100%"},
		{"no target → nothing honest to say", 512 * 1024, 0, 0, ""},
	}
	for _, c := range cases {
		if got := formatWarmupLine(tpStub, c.bytes, c.target, c.left); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
