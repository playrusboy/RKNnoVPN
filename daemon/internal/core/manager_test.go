package core

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/netstack"
)

func TestIgnorableCleanupScriptError(t *testing.T) {
	if !ignorableCleanupScriptError(errors.New("script not found: /data/adb/modules/rknnovpn/scripts/dns.sh: no such file or directory")) {
		t.Fatal("missing cleanup script should be treated as an idempotent cleanup no-op")
	}
	if ignorableCleanupScriptError(errors.New("exec iptables.sh stop: exit status 2")) {
		t.Fatal("real cleanup command failures must still be reported")
	}
}

func TestSelfTestAppsAreBuiltInAlwaysDirect(t *testing.T) {
	for _, packageName := range SelfTestProtectedPackages {
		if !IsBuiltInAlwaysDirectPackage(packageName) {
			t.Fatalf("%s self-test app must stay direct by default", packageName)
		}
	}
}

func TestRuntimeErrorCarriesTypedStageDetails(t *testing.T) {
	report := NewRuntimeStageReport("start")
	report.AddStage("netstack-apply", "failed", "RULES_NOT_APPLIED", "iptables denied", true)
	err := runtimeErrorWithReport("iptables start", "RULES_NOT_APPLIED", errors.New("iptables denied"), true, report)
	runtimeErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if runtimeErr.RuntimeCode() != "RULES_NOT_APPLIED" {
		t.Fatalf("unexpected code: %#v", runtimeErr)
	}
	if !runtimeErr.RuntimeRollbackApplied() {
		t.Fatalf("rollback flag should be preserved: %#v", runtimeErr)
	}
	if !strings.Contains(runtimeErr.RuntimeUserMessage(), "routing rules") {
		t.Fatalf("expected user-facing message, got %q", runtimeErr.RuntimeUserMessage())
	}
	if runtimeErr.RuntimeDebug() != "iptables denied" {
		t.Fatalf("expected debug detail, got %q", runtimeErr.RuntimeDebug())
	}
	stageReport, ok := runtimeErr.RuntimeStageReport().(RuntimeStageReport)
	if !ok {
		t.Fatalf("expected runtime stage report, got %T", runtimeErr.RuntimeStageReport())
	}
	if stageReport.FailedStage != "netstack-apply" || stageReport.LastCode != "RULES_NOT_APPLIED" {
		t.Fatalf("unexpected stage report: %#v", stageReport)
	}
}

func TestRuntimeStageReportRecordsFailureAndFinish(t *testing.T) {
	report := NewRuntimeStageReport("hot-swap")
	report.AddStage("render-config", "ok", "", "/tmp/singbox.json", false)
	report.AddStage("wait-tproxy", "failed", "TPROXY_PORT_DOWN", "timeout", true)

	if report.Status != "failed" {
		t.Fatalf("expected failed report, got %#v", report)
	}
	if report.FailedStage != "wait-tproxy" || report.LastCode != "TPROXY_PORT_DOWN" {
		t.Fatalf("unexpected failed stage metadata: %#v", report)
	}
	if !report.RollbackApplied {
		t.Fatalf("rollback flag should be set: %#v", report)
	}
	if report.FinishedAt.IsZero() {
		t.Fatalf("failed report should have finished timestamp: %#v", report)
	}

	report = NewRuntimeStageReport("hot-swap")
	report.AddStage("render-config", "ok", "", "", false)
	report.FinishOK()
	if report.Status != "ok" || report.FinishedAt.IsZero() {
		t.Fatalf("expected ok finished report, got %#v", report)
	}
}

func TestSuccessfulStartReportUpdatesRuntimeReport(t *testing.T) {
	manager := NewCoreManager(config.DefaultConfig(), t.TempDir(), nil)
	report := NewRuntimeStageReport("start")
	report.AddStage("commit-state", "ok", "", "vless://example.com", false)

	manager.finishStartReport(report)

	if manager.LastRuntimeReport().Status != "ok" {
		t.Fatalf("successful start should leave runtime report finished: %#v", manager.LastRuntimeReport())
	}
	if manager.LastStartReport().Status != "ok" {
		t.Fatalf("successful start should leave start report finished: %#v", manager.LastStartReport())
	}
}

func TestKillProcessWaitsOnTrackedExitChannel(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "trap 'exit 0' TERM; while :; do sleep 1; done")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	manager := NewCoreManager(config.DefaultConfig(), t.TempDir(), nil)
	manager.process = cmd.Process
	manager.pid = cmd.Process.Pid
	manager.exitCh = watchCommand(cmd)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
	})

	if err := manager.killProcess(); err != nil {
		t.Fatalf("killProcess failed: %v", err)
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); err == nil {
		t.Fatalf("pid %d still exists after killProcess returned", cmd.Process.Pid)
	}
}

func TestStartStopsBeforeSpawnAndNetstackWhenConfigCheckFails(t *testing.T) {
	dataDir := t.TempDir()
	binDir := filepath.Join(dataDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	singBoxPath := filepath.Join(binDir, "sing-box")
	if err := os.WriteFile(singBoxPath, []byte("#!/bin/sh\necho invalid config >&2\nexit 2\n"), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	manager := NewCoreManager(cfg, dataDir, nil)

	err := manager.Start(cfg.ResolveProfile())
	if err == nil {
		t.Fatal("expected config-check failure")
	}
	runtimeErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T: %v", err, err)
	}
	if runtimeErr.RuntimeCode() != "CONFIG_CHECK_FAILED" {
		t.Fatalf("expected CONFIG_CHECK_FAILED, got %#v", runtimeErr)
	}
	if manager.GetState() != StateStopped {
		t.Fatalf("failed config check must leave core stopped, got %s", manager.GetState())
	}
	report := manager.LastStartReport()
	if report.FailedStage != "config-check" {
		t.Fatalf("expected config-check failed stage, got %#v", report)
	}
	for _, stage := range report.Stages {
		if stage.Name == "spawn-core" || stage.Name == "netstack-apply" {
			t.Fatalf("config-check failure must not reach %s: %#v", stage.Name, report)
		}
	}
}

func TestStartDoesNotRemoveExternalResetLock(t *testing.T) {
	dataDir := t.TempDir()
	runDir := filepath.Join(dataDir, "run")
	binDir := filepath.Join(dataDir, "bin")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	resetLock := filepath.Join(runDir, "reset.lock")
	if err := os.WriteFile(resetLock, []byte("external reset\n"), 0640); err != nil {
		t.Fatal(err)
	}
	singBoxPath := filepath.Join(binDir, "sing-box")
	if err := os.WriteFile(singBoxPath, []byte("#!/bin/sh\necho invalid config >&2\nexit 2\n"), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	manager := NewCoreManager(cfg, dataDir, nil)

	if err := manager.Start(cfg.ResolveProfile()); err == nil {
		t.Fatal("expected config-check failure")
	}
	if _, err := os.Stat(resetLock); err != nil {
		t.Fatalf("start must not remove reset.lock, stat err=%v", err)
	}
}

func TestHotSwapCleansNetstackWhenNewCoreExitsBeforeListeners(t *testing.T) {
	dataDir := t.TempDir()
	binDir := filepath.Join(dataDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	singBoxPath := filepath.Join(binDir, "sing-box")
	if err := os.WriteFile(singBoxPath, []byte("#!/bin/sh\nif [ \"$1\" = check ]; then exit 0; fi\necho run failed >&2\nexit 42\n"), 0755); err != nil {
		t.Fatal(err)
	}

	oldCmd := exec.Command("/bin/sh", "-c", "trap 'exit 0' TERM; while :; do sleep 1; done")
	if err := oldCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = oldCmd.Process.Kill()
	})

	cfg := config.DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	manager := NewCoreManager(cfg, dataDir, nil)
	manager.process = oldCmd.Process
	manager.pid = oldCmd.Process.Pid
	manager.exitCh = watchCommand(oldCmd)
	manager.state = StateRunning
	manager.activeProfile = "vless://old.example"
	if err := os.MkdirAll(filepath.Join(dataDir, "run"), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dataDir, "run", "active"), []byte("active\n"), 0640)

	fake := &fakeCoreNetstack{}
	manager.netstackFactory = func() coreNetstack { return fake }

	err := manager.HotSwap(cfg.ResolveProfile())
	if err == nil {
		t.Fatal("expected hot-swap listener failure")
	}
	runtimeErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T: %v", err, err)
	}
	if !runtimeErr.RuntimeRollbackApplied() {
		t.Fatalf("hot-swap listener failure must be reported as rollback-applied: %#v", runtimeErr)
	}
	if fake.cleanupCalls != 1 {
		t.Fatalf("hot-swap failure must cleanup netstack exactly once, got %d", fake.cleanupCalls)
	}
	if manager.GetState() != StateDegraded {
		t.Fatalf("hot-swap failure should leave degraded after cleanup, got %s", manager.GetState())
	}
	if manager.pid != 0 || manager.process != nil || manager.activeProfile != "" {
		t.Fatalf("hot-swap cleanup left stale process state: pid=%d process=%v profile=%q", manager.pid, manager.process, manager.activeProfile)
	}
}

func TestStartCleansNetstackWhenVerifyFailsAfterApply(t *testing.T) {
	dataDir := t.TempDir()
	binDir := filepath.Join(dataDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	tproxyPort := freeTCPPort(t)
	dnsPort := freeTCPPort(t)
	singBoxPath := filepath.Join(binDir, "sing-box")
	script := fmt.Sprintf("#!/bin/sh\nGO_WANT_SINGBOX_HELPER=1 FAKE_TPROXY_PORT=%d FAKE_DNS_PORT=%d exec %s -test.run=TestSingBoxHelperProcess -- \"$@\"\n", tproxyPort, dnsPort, strconv.Quote(os.Args[0]))
	if err := os.WriteFile(singBoxPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Proxy.TProxyPort = tproxyPort
	cfg.Proxy.DNSPort = dnsPort
	cfg.Proxy.APIPort = 0
	cfg.Proxy.GID = os.Getegid()
	manager := NewCoreManager(cfg, dataDir, nil)
	manager.disableCoreCredentialForTest = true
	fake := &fakeCoreNetstack{verifyReport: netstack.Report{
		Operation: "verify",
		Status:    "failed",
		Steps: []netstack.Step{{
			Name:   "iptables-status",
			Status: "failed",
			Detail: "missing IPv4 hook",
		}},
		Errors: []string{"iptables-status: missing IPv4 hook"},
	}}
	manager.netstackFactory = func() coreNetstack { return fake }

	err := manager.Start(cfg.ResolveProfile())
	if err == nil {
		t.Fatal("expected netstack verify failure")
	}
	runtimeErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T: %v", err, err)
	}
	if runtimeErr.RuntimeCode() != "NETSTACK_VERIFY_FAILED" {
		t.Fatalf("expected NETSTACK_VERIFY_FAILED, got %#v", runtimeErr)
	}
	if !runtimeErr.RuntimeRollbackApplied() {
		t.Fatalf("verify failure must be reported as rollback-applied: %#v", runtimeErr)
	}
	if fake.applyCalls != 1 || fake.verifyCalls != 1 || fake.cleanupCalls != 1 {
		t.Fatalf("unexpected netstack calls: apply=%d verify=%d cleanup=%d", fake.applyCalls, fake.verifyCalls, fake.cleanupCalls)
	}
	if manager.GetState() != StateStopped {
		t.Fatalf("verify rollback should leave stopped, got %s", manager.GetState())
	}
	if manager.pid != 0 || manager.process != nil || manager.activeProfile != "" {
		t.Fatalf("verify rollback left stale process state: pid=%d process=%v profile=%q", manager.pid, manager.process, manager.activeProfile)
	}
}

type fakeCoreNetstack struct {
	cleanupCalls int
	applyCalls   int
	verifyCalls  int
	applyReport  netstack.Report
	verifyReport netstack.Report
}

func (f *fakeCoreNetstack) Apply() netstack.Report {
	f.applyCalls++
	if f.applyReport.Operation != "" || len(f.applyReport.Errors) > 0 {
		return f.applyReport
	}
	return netstack.Report{Operation: "apply", Status: "ok"}
}

func (f *fakeCoreNetstack) Cleanup() netstack.Report {
	f.cleanupCalls++
	return netstack.Report{Operation: "cleanup", Status: "ok"}
}

func (f *fakeCoreNetstack) Verify() netstack.Report {
	f.verifyCalls++
	if f.verifyReport.Operation != "" || len(f.verifyReport.Errors) > 0 {
		return f.verifyReport
	}
	return netstack.Report{Operation: "verify", Status: "ok"}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func TestSingBoxHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_SINGBOX_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(2)
	}
	switch args[0] {
	case "check":
		os.Exit(0)
	case "run":
		tproxyListener, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("FAKE_TPROXY_PORT"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		defer tproxyListener.Close()
		dnsListener, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("FAKE_DNS_PORT"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		defer dnsListener.Close()
		select {}
	default:
		os.Exit(2)
	}
}

func TestSingBoxConfigCheckTimeout(t *testing.T) {
	dataDir := t.TempDir()
	singBoxPath := filepath.Join(dataDir, "sing-box")
	if err := os.WriteFile(singBoxPath, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0755); err != nil {
		t.Fatal(err)
	}

	err := runSingBoxConfigCheck(singBoxPath, filepath.Join(dataDir, "singbox.json"), 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected config check timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestScriptEnvIncludesLocalHelperPorts(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Profile.Inbounds = []byte(`{"socksPort":10808,"httpPort":10809}`)
	manager := NewCoreManager(cfg, t.TempDir(), nil)

	env := manager.scriptEnv()
	if env["SOCKS_PORT"] != "10808" {
		t.Fatalf("expected SOCKS_PORT=10808, got %q", env["SOCKS_PORT"])
	}
	if env["HTTP_PORT"] != "10809" {
		t.Fatalf("expected HTTP_PORT=10809, got %q", env["HTTP_PORT"])
	}
}

func TestScriptEnvDisablesLocalHelperPortsByDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	manager := NewCoreManager(cfg, t.TempDir(), nil)

	env := manager.scriptEnv()
	if env["SOCKS_PORT"] != "0" {
		t.Fatalf("default SOCKS_PORT must stay disabled, got %q", env["SOCKS_PORT"])
	}
	if env["HTTP_PORT"] != "0" {
		t.Fatalf("default HTTP_PORT must stay disabled, got %q", env["HTTP_PORT"])
	}
}

func TestRuntimeListenerWaitsIncludeDNSAndOptionalAPI(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Proxy.TProxyPort = 19053
	cfg.Proxy.DNSPort = 19056
	cfg.Proxy.APIPort = 19090
	manager := NewCoreManager(cfg, t.TempDir(), nil)

	specs := manager.runtimeListenerWaits()
	if len(specs) != 3 {
		t.Fatalf("expected tproxy, DNS, and API listener waits, got %#v", specs)
	}
	expected := []struct {
		stage string
		code  string
		port  int
	}{
		{"wait-tproxy", "TPROXY_PORT_DOWN", 19053},
		{"wait-dns", "DNS_LISTENER_DOWN", 19056},
		{"wait-api", "API_PORT_DOWN", 19090},
	}
	for i, want := range expected {
		if specs[i].Stage != want.stage || specs[i].Code != want.code || specs[i].Port != want.port {
			t.Fatalf("unexpected listener wait %d: got %#v want %#v", i, specs[i], want)
		}
	}
}

func TestRuntimeListenerWaitsSkipDisabledAPI(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Proxy.APIPort = 0
	manager := NewCoreManager(cfg, t.TempDir(), nil)

	specs := manager.runtimeListenerWaits()
	if len(specs) != 2 {
		t.Fatalf("disabled API should leave only tproxy and DNS waits, got %#v", specs)
	}
	if specs[0].Port != 10853 || specs[1].Port != 10856 {
		t.Fatalf("default listener ports not applied: %#v", specs)
	}
}

func TestWaitForPortOrExitRejectsIPv6OnlyLoopbackListener(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback is unavailable: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	manager := NewCoreManager(config.DefaultConfig(), t.TempDir(), nil)
	exitCh := make(chan error)

	if err := manager.waitForPortOrExit(port, time.Second, exitCh, ""); err == nil {
		t.Fatal("IPv6-only loopback listener must not satisfy IPv4 TPROXY readiness")
	}
}
