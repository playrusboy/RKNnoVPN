package root

import (
	"testing"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/netstack"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func TestReloadAfterConfigChangeSkipsNetstackReapplyForHotSwap(t *testing.T) {
	reapplyCalled := false
	var lastReport core.RuntimeStageReport

	err := ReloadAfterConfigChange(
		ConfigReloadInput{
			Config:     config.DefaultConfig(),
			Context:    "apply config",
			SavedLabel: "config saved",
			Generation: 7,
		},
		ConfigReloadDeps{
			HotSwap: func(profile *config.NodeProfile) error { return nil },
			LastRuntimeReport: func() core.RuntimeStageReport {
				report := core.NewRuntimeStageReport("hot-swap")
				report.FinishOK()
				return report
			},
			ReapplyRuntimeRules: func(cfg *config.Config) (netstack.Report, error) {
				reapplyCalled = true
				return netstack.Report{}, nil
			},
			RefreshHealth: func() runtimev2.HealthSnapshot {
				return runtimev2.HealthSnapshot{
					CoreReady:    true,
					RoutingReady: true,
					DNSReady:     true,
					EgressReady:  true,
				}
			},
			ObserveReloadReport: func(report core.RuntimeStageReport) {
				lastReport = report
			},
		},
	)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if reapplyCalled {
		t.Fatal("hot-swap reload should not reapply netstack when runtime env is unchanged")
	}
	if !reportHasStage(lastReport, "netstack-reapply", "skipped") {
		t.Fatalf("expected skipped netstack-reapply stage, got %#v", lastReport)
	}
}

func TestReloadAfterConfigChangeCanReapplyNetstackAfterHotSwap(t *testing.T) {
	reapplyCalled := false
	var lastReport core.RuntimeStageReport

	err := ReloadAfterConfigChange(
		ConfigReloadInput{
			Config:               config.DefaultConfig(),
			Context:              "apply config",
			SavedLabel:           "config saved",
			Generation:           8,
			NetstackReapplyAfter: true,
		},
		ConfigReloadDeps{
			HotSwap: func(profile *config.NodeProfile) error { return nil },
			LastRuntimeReport: func() core.RuntimeStageReport {
				report := core.NewRuntimeStageReport("hot-swap")
				report.FinishOK()
				return report
			},
			ReapplyRuntimeRules: func(cfg *config.Config) (netstack.Report, error) {
				reapplyCalled = true
				return netstack.Report{
					Operation: "apply",
					Status:    "ok",
					Steps: []netstack.Step{
						{Name: "iptables-start", Status: "ok"},
					},
				}, nil
			},
			RefreshHealth: func() runtimev2.HealthSnapshot {
				return runtimev2.HealthSnapshot{
					CoreReady:    true,
					RoutingReady: true,
					DNSReady:     true,
					EgressReady:  true,
				}
			},
			ObserveReloadReport: func(report core.RuntimeStageReport) {
				lastReport = report
			},
		},
	)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if !reapplyCalled {
		t.Fatal("hot-swap reload should reapply netstack when runtime policy changed")
	}
	if !reportHasStage(lastReport, "netstack-reapply", "ok") {
		t.Fatalf("expected ok netstack-reapply stage, got %#v", lastReport)
	}
}

func TestPlanReloadSeparatesCoreRestartFromNetstackReapply(t *testing.T) {
	base := map[string]string{
		"TPROXY_PORT": "10853",
		"DNS_PORT":    "10856",
		"PROXY_UIDS":  "10001",
	}
	routingChanged := map[string]string{
		"TPROXY_PORT": "10853",
		"DNS_PORT":    "10856",
		"PROXY_UIDS":  "10002",
	}
	portChanged := map[string]string{
		"TPROXY_PORT": "10854",
		"DNS_PORT":    "10856",
		"PROXY_UIDS":  "10001",
	}

	plan := PlanReload(base, routingChanged)
	if plan.FullRestart {
		t.Fatalf("routing-only policy changes should not force core restart: %#v", plan)
	}
	if !plan.NetstackReapplyAfter {
		t.Fatalf("routing-only policy changes should reapply netstack: %#v", plan)
	}

	plan = PlanReload(base, portChanged)
	if !plan.FullRestart || plan.NetstackReapplyAfter {
		t.Fatalf("listener port changes should use full restart only: %#v", plan)
	}
}

func reportHasStage(report core.RuntimeStageReport, name string, status string) bool {
	for _, stage := range report.Stages {
		if stage.Name == name && stage.Status == status {
			return true
		}
	}
	return false
}
