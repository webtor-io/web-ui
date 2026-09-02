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

func TestFormatSwarmLine_SilentSwarmCountsDown(t *testing.T) {
	got := formatSwarmLine(tpStub, 0, 0, 3, 0, 0, 42*time.Second)
	if !strings.HasPrefix(got, "job.swarmWaiting{") || !strings.Contains(got, "Left=42") {
		t.Errorf("silent swarm must show the countdown: %s", got)
	}
	if !strings.Contains(got, "job.peers{Peers=3}") {
		t.Errorf("without a seeder/leecher split the combined peer count is used: %s", got)
	}
}

func TestFormatSwarmLine_PrefersSeederLeecherSplit(t *testing.T) {
	got := formatSwarmLine(tpStub, 2, 5, 7, 0, 0, 10*time.Second)
	if !strings.Contains(got, "job.swarm{") || !strings.Contains(got, "Seeders=2") || !strings.Contains(got, "Leechers=5") {
		t.Errorf("seeders/leechers must win over the peer count: %s", got)
	}
}

func TestFormatSwarmLine_DownloadingShowsSpeedAndBytes(t *testing.T) {
	got := formatSwarmLine(tpStub, 1, 1, 2, 3*1024*1024, 512*1024, 0)
	// helpers.Bytes renders decimal units: "512 kB", "3.0 MB".
	if !strings.HasPrefix(got, "job.swarmDownloading{") || !strings.Contains(got, "Speed=512 kB") {
		t.Errorf("downloading line must carry speed: %s", got)
	}
	if !strings.Contains(got, "Bytes=3.0 MB") {
		t.Errorf("downloading line must carry bytes: %s", got)
	}
	if strings.Contains(got, "Left=") {
		t.Errorf("no countdown once data flows: %s", got)
	}
}

func TestFormatSwarmLine_NoCountdownPastDeadline(t *testing.T) {
	got := formatSwarmLine(tpStub, 0, 0, 0, 0, 0, -3*time.Second)
	if strings.Contains(got, "Left=") {
		t.Errorf("a negative countdown must not render: %s", got)
	}
}
