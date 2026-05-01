package root

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

type fakeProbeIO struct {
	urlErr error
}

func (f fakeProbeIO) TCPConnect(host string, port int, timeout time.Duration) (int64, error) {
	return 12, nil
}

func (f fakeProbeIO) BootstrapDNS(cfg *config.Config, host string, timeout time.Duration) bool {
	return true
}

func (f fakeProbeIO) ClashDelay(apiPort int, outboundTag string, testURL string, timeoutMS int) (int64, int, error) {
	return 0, 0, f.urlErr
}

func (f fakeProbeIO) TransparentURLProbe(cfg *config.Config, testURL string, timeoutMS int) (URLProbeMetrics, error) {
	return URLProbeMetrics{}, f.urlErr
}

func TestRunNodeProbesKeepsAPIDisabledAsUnknown(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Profile.Nodes = []json.RawMessage{
		mustRawNode(t, "node-a"),
		mustRawNode(t, "node-b"),
	}

	results := RunNodeProbes(NodeProbeInput{
		Config:    cfg,
		State:     core.StateRunning,
		TimeoutMS: 1000,
		IO:        fakeProbeIO{urlErr: errors.New("api_disabled")},
	})
	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}
	for _, result := range results {
		if result.ErrorClass != "api_disabled" {
			t.Fatalf("ErrorClass = %q, want api_disabled", result.ErrorClass)
		}
		if result.URLStatus != "not_run" || result.Verdict != "unknown" {
			t.Fatalf("api-disabled probe should be unknown/not_run, got url=%q verdict=%q", result.URLStatus, result.Verdict)
		}
	}
}

func TestFinalizeNodeProbeResultDoesNotMarkSkippedURLProbeUnusable(t *testing.T) {
	result := FinalizeNodeProbeResult(nodeProbeResult("ok", "not_run", "unknown"))
	if result.Verdict != "unknown" {
		t.Fatalf("skipped URL probe verdict = %q, want unknown", result.Verdict)
	}

	result = FinalizeNodeProbeResult(nodeProbeResult("ok", "fail", "unknown"))
	if result.Verdict != "unusable" {
		t.Fatalf("failed URL probe verdict = %q, want unusable", result.Verdict)
	}
}

func mustRawNode(t *testing.T, id string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]interface{}{
		"id":       id,
		"name":     id,
		"protocol": "vless",
		"server":   "example.com",
		"port":     443,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func nodeProbeResult(tcpStatus string, urlStatus string, verdict string) runtimev2.NodeProbeResult {
	return runtimev2.NodeProbeResult{
		TCPStatus: tcpStatus,
		URLStatus: urlStatus,
		Verdict:   verdict,
	}
}
