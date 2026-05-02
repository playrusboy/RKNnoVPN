package root

import (
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

type fakeProbeIO struct {
	urlErr           error
	tcpErr           error
	dnsBootstrap     *bool
	transparentCalls *int
	throughputModes  *[]bool
	clashCalls       *int
}

type countingProbeIO struct {
	active int32
	max    int32
}

func (f *countingProbeIO) TCPConnect(host string, port int, timeout time.Duration) (int64, error) {
	active := atomic.AddInt32(&f.active, 1)
	for {
		maxActive := atomic.LoadInt32(&f.max)
		if active <= maxActive || atomic.CompareAndSwapInt32(&f.max, maxActive, active) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	atomic.AddInt32(&f.active, -1)
	return 12, nil
}

func (f *countingProbeIO) BootstrapDNS(cfg *config.Config, host string, timeout time.Duration) bool {
	return true
}

func (f *countingProbeIO) ClashDelay(apiPort int, apiSecret string, outboundTag string, testURL string, timeoutMS int) (int64, int, error) {
	return 0, 0, nil
}

func (f *countingProbeIO) TransparentURLProbe(cfg *config.Config, testURL string, timeoutMS int, includeThroughput bool) (URLProbeMetrics, error) {
	return URLProbeMetrics{}, nil
}

func (f fakeProbeIO) TCPConnect(host string, port int, timeout time.Duration) (int64, error) {
	if f.tcpErr != nil {
		return 0, f.tcpErr
	}
	return 12, nil
}

func (f fakeProbeIO) BootstrapDNS(cfg *config.Config, host string, timeout time.Duration) bool {
	if f.dnsBootstrap != nil {
		return *f.dnsBootstrap
	}
	return true
}

func (f fakeProbeIO) ClashDelay(apiPort int, apiSecret string, outboundTag string, testURL string, timeoutMS int) (int64, int, error) {
	if f.clashCalls != nil {
		*f.clashCalls++
	}
	return 0, 0, f.urlErr
}

func (f fakeProbeIO) TransparentURLProbe(cfg *config.Config, testURL string, timeoutMS int, includeThroughput bool) (URLProbeMetrics, error) {
	if f.transparentCalls != nil {
		*f.transparentCalls++
	}
	if f.throughputModes != nil {
		*f.throughputModes = append(*f.throughputModes, includeThroughput)
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
	if results[0].Verdict != "unknown" {
		t.Fatalf("soft outbound URL failure verdict = %q, want unknown", results[0].Verdict)
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

func TestRunNodeProbesControlsTransparentThroughputMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Profile.Nodes = []json.RawMessage{mustRawNode(t, "node-a")}
	modes := []bool{}

	RunNodeProbes(NodeProbeInput{
		Config:            cfg,
		State:             core.StateRunning,
		TimeoutMS:         1000,
		IncludeThroughput: false,
		IO: fakeProbeIO{
			throughputModes: &modes,
		},
	})
	RunNodeProbes(NodeProbeInput{
		Config:            cfg,
		State:             core.StateRunning,
		TimeoutMS:         1000,
		IncludeThroughput: true,
		IO: fakeProbeIO{
			throughputModes: &modes,
		},
	})

	if len(modes) != 2 || modes[0] || !modes[1] {
		t.Fatalf("transparent throughput modes = %#v, want [false true]", modes)
	}
}

func TestRunNodeProbesSkipsTunnelProbeAfterTCPFailure(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Proxy.APIPort = 9090
	cfg.Profile.Nodes = []json.RawMessage{mustRawNode(t, "node-a")}
	clashCalls := 0

	results := RunNodeProbes(NodeProbeInput{
		Config:    cfg,
		State:     core.StateRunning,
		TimeoutMS: 1000,
		IO: fakeProbeIO{
			tcpErr:     errors.New("connect timeout"),
			clashCalls: &clashCalls,
		},
	})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if results[0].URLStatus != "not_run" || results[0].Verdict != "unusable" || results[0].ErrorClass != "tcp_direct_failed" {
		t.Fatalf("tcp failure should skip tunnel probe, got %#v", results[0])
	}
	if clashCalls != 0 {
		t.Fatalf("clash delay calls = %d, want 0", clashCalls)
	}
}

func TestRunNodeProbesUsesBoundedConcurrency(t *testing.T) {
	cfg := config.DefaultConfig()
	for i := 0; i < maxNodeProbeWorkers+3; i++ {
		cfg.Profile.Nodes = append(cfg.Profile.Nodes, mustRawNode(t, "node-"+string(rune('a'+i))))
	}
	io := &countingProbeIO{}

	results := RunNodeProbes(NodeProbeInput{
		Config:    cfg,
		State:     core.StateStopped,
		TimeoutMS: 1000,
		IO:        io,
	})
	if len(results) != maxNodeProbeWorkers+3 {
		t.Fatalf("results length = %d, want %d", len(results), maxNodeProbeWorkers+3)
	}
	maxActive := atomic.LoadInt32(&io.max)
	if maxActive <= 1 {
		t.Fatalf("expected probes to run concurrently, max active = %d", maxActive)
	}
	if maxActive > maxNodeProbeWorkers {
		t.Fatalf("max active probes = %d, want <= %d", maxActive, maxNodeProbeWorkers)
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
