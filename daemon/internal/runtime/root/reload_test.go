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

func reportHasStage(report core.RuntimeStageReport, name string, status string) bool {
	for _, stage := range report.Stages {
		if stage.Name == name && stage.Status == status {
			return true
		}
	}
	return false
}
