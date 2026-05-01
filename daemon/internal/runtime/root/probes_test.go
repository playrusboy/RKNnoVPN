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
	urlErr           error
	dnsBootstrap     *bool
	transparentCalls *int
	clashCalls       *int
}

func (f fakeProbeIO) TCPConnect(host string, port int, timeout time.Duration) (int64, error) {
	return 12, nil
}

func (f fakeProbeIO) BootstrapDNS(cfg *config.Config, host string, timeout time.Duration) bool {
	if f.dnsBootstrap != nil {
		return *f.dnsBootstrap
	}
	return true
}

func (f fakeProbeIO) ClashDelay(apiPort int, outboundTag string, testURL string, timeoutMS int) (int64, int, error) {
	if f.clashCalls != nil {
		*f.clashCalls++
	}
	return 0, 0, f.urlErr
}

func (f fakeProbeIO) TransparentURLProbe(cfg *config.Config, testURL string, timeoutMS int) (URLProbeMetrics, error) {
	if f.transparentCalls != nil {
		*f.transparentCalls++
	}
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

func TestRunNodeProbesOverridesBootstrapFailureWithURLFailure(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Profile.ActiveNodeID = "node-a"
	cfg.Profile.Nodes = []json.RawMessage{mustRawNode(t, "node-a")}
	dnsOK := false

	results := RunNodeProbes(NodeProbeInput{
		Config:    cfg,
		State:     core.StateRunning,
		TimeoutMS: 1000,
		RuntimeHealth: runtimev2.HealthSnapshot{
			CoreReady:    true,
			RoutingReady: true,
			DNSReady:     true,
			LastCode:     "OUTBOUND_URL_FAILED",
		},
		IO: fakeProbeIO{
			dnsBootstrap: &dnsOK,
			urlErr:       errors.New("tls: failed to verify certificate"),
		},
	})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if results[0].ErrorClass != "outbound_url_failed" {
		t.Fatalf("ErrorClass = %q, want outbound_url_failed", results[0].ErrorClass)
	}
}

func TestRunNodeProbesUsesTransparentURLProbeForActiveNodeWithoutClashAPI(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Profile.ActiveNodeID = "node-b"
	cfg.Profile.Nodes = []json.RawMessage{
		mustRawNode(t, "node-a"),
		mustRawNode(t, "node-b"),
	}
	transparentCalls := 0
	clashCalls := 0

	results := RunNodeProbes(NodeProbeInput{
		Config:    cfg,
		State:     core.StateRunning,
		TimeoutMS: 1000,
		NodeIDs:   []string{"node-b"},
		IO: fakeProbeIO{
			transparentCalls: &transparentCalls,
			clashCalls:       &clashCalls,
		},
	})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if results[0].URLStatus != "ok" || results[0].Verdict != "usable" {
		t.Fatalf("active node should use transparent route probe, got url=%q verdict=%q error=%q", results[0].URLStatus, results[0].Verdict, results[0].ErrorClass)
	}
	if transparentCalls != 1 {
		t.Fatalf("transparent probe calls = %d, want 1", transparentCalls)
	}
	if clashCalls != 0 {
		t.Fatalf("clash probe calls = %d, want 0", clashCalls)
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
