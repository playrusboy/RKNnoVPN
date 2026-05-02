package root

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

const maxNodeProbeWorkers = 3

type URLProbeMetrics struct {
	LatencyMS     int64
	StatusCode    int
	ResponseBytes int64
	ThroughputBps int64
}

type ProbeIO interface {
	TCPConnect(host string, port int, timeout time.Duration) (int64, error)
	BootstrapDNS(cfg *config.Config, host string, timeout time.Duration) bool
	ClashDelay(apiPort int, apiSecret string, outboundTag string, testURL string, timeoutMS int) (int64, int, error)
	TransparentURLProbe(cfg *config.Config, testURL string, timeoutMS int, includeThroughput bool) (URLProbeMetrics, error)
}

type NodeProbeRunner struct {
	Context           context.Context
	Config            *config.Config
	TimeoutMS         int
	Timeout           time.Duration
	Requested         map[string]bool
	TestURL           string
	RuntimeRunning    bool
	RuntimeHealth     runtimev2.HealthSnapshot
	APIPort           int
	ProfileCount      int
	IncludeThroughput bool
	IO                ProbeIO
}

type NodeProbeInput struct {
	Context           context.Context
	Config            *config.Config
	State             core.State
	RuntimeHealth     runtimev2.HealthSnapshot
	URL               string
	TimeoutMS         int
	NodeIDs           []string
	APIPort           int
	IncludeThroughput bool
	IO                ProbeIO
}

func RunNodeProbes(input NodeProbeInput) []runtimev2.NodeProbeResult {
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	profiles := ProbeNodeProfiles(input.Config)
	timeout := time.Duration(input.TimeoutMS) * time.Millisecond
	runtimeRunning := input.State == core.StateRunning || input.State == core.StateDegraded
	runner := NodeProbeRunner{
		Context:           ctx,
		Config:            input.Config,
		TimeoutMS:         input.TimeoutMS,
		Timeout:           timeout,
		Requested:         RequestedNodeIDs(input.NodeIDs),
		TestURL:           ResolveNodeProbeURL(input.URL, input.Config),
		RuntimeRunning:    runtimeRunning,
		RuntimeHealth:     input.RuntimeHealth,
		APIPort:           input.APIPort,
		ProfileCount:      len(profiles),
		IncludeThroughput: input.IncludeThroughput,
		IO:                input.IO,
	}
	return runner.Run(profiles)
}

func (r NodeProbeRunner) Run(profiles []*config.NodeProfile) []runtimev2.NodeProbeResult {
	selected := make([]*config.NodeProfile, 0, len(profiles))
	for _, profile := range profiles {
		if r.Context.Err() != nil {
			break
		}
		if len(r.Requested) > 0 && !r.Requested[profile.ID] {
			continue
		}
		selected = append(selected, profile)
	}
	if len(selected) <= 1 {
		results := make([]runtimev2.NodeProbeResult, 0, len(selected))
		for _, profile := range selected {
			results = append(results, r.probeProfile(profile))
		}
		return results
	}

	results := make([]runtimev2.NodeProbeResult, len(selected))
	workers := minInt(maxNodeProbeWorkers, len(selected))
	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index] = r.probeProfile(selected[index])
			}
		}()
	}
	enqueued := 0
	for index := range selected {
		if r.Context.Err() != nil {
			break
		}
		jobs <- index
		enqueued++
	}
	close(jobs)
	wg.Wait()
	return results[:enqueued]
}

func (r NodeProbeRunner) probeProfile(profile *config.NodeProfile) runtimev2.NodeProbeResult {
	result := NewNodeProbeResult(profile)
	if r.Context.Err() != nil {
		result.URLStatus = "not_run"
		result.ThroughputStatus = "unavailable"
		result.Verdict = "unknown"
		result.ErrorClass = "cancelled"
		result.ErrorDetail = r.Context.Err().Error()
		return result
	}
	r.runTCPDirectProbe(profile, &result)
	if r.Context.Err() != nil {
		return FinalizeNodeProbeResult(result)
	}
	if result.TCPStatus == "fail" {
		result.URLStatus = "not_run"
		result.ThroughputStatus = "unavailable"
		result.Verdict = "unusable"
		return FinalizeNodeProbeResult(result)
	}
	r.runDNSBootstrapProbe(profile, &result)
	if r.Context.Err() != nil {
		return FinalizeNodeProbeResult(result)
	}
	r.runTunnelProbe(profile, &result)
	return FinalizeNodeProbeResult(result)
}

func NewNodeProbeResult(profile *config.NodeProfile) runtimev2.NodeProbeResult {
	return runtimev2.NodeProbeResult{
		ID:               profile.ID,
		Name:             firstNonEmpty(profile.Name, profile.Tag, profile.Address),
		Protocol:         profile.Protocol,
		Server:           profile.Address,
		Port:             profile.Port,
		TCPStatus:        "not_run",
		URLStatus:        "not_run",
		ThroughputStatus: "not_run",
		Verdict:          "unknown",
	}
}

func (r NodeProbeRunner) runTCPDirectProbe(profile *config.NodeProfile, result *runtimev2.NodeProbeResult) {
	tcpMS, tcpErr := r.IO.TCPConnect(profile.Address, profile.Port, r.Timeout)
	if tcpErr == nil {
		result.TCPDirect = &tcpMS
		result.TCPStatus = "ok"
		return
	}
	result.TCPStatus = "fail"
	result.ErrorClass = "tcp_direct_failed"
}

func (r NodeProbeRunner) runDNSBootstrapProbe(profile *config.NodeProfile, result *runtimev2.NodeProbeResult) {
	result.DNSBootstrap = r.IO.BootstrapDNS(r.Config, profile.Address, r.Timeout)
	if !result.DNSBootstrap && result.ErrorClass == "" {
		result.ErrorClass = "dns_bootstrap_failed"
	}
}

func (r NodeProbeRunner) runTunnelProbe(profile *config.NodeProfile, result *runtimev2.NodeProbeResult) {
	if result.TCPStatus == "fail" {
		result.URLStatus = "not_run"
		result.ThroughputStatus = "unavailable"
		result.Verdict = "unusable"
		return
	}
	if !r.RuntimeRunning {
		result.URLStatus = "fail"
		result.Verdict = "unusable"
		if result.ErrorClass == "" {
			result.ErrorClass = "tunnel_unavailable"
		}
		return
	}

	urlMS, urlErr := r.runTunnelURLProbe(profile, result)
	if urlErr == nil {
		result.TunnelDelay = &urlMS
		result.URLStatus = "ok"
		result.Verdict = "usable"
		result.ErrorClass = "ok"
		return
	}

	result.ErrorDetail = urlErr.Error()
	if classified := ClassifyURLTestFailure(urlErr, r.RuntimeHealth); classified != "" {
		result.ErrorClass = classified
	} else if result.ErrorClass == "" {
		result.ErrorClass = "tunnel_delay_failed"
	}
	if result.ErrorClass == "api_disabled" {
		result.URLStatus = "not_run"
		if result.ThroughputStatus == "not_run" {
			result.ThroughputStatus = "unavailable"
		}
		result.Verdict = "unknown"
		return
	}

	result.URLStatus = "fail"
	if result.ThroughputStatus == "not_run" {
		result.ThroughputStatus = "unavailable"
	}
	if result.TCPStatus == "ok" && isSoftURLProbeFailure(result.ErrorClass) {
		result.Verdict = "unknown"
		return
	}
	result.Verdict = "unusable"
}

func isSoftURLProbeFailure(errorClass string) bool {
	switch errorClass {
	case "outbound_url_failed", "runtime_degraded":
		return true
	default:
		return false
	}
}

func (r NodeProbeRunner) runTunnelURLProbe(profile *config.NodeProfile, result *runtimev2.NodeProbeResult) (int64, error) {
	if r.APIPort > 0 {
		apiSecret := ""
		if r.Config != nil {
			apiSecret = r.Config.Proxy.APISecret
		}
		urlMS, _, err := r.IO.ClashDelay(r.APIPort, apiSecret, profile.Tag, r.TestURL, r.TimeoutMS)
		result.ThroughputStatus = "latency_only"
		return urlMS, err
	}
	if r.ProfileCount == 1 || profile.ID == strings.TrimSpace(r.Config.Profile.ActiveNodeID) {
		metrics, err := r.IO.TransparentURLProbe(r.Config, r.TestURL, r.TimeoutMS, r.IncludeThroughput)
		if metrics.ResponseBytes > 0 {
			responseBytes := metrics.ResponseBytes
			result.ResponseBytes = &responseBytes
		}
		if metrics.ThroughputBps > 0 {
			throughputBps := metrics.ThroughputBps
			result.ThroughputBps = &throughputBps
			result.ThroughputStatus = "ok"
		} else {
			result.ThroughputStatus = "latency_only"
		}
		return metrics.LatencyMS, err
	}
	result.ThroughputStatus = "unavailable"
	return 0, fmt.Errorf("api_disabled")
}

func FinalizeNodeProbeResult(result runtimev2.NodeProbeResult) runtimev2.NodeProbeResult {
	if result.TCPStatus == "ok" && result.URLStatus == "fail" && !isSoftURLProbeFailure(result.ErrorClass) {
		result.Verdict = "unusable"
	}
	return result
}

func RequestedNodeIDs(nodeIDs []string) map[string]bool {
	requested := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		if id = strings.TrimSpace(id); id != "" {
			requested[id] = true
		}
	}
	return requested
}

func ProbeNodeProfiles(cfg *config.Config) []*config.NodeProfile {
	if cfg == nil {
		return nil
	}
	profiles := config.ProfilesFromConfigNodes(cfg)
	if len(profiles) > 0 {
		return profiles
	}
	profile := cfg.ResolveProfile()
	if profile.Address == "" {
		return nil
	}
	profile.Tag = firstNonEmpty(profile.Tag, "proxy")
	return []*config.NodeProfile{profile}
}

func ResolveNodeProbeURL(url string, cfg *config.Config) string {
	testURL := strings.TrimSpace(url)
	if testURL == "" && cfg != nil {
		testURL = strings.TrimSpace(cfg.Health.URL)
	}
	if testURL == "" {
		testURL = "https://www.gstatic.com/generate_204"
	}
	return testURL
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
