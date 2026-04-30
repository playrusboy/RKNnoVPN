package applytx

import (
	"errors"
	"reflect"
	"testing"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func TestConfigTransactionRunsInOrderAndReportsRuntimeState(t *testing.T) {
	cfg := config.DefaultConfig()
	var calls []string

	result, err := ConfigTransaction{
		Action: "config-import",
		EnsureIdle: func() error {
			calls = append(calls, "ensure-idle")
			return nil
		},
		SaveProfile: func(next *config.Config) error {
			if next != cfg {
				t.Fatalf("unexpected config pointer")
			}
			calls = append(calls, "save-profile")
			return nil
		},
		RuntimeRunning: func() bool {
			calls = append(calls, "runtime-running")
			return true
		},
		ApplyConfig: func(next *config.Config, reload bool, operation runtimev2.OperationKind) error {
			if next != cfg || !reload {
				t.Fatalf("unexpected apply args")
			}
			if operation != runtimev2.OperationConfigMutation {
				t.Fatalf("unexpected runtime operation: %s", operation)
			}
			calls = append(calls, "apply-config")
			return nil
		},
	}.Run(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ConfigSaved || !result.RuntimeWasRunning {
		t.Fatalf("unexpected transaction result: %#v", result)
	}
	want := []string{"ensure-idle", "save-profile", "runtime-running", "apply-config"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected call order: got %#v want %#v", calls, want)
	}
}

func TestConfigTransactionStopsBeforeSaveWhenRuntimeBusy(t *testing.T) {
	cfg := config.DefaultConfig()
	busyErr := errors.New("busy")
	saved := false

	result, err := ConfigTransaction{
		Action: "profile.apply",
		EnsureIdle: func() error {
			return busyErr
		},
		SaveProfile: func(*config.Config) error {
			saved = true
			return nil
		},
		ApplyConfig: func(*config.Config, bool, runtimev2.OperationKind) error {
			t.Fatal("apply must not run")
			return nil
		},
	}.Run(cfg, true)
	if !errors.Is(err, busyErr) {
		t.Fatalf("expected busy error, got %v", err)
	}
	if saved || result.ConfigSaved {
		t.Fatalf("busy transaction must not save config: saved=%v result=%#v", saved, result)
	}
}

func TestConfigTransactionReportsSavedApplyFailure(t *testing.T) {
	cfg := config.DefaultConfig()
	applyErr := errors.New("apply failed")

	result, err := ConfigTransaction{
		Action: "profile.apply",
		SaveProfile: func(*config.Config) error {
			return nil
		},
		RuntimeRunning: func() bool {
			return true
		},
		ApplyConfig: func(*config.Config, bool, runtimev2.OperationKind) error {
			return applyErr
		},
	}.Run(cfg, true)
	if !errors.Is(err, applyErr) {
		t.Fatalf("expected apply error, got %v", err)
	}
	if !result.ConfigSaved || !result.RuntimeWasRunning {
		t.Fatalf("saved apply failure must preserve transaction state: %#v", result)
	}
}

func TestConfigTransactionRejectsIncompleteWiring(t *testing.T) {
	cfg := config.DefaultConfig()
	if _, err := (ConfigTransaction{}).Run(nil, false); err == nil {
		t.Fatal("nil config must fail")
	}
	if result, err := (ConfigTransaction{}).Run(cfg, false); err == nil || result.ConfigSaved {
		t.Fatalf("missing action must fail before save, result=%#v err=%v", result, err)
	}
	if result, err := (ConfigTransaction{
		Action: "profile.apply",
	}).Run(cfg, false); err == nil || result.ConfigSaved {
		t.Fatalf("missing save callback must fail before save, result=%#v err=%v", result, err)
	}
	if result, err := (ConfigTransaction{
		Action:      "profile.apply",
		SaveProfile: func(*config.Config) error { return nil },
	}).Run(cfg, false); err == nil || !result.ConfigSaved {
		t.Fatalf("missing apply callback must fail after save, result=%#v err=%v", result, err)
	}
}

func TestRuntimeOperationForAction(t *testing.T) {
	cases := map[string]runtimev2.OperationKind{
		"config-import":             runtimev2.OperationConfigMutation,
		"profile.apply":             runtimev2.OperationProfileApply,
		"profile.importNodes":       runtimev2.OperationProfileApply,
		"profile.setActiveNode":     runtimev2.OperationProfileApply,
		"subscription.refresh":      runtimev2.OperationProfileApply,
		"backend.applyDesiredState": runtimev2.OperationApplyDesiredState,
	}
	for action, want := range cases {
		got, ok := RuntimeOperationForAction(action)
		if !ok || got != want {
			t.Fatalf("action %s mapped to %s, want %s", action, got, want)
		}
	}
	if got, ok := RuntimeOperationForAction("unknown-profile-like-action"); ok || got != "" {
		t.Fatalf("unknown action mapped to %s, ok=%v", got, ok)
	}
}
