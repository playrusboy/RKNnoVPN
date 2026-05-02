package control

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/diagnostics"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/health"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/netstack"
	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/updater"
)

type DiagnosticsState struct {
	Config      *config.Config
	ConfigPath  string
	ProfilePath string
	DataDir     string
}

type DiagnosticsHandlers struct {
	Version               string
	ConfigPath            string
	ProfilePath           string
	DataDir               string
	CurrentConfig         CurrentConfigFunc
	RunHealth             func() *health.HealthResult
	HealthSnapshot        func(*health.HealthResult, bool) runtimev2.HealthSnapshot
	RefreshRuntimeHealth  func() runtimev2.HealthSnapshot
	RuntimeStatus         RuntimeStatusFunc
	NetstackReport        func(*config.Config) netstack.Report
	NetstackRuntimeReport func(*config.Config) netstack.Report
	TestNodes             func(url string, timeoutMS int, nodeIDs []string) []runtimev2.NodeProbeResult
	TestNodesContext      func(ctx context.Context, url string, timeoutMS int, nodeIDs []string) []runtimev2.NodeProbeResult
	CoreStartReport       func() core.RuntimeStageReport
	CoreRuntimeReport     func() core.RuntimeStageReport
	ReloadReport          func() core.RuntimeStageReport
	Exec                  diagnostics.ExecCommandFunc
	ExecContext           func(ctx context.Context, name string, args ...string) (string, error)
	Now                   func() time.Time
}

func (h DiagnosticsHandlers) DiagnosticsReport(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	return h.DiagnosticsReportContext(context.Background(), params)
}

func (h DiagnosticsHandlers) DiagnosticsReportContext(ctx context.Context, params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeDiagnosticsReportParams(params)
	if err != nil {
		return nil, &ipc.RPCError{
			Code:    ipc.CodeInvalidParams,
			Message: err.Error(),
		}
	}
	report, err := h.buildReportContext(ctx, request.Lines)
	if err != nil {
		return nil, contextRPCError(err)
	}
	return report, nil
}

func (h DiagnosticsHandlers) SelfCheck(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	return h.SelfCheckContext(context.Background(), params)
}

func (h DiagnosticsHandlers) SelfCheckContext(ctx context.Context, params *json.RawMessage) (interface{}, *ipc.RPCError) {
	summary, err := h.BuildSelfCheckSummaryContext(ctx, DefaultDiagnosticsReportLines)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: err.Error()}
	}
	return summary, nil
}

func (h DiagnosticsHandlers) DiagnosticsHealth(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	return h.DiagnosticsHealthContext(context.Background(), params)
}

func (h DiagnosticsHandlers) DiagnosticsHealthContext(ctx context.Context, params *json.RawMessage) (interface{}, *ipc.RPCError) {
	ctx = diagnosticsContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, contextRPCError(err)
	}
	return h.refreshRuntimeHealth(), nil
}

func (h DiagnosticsHandlers) DiagnosticsTestNodes(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	return h.DiagnosticsTestNodesContext(context.Background(), params)
}

func (h DiagnosticsHandlers) DiagnosticsTestNodesContext(ctx context.Context, params *json.RawMessage) (interface{}, *ipc.RPCError) {
	ctx = diagnosticsContext(ctx)
	var p struct {
		NodeIDs   []string `json:"node_ids"`
		URL       string   `json:"url"`
		TimeoutMS int      `json:"timeout_ms"`
	}
	if params != nil {
		if err := json.Unmarshal(*params, &p); err != nil {
			return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: "invalid params: " + err.Error()}
		}
	}
	if p.TimeoutMS <= 0 {
		p.TimeoutMS = 5000
	}
	if h.TestNodes == nil && h.TestNodesContext == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "node probe callback is not configured"}
	}
	if err := ctx.Err(); err != nil {
		return nil, contextRPCError(err)
	}

	results := h.testNodes(ctx, p.URL, p.TimeoutMS, p.NodeIDs)
	if err := ctx.Err(); err != nil {
		return nil, contextRPCError(err)
	}
	return map[string]interface{}{
		"url":     p.URL,
		"results": results,
	}, nil
}

func (h DiagnosticsHandlers) buildReport(lines int) map[string]interface{} {
	report, _ := h.buildReportContext(context.Background(), lines)
	return report
}

func (h DiagnosticsHandlers) buildReportContext(ctx context.Context, lines int) (map[string]interface{}, error) {
	ctx = diagnosticsContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state := h.state()
	cfg := state.Config
	cfgPath := state.ConfigPath
	profilePath := state.ProfilePath
	dataDir := state.DataDir

	modulePaths := modulecontract.NewPaths(dataDir)
	renderedConfigPath := filepath.Join(modulePaths.RenderedConfigDir(), "singbox.json")
	singBoxPath := filepath.Join(modulePaths.BinDir(), "sing-box")

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	healthResult := h.runHealth()
	healthSnapshot := h.healthSnapshot(healthResult, true)
	var backendStatus interface{}
	runtimeStatus, hasRuntimeStatus := h.runtimeStatus()
	if hasRuntimeStatus {
		backendStatus = runtimeStatus
	}
	moduleVersion := diagnostics.ReadModuleVersion()
	ports := diagnostics.PortStatuses(cfg)
	portConflicts := diagnostics.LocalPortConflicts(cfg)
	netstackReport := h.netstackReport(cfg)
	netstackRuntimeReport := h.netstackRuntimeReport(cfg)
	leftovers := netstackReport.Leftovers
	var nodeResults []runtimev2.NodeProbeResult
	if cfg != nil && (h.TestNodes != nil || h.TestNodesContext != nil) {
		nodeResults = h.testNodes(ctx, cfg.Health.URL, 2500, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	exec := h.execForContext(ctx)
	privacy := diagnostics.Privacy(cfg, lines, exec)
	singBoxCheck := diagnostics.SingBoxCheck(singBoxPath, renderedConfigPath, lines, exec)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	releaseIntegrity := diagnostics.ReleaseIntegrityReport(dataDir)
	runtimePreflight := diagnostics.RuntimePreflightReport(dataDir)
	routingSummary := diagnostics.RoutingSummaryFromConfig(cfg)
	profileSummary := diagnostics.ProfileSummaryFromConfig(cfg, runtimeStatus)
	packageResolution := diagnostics.PackageResolutionFromConfig(cfg)
	summary := diagnostics.WithIPCContractFacts(
		diagnostics.BuildSummaryWithCanonical(h.Version, ProtocolVersion, runtimeStatus.Canonical, healthSnapshot, leftovers, netstackRuntimeReport, nodeResults, ports, privacy, moduleVersion, singBoxCheck, releaseIntegrity, profileSummary, routingSummary, packageResolution),
		ipc.ContractVersion(),
		ipc.APKRequiredMethods(),
	).WithRuntimePreflight(runtimePreflight)
	daemonPID, socketInode := DaemonIdentity(modulePaths.DaemonSocket())
	versions := addCompatibilityIdentityFields(addIPCContractFields(map[string]interface{}{
		"daemon":             h.Version,
		"core":               h.Version,
		"daemonctl_expected": h.Version,
		"panel_min_version":  h.Version,
		"sing_box":           diagnostics.SingBoxVersion(singBoxPath, lines, exec),
		"module":             moduleVersion,
		"runtime_preflight":  runtimePreflight,
	}), h.Version, moduleVersion["version"], daemonPID, socketInode)

	report := map[string]interface{}{
		"generated_at":      h.now().Format(time.RFC3339),
		"summary":           summary,
		"diagnostics_graph": summary.Graph,
		"versions":          versions,
		"device":            diagnostics.DeviceCommands(lines, exec),
		"paths": map[string]diagnostics.FileStatus{
			"data_dir":          diagnostics.StatFile(dataDir, false),
			"current_release":   diagnostics.StatFile(filepath.Join(dataDir, "current"), false),
			"releases_dir":      diagnostics.StatFile(filepath.Join(dataDir, "releases"), false),
			"config":            diagnostics.StatFile(cfgPath, false),
			"profile":           diagnostics.StatFile(profilePath, false),
			"rendered_singbox":  diagnostics.StatFile(renderedConfigPath, false),
			"sing_box_binary":   diagnostics.StatFile(singBoxPath, true),
			"daemon_log":        diagnostics.StatFile(filepath.Join(modulePaths.LogDir(), "daemon.log"), false),
			"sing_box_log":      diagnostics.StatFile(filepath.Join(modulePaths.LogDir(), "sing-box.log"), false),
			"daemon_socket":     diagnostics.StatFile(modulePaths.DaemonSocket(), false),
			"sing_box_pid_file": diagnostics.StatFile(modulePaths.SingBoxPIDFile(), false),
			"runtime_state":     diagnostics.StatFile(runtimev2.RuntimeStatePath(dataDir), false),
			"install_state":     diagnostics.StatFile(updater.InstallStatePath(dataDir), false),
		},
		"health": map[string]interface{}{
			"snapshot": healthSnapshot,
			"raw":      healthResult,
		},
		"canonical_status":    runtimeStatus.Canonical,
		"backend_status":      backendStatus,
		"core_start_report":   h.coreStartReport(),
		"core_runtime_report": h.coreRuntimeReport(),
		"reload_report":       h.reloadReport(),
		"ports":               ports,
		"port_conflicts":      portConflicts,
		"routing":             routingSummary,
		"profile":             profileSummary,
		"package_resolution":  packageResolution,
		"netstack":            netstackReport,
		"netstack_runtime":    netstackRuntimeReport,
		"runtime_preflight":   runtimePreflight,
		"leftovers":           leftovers,
		"node_tests":          diagnostics.RedactNodeProbeResults(nodeResults),
		"logs":                diagnostics.ReadLogSections(diagnostics.DefaultLogFileSpecs(dataDir), lines, 512*1024, diagnostics.RedactText),
		"config": map[string]diagnostics.JSONSection{
			"daemon":           diagnostics.ReadRedactedJSONFile(cfgPath),
			"profile":          diagnostics.ReadRedactedJSONFile(profilePath),
			"rendered_singbox": diagnostics.ReadRedactedJSONFile(renderedConfigPath),
		},
		"runtime":           diagnostics.RuntimeCommands(lines, exec),
		"privacy":           privacy,
		"release_integrity": releaseIntegrity,
	}
	report["sing_box_check"] = singBoxCheck
	return report, nil
}

func (h DiagnosticsHandlers) BuildSelfCheckSummary(lines int) (diagnostics.Summary, error) {
	return h.BuildSelfCheckSummaryContext(context.Background(), lines)
}

func (h DiagnosticsHandlers) BuildSelfCheckSummaryContext(ctx context.Context, lines int) (diagnostics.Summary, error) {
	ctx = diagnosticsContext(ctx)
	if err := ctx.Err(); err != nil {
		return diagnostics.Summary{}, err
	}
	state := h.state()
	cfg := state.Config
	dataDir := state.DataDir
	if cfg == nil {
		return diagnostics.Summary{Status: "failed", Issues: []string{"config unavailable"}, IssueCount: 1}, nil
	}
	if lines <= 0 {
		lines = DefaultDiagnosticsReportLines
	}
	modulePaths := modulecontract.NewPaths(dataDir)
	renderedConfigPath := filepath.Join(modulePaths.RenderedConfigDir(), "singbox.json")
	singBoxPath := filepath.Join(modulePaths.BinDir(), "sing-box")
	healthResult := h.runHealth()
	healthSnapshot := h.healthSnapshot(healthResult, true)
	if err := ctx.Err(); err != nil {
		return diagnostics.Summary{}, err
	}
	netstackReport := h.netstackReport(cfg)
	netstackRuntimeReport := h.netstackRuntimeReport(cfg)
	var nodeResults []runtimev2.NodeProbeResult
	if cfg != nil && (h.TestNodes != nil || h.TestNodesContext != nil) {
		nodeResults = h.testNodes(ctx, cfg.Health.URL, 2500, nil)
	}
	if err := ctx.Err(); err != nil {
		return diagnostics.Summary{}, err
	}
	runtimeStatus, _ := h.runtimeStatus()
	exec := h.execForContext(ctx)
	runtimePreflight := diagnostics.RuntimePreflightReport(dataDir)
	return diagnostics.WithIPCContractFacts(
		diagnostics.BuildSummaryWithCanonical(
			h.Version,
			ProtocolVersion,
			runtimeStatus.Canonical,
			healthSnapshot,
			netstackReport.Leftovers,
			netstackRuntimeReport,
			nodeResults,
			diagnostics.PortStatuses(cfg),
			diagnostics.Privacy(cfg, lines, exec),
			diagnostics.ReadModuleVersion(),
			diagnostics.SingBoxCheck(singBoxPath, renderedConfigPath, lines, exec),
			diagnostics.ReleaseIntegrityReport(dataDir),
			diagnostics.ProfileSummaryFromConfig(cfg, runtimeStatus),
			diagnostics.RoutingSummaryFromConfig(cfg),
			diagnostics.PackageResolutionFromConfig(cfg),
		),
		ipc.ContractVersion(),
		ipc.APKRequiredMethods(),
	).WithRuntimePreflight(runtimePreflight), nil
}

func (h DiagnosticsHandlers) state() DiagnosticsState {
	profilePath := h.ProfilePath
	if profilePath == "" && h.ConfigPath != "" {
		profilePath = profiledoc.Path(h.ConfigPath)
	}
	var cfg *config.Config
	if h.CurrentConfig != nil {
		cfg = h.CurrentConfig()
	}
	return DiagnosticsState{
		Config:      cfg,
		ConfigPath:  h.ConfigPath,
		ProfilePath: profilePath,
		DataDir:     h.DataDir,
	}
}

func (h DiagnosticsHandlers) runHealth() *health.HealthResult {
	if h.RunHealth != nil {
		return h.RunHealth()
	}
	return nil
}

func (h DiagnosticsHandlers) healthSnapshot(result *health.HealthResult, allowEgressProbe bool) runtimev2.HealthSnapshot {
	if h.HealthSnapshot != nil {
		return h.HealthSnapshot(result, allowEgressProbe)
	}
	return runtimev2.HealthSnapshot{}
}

func (h DiagnosticsHandlers) refreshRuntimeHealth() runtimev2.HealthSnapshot {
	if h.RefreshRuntimeHealth != nil {
		return h.RefreshRuntimeHealth()
	}
	return runtimev2.HealthSnapshot{}
}

func (h DiagnosticsHandlers) runtimeStatus() (runtimev2.Status, bool) {
	if h.RuntimeStatus != nil {
		return h.RuntimeStatus()
	}
	return runtimev2.Status{}, false
}

func (h DiagnosticsHandlers) netstackReport(cfg *config.Config) netstack.Report {
	if h.NetstackReport != nil {
		return h.NetstackReport(cfg)
	}
	return netstack.Report{}
}

func (h DiagnosticsHandlers) netstackRuntimeReport(cfg *config.Config) netstack.Report {
	if h.NetstackRuntimeReport != nil {
		return h.NetstackRuntimeReport(cfg)
	}
	return netstack.Report{}
}

func (h DiagnosticsHandlers) coreStartReport() core.RuntimeStageReport {
	if h.CoreStartReport != nil {
		return h.CoreStartReport()
	}
	return core.RuntimeStageReport{}
}

func (h DiagnosticsHandlers) coreRuntimeReport() core.RuntimeStageReport {
	if h.CoreRuntimeReport != nil {
		return h.CoreRuntimeReport()
	}
	return core.RuntimeStageReport{}
}

func (h DiagnosticsHandlers) reloadReport() core.RuntimeStageReport {
	if h.ReloadReport != nil {
		return h.ReloadReport()
	}
	return core.RuntimeStageReport{}
}

func (h DiagnosticsHandlers) exec() diagnostics.ExecCommandFunc {
	if h.Exec != nil {
		return h.Exec
	}
	return func(name string, args ...string) (string, error) {
		return "", nil
	}
}

func (h DiagnosticsHandlers) execForContext(ctx context.Context) diagnostics.ExecCommandFunc {
	ctx = diagnosticsContext(ctx)
	if h.ExecContext != nil {
		return func(name string, args ...string) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return h.ExecContext(ctx, name, args...)
		}
	}
	exec := h.exec()
	return func(name string, args ...string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return exec(name, args...)
	}
}

func (h DiagnosticsHandlers) testNodes(ctx context.Context, url string, timeoutMS int, nodeIDs []string) []runtimev2.NodeProbeResult {
	if h.TestNodesContext != nil {
		return h.TestNodesContext(ctx, url, timeoutMS, nodeIDs)
	}
	if h.TestNodes != nil {
		return h.TestNodes(url, timeoutMS, nodeIDs)
	}
	return nil
}

func (h DiagnosticsHandlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func diagnosticsContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func contextRPCError(err error) *ipc.RPCError {
	return &ipc.RPCError{Code: ipc.CodeInternalError, Message: "request cancelled: " + err.Error()}
}
