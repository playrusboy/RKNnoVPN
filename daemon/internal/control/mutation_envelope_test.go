package control

import (
	"testing"

	applytx "github.com/youtubediscord/RKNnoVPN/daemon/internal/apply"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func TestConfigMutationOperationDocumentsTransactionStages(t *testing.T) {
	op := configMutationOperation("config-import", "accepted", true, true, false, "accepted", -1, "", "", nil)
	stages, ok := op["stages"].([]map[string]interface{})
	if !ok {
		t.Fatalf("operation stages missing: %#v", op)
	}
	for _, name := range []string{"validate", "render", "runtime-idle", "persist-draft", "runtime-apply", "verify", "commit-generation", "cleanup"} {
		if !hasStage(stages, name) {
			t.Fatalf("operation missing stage %s: %#v", name, stages)
		}
	}
	if status := stageStatusByName(stages, "cleanup"); status != "skipped" {
		t.Fatalf("cleanup without reset report should be skipped, got %q in %#v", status, stages)
	}
	if op["accepted"] != true || op["operationActive"] != true {
		t.Fatalf("accepted async mutation must be explicit: %#v", op)
	}
}

func TestProfileOperationMapsResetReportRollback(t *testing.T) {
	resetReport := struct {
		Status string
	}{Status: "ok"}
	result := ProfileOperation("profile.apply", "saved_not_applied", true, false, "failed", 2, 1, "CORE_SPAWN_FAILED", "boom", resetReport, nil, -1)
	if result["rollback"] != "cleanup_succeeded" {
		t.Fatalf("reset report status should drive rollback, got %#v", result)
	}
	if result["resetReport"] == nil {
		t.Fatalf("reset report should remain visible: %#v", result)
	}
}

func TestProfileSuccessSurfacesHotSwapReason(t *testing.T) {
	status := runtimev2.Status{}
	result := ProfileSuccessWithRuntimeApply(
		"profile.setActiveNode",
		true,
		true,
		status,
		status,
		nil,
		1,
		applytx.ConfigTransactionResult{
			RuntimeApplyMode:   "hot-swap",
			RuntimeApplyReason: "new-active-node-requires-xray-sidecar",
			RequiresHotSwap:    true,
		},
	)
	if result["requiresHotSwap"] != true || result["runtimeApplyReason"] != "new-active-node-requires-xray-sidecar" {
		t.Fatalf("hotswap reason missing from result: %#v", result)
	}
	operation := result["operation"].(map[string]interface{})
	if operation["requiresHotSwap"] != true || operation["runtimeApplyMode"] != "hot-swap" {
		t.Fatalf("hotswap reason missing from operation: %#v", operation)
	}
}

func hasStage(stages []map[string]interface{}, name string) bool {
	return stageStatusByName(stages, name) != ""
}

func stageStatusByName(stages []map[string]interface{}, name string) string {
	for _, stage := range stages {
		if stage["name"] == name {
			status, _ := stage["status"].(string)
			return status
		}
	}
	return ""
}
