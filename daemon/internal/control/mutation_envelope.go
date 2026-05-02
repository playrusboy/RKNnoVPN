package control

import (
	"errors"
	"reflect"

	applytx "github.com/youtubediscord/RKNnoVPN/daemon/internal/apply"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimeerr"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

type RuntimeStatusFunc func() (runtimev2.Status, bool)

func MutationSuccess(action string, status string, reload bool, runtimeWasRunning bool, updated int, runtimeStatus RuntimeStatusFunc) map[string]interface{} {
	result := mutationSuccess(action, status, reload, runtimeWasRunning, updated)
	if runtimeStatus != nil {
		status, ok := runtimeStatus()
		if ok {
			operation, _ := result["operation"].(map[string]interface{})
			AttachMutationGenerations(result, operation, status)
			result["runtimeStatus"] = status
		}
	}
	return result
}

func MutationRPCErrorSaved(action string, err error, saved bool, runtimeStatus RuntimeStatusFunc) *ipc.RPCError {
	var busy *runtimev2.OperationBusyError
	if errors.As(err, &busy) {
		rpcErr := &ipc.RPCError{
			Code:    ipc.CodeRuntimeBusy,
			Message: busy.Error(),
			Data:    MutationErrorData(action, err, saved, runtimeStatus),
		}
		rpcErr.Data.(map[string]interface{})["busy"] = busy.Data()
		return rpcErr
	}
	var validation applytx.ConfigValidationError
	if errors.As(err, &validation) {
		return &ipc.RPCError{
			Code:    ipc.CodeConfigError,
			Message: err.Error(),
			Data:    MutationErrorData(action, err, saved, runtimeStatus),
		}
	}
	rpcErr := &ipc.RPCError{
		Code:    ipc.CodeInternalError,
		Message: err.Error(),
	}
	rpcErr.Data = MutationErrorData(action, err, saved, runtimeStatus)
	return rpcErr
}

func MutationErrorData(action string, err error, saved bool, runtimeStatus RuntimeStatusFunc) map[string]interface{} {
	code := runtimeerr.Code(err, "CONFIG_APPLY_FAILED")
	resetReport := runtimeerr.ResetReportFromError(err)
	data := mutationErrorData(action, saved, code, err.Error(), resetReport)
	if runtimeStatus != nil {
		status, ok := runtimeStatus()
		if ok {
			operation, _ := data["operation"].(map[string]interface{})
			AttachMutationGenerations(data, operation, status)
			data["runtimeStatus"] = status
		}
	}
	return data
}

func AttachMutationGenerations(result map[string]interface{}, operation map[string]interface{}, status runtimev2.Status) {
	appliedGeneration := status.AppliedState.Generation
	desiredGeneration := appliedGeneration
	if status.ActiveOperation != nil {
		desiredGeneration = status.ActiveOperation.Generation
	} else if result["config_saved"] == true || result["configSaved"] == true {
		desiredGeneration = appliedGeneration + 1
	}
	result["desiredGeneration"] = desiredGeneration
	result["appliedGeneration"] = appliedGeneration
	if operation != nil {
		operation["desiredGeneration"] = desiredGeneration
		operation["appliedGeneration"] = appliedGeneration
	}
}

func RuntimeApplyStatus(reload bool, runtimeWasRunning bool) string {
	switch {
	case reload && runtimeWasRunning:
		return "accepted"
	case reload:
		return "skipped_runtime_stopped"
	default:
		return "not_requested"
	}
}

func mutationSuccess(action string, status string, reload bool, runtimeWasRunning bool, updated int) map[string]interface{} {
	runtimeApply := RuntimeApplyStatus(reload, runtimeWasRunning)
	runtimeApplied := runtimeApply == "applied"
	accepted := runtimeApply == "accepted"
	if accepted {
		status = "accepted"
	}
	operation := configMutationOperation(action, status, true, reload, runtimeApplied, runtimeApply, updated, "", "", nil)
	result := map[string]interface{}{
		"ok":               true,
		"status":           status,
		"reload":           reload,
		"config_saved":     true,
		"runtime_applied":  runtimeApplied,
		"runtime_apply":    runtimeApply,
		"accepted":         accepted,
		"operation_active": accepted,
		"operation":        operation,
	}
	if updated >= 0 {
		result["updated"] = updated
	}
	return result
}

func mutationErrorData(action string, saved bool, code string, message string, resetReport interface{}) map[string]interface{} {
	runtimeApply := "not_started"
	status := "failed"
	if saved {
		runtimeApply = "failed"
		status = "saved_not_applied"
	}
	operation := configMutationOperation(action, status, saved, saved, false, runtimeApply, -1, code, message, resetReport)
	data := map[string]interface{}{
		"ok":              false,
		"status":          status,
		"config_saved":    saved,
		"runtime_applied": false,
		"message":         message,
		"code":            code,
		"runtime_apply":   runtimeApply,
		"operation":       operation,
	}
	if resetReport != nil {
		data["resetReport"] = resetReport
	}
	return data
}

func configMutationOperation(action string, status string, saved bool, reload bool, runtimeApplied bool, runtimeApply string, updated int, code string, message string, resetReport interface{}) map[string]interface{} {
	const operationType = "config-mutation"
	rollback := rollbackStatus(saved, status, resetReport)
	stages := mutationStages(operationType, status, saved, runtimeApplyStage(reload, runtimeApply), runtimeApply, code, resetReport)
	operation := map[string]interface{}{
		"type":            operationType,
		"action":          action,
		"status":          status,
		"configSaved":     saved,
		"runtimeApplied":  runtimeApplied,
		"runtimeApply":    runtimeApply,
		"accepted":        runtimeApply == "accepted",
		"operationActive": runtimeApply == "accepted",
		"rollback":        rollback,
		"stages":          stages,
	}
	if updated >= 0 {
		operation["updated"] = updated
	}
	if code != "" {
		operation["code"] = code
	}
	if message != "" {
		operation["message"] = message
	}
	return operation
}

func ProfileOperation(
	action string,
	status string,
	configSaved bool,
	runtimeApplied bool,
	runtimeApply string,
	desiredGeneration int64,
	appliedGeneration int64,
	code string,
	message string,
	resetReport interface{},
	warnings []profiledoc.Warning,
	updated int,
) map[string]interface{} {
	const operationType = "profile-apply"
	rollback := rollbackStatus(configSaved, status, resetReport)
	stages := mutationStages(operationType, status, configSaved, runtimeApply, runtimeApply, code, resetReport)
	warningItems := make([]map[string]string, 0, len(warnings))
	for _, warning := range warnings {
		warningItems = append(warningItems, map[string]string{
			"code":    warning.Code,
			"message": warning.Message,
		})
	}
	result := map[string]interface{}{
		"status":            status,
		"configSaved":       configSaved,
		"config_saved":      configSaved,
		"runtimeApplied":    runtimeApplied,
		"runtime_applied":   runtimeApplied,
		"runtimeApply":      runtimeApply,
		"runtime_apply":     runtimeApply,
		"desiredGeneration": desiredGeneration,
		"appliedGeneration": appliedGeneration,
		"rollback":          rollback,
		"stages":            stages,
		"warnings":          warningItems,
		"operation": map[string]interface{}{
			"type":              operationType,
			"action":            action,
			"status":            status,
			"configSaved":       configSaved,
			"runtimeApplied":    runtimeApplied,
			"runtimeApply":      runtimeApply,
			"desiredGeneration": desiredGeneration,
			"appliedGeneration": appliedGeneration,
			"rollback":          rollback,
			"stages":            stages,
			"warnings":          warningItems,
		},
	}
	if updated >= 0 {
		result["updated"] = updated
		result["operation"].(map[string]interface{})["updated"] = updated
	}
	if code != "" {
		result["code"] = code
		result["operation"].(map[string]interface{})["code"] = code
	}
	if message != "" {
		result["message"] = message
		result["operation"].(map[string]interface{})["message"] = message
	}
	if resetReport != nil {
		result["resetReport"] = resetReport
	}
	return result
}

func ProfileValidationRPCError(action string, before runtimev2.Status, err error, warnings []profiledoc.Warning, updated int) *ipc.RPCError {
	return &ipc.RPCError{
		Code:    ipc.CodeConfigError,
		Message: "profile validation failed: " + err.Error(),
		Data: ProfileOperation(
			action,
			"failed",
			false,
			false,
			"not_started",
			ProfileDesiredGeneration(before, before),
			before.AppliedState.Generation,
			"CONFIG_VALIDATION_FAILED",
			err.Error(),
			nil,
			warnings,
			updated,
		),
	}
}

func ProfileRPCErrorSaved(action string, err error, saved bool, status runtimev2.Status, before runtimev2.Status, warnings []profiledoc.Warning, updated int) *ipc.RPCError {
	resultStatus := "failed"
	runtimeApply := "not_started"
	if saved {
		resultStatus = "saved_not_applied"
		runtimeApply = "failed"
	}
	data := ProfileOperation(
		action,
		resultStatus,
		saved,
		false,
		runtimeApply,
		ProfileDesiredGeneration(status, before),
		status.AppliedState.Generation,
		runtimeerr.Code(err, "PROFILE_APPLY_FAILED"),
		err.Error(),
		runtimeerr.ResetReportFromError(err),
		warnings,
		updated,
	)
	var busy *runtimev2.OperationBusyError
	if errors.As(err, &busy) {
		data["busy"] = busy.Data()
		return &ipc.RPCError{
			Code:    ipc.CodeRuntimeBusy,
			Message: busy.Error(),
			Data:    data,
		}
	}
	var validation applytx.ConfigValidationError
	if errors.As(err, &validation) {
		return &ipc.RPCError{
			Code:    ipc.CodeConfigError,
			Message: err.Error(),
			Data:    data,
		}
	}
	return &ipc.RPCError{
		Code:    ipc.CodeInternalError,
		Message: err.Error(),
		Data:    data,
	}
}

func ProfileSuccess(action string, reload bool, runtimeWasRunning bool, status runtimev2.Status, before runtimev2.Status, warnings []profiledoc.Warning, updated int) map[string]interface{} {
	return ProfileSuccessWithRuntimeApply(action, reload, runtimeWasRunning, status, before, warnings, updated, applytx.ConfigTransactionResult{})
}

func ProfileSuccessWithRuntimeApply(action string, reload bool, runtimeWasRunning bool, status runtimev2.Status, before runtimev2.Status, warnings []profiledoc.Warning, updated int, mutation applytx.ConfigTransactionResult) map[string]interface{} {
	runtimeApply := RuntimeApplyStatus(reload, runtimeWasRunning)
	if mutation.RuntimeApply != "" {
		runtimeApply = mutation.RuntimeApply
	}
	runtimeApplied := runtimeApply == "applied"
	if runtimeApply == "accepted" {
		runtimeApplied = false
	}
	resultStatus := "ok"
	if runtimeApply == "skipped_runtime_stopped" {
		resultStatus = "saved"
	}
	result := ProfileOperation(
		action,
		resultStatus,
		true,
		runtimeApplied,
		runtimeApply,
		ProfileDesiredGeneration(status, before),
		status.AppliedState.Generation,
		"",
		"",
		nil,
		warnings,
		updated,
	)
	result["ok"] = true
	result["runtimeStatus"] = status
	if mutation.RuntimeApplyMode != "" {
		result["runtimeApplyMode"] = mutation.RuntimeApplyMode
		result["operation"].(map[string]interface{})["runtimeApplyMode"] = mutation.RuntimeApplyMode
	}
	if mutation.RuntimeApplyReason != "" {
		result["runtimeApplyReason"] = mutation.RuntimeApplyReason
		result["operation"].(map[string]interface{})["runtimeApplyReason"] = mutation.RuntimeApplyReason
	}
	if mutation.RequiresHotSwap {
		result["requiresHotSwap"] = true
		result["operation"].(map[string]interface{})["requiresHotSwap"] = true
	}
	return result
}

func ProfileDesiredGeneration(status runtimev2.Status, before runtimev2.Status) int64 {
	if status.ActiveOperation != nil {
		return status.ActiveOperation.Generation
	}
	if status.AppliedState.Generation > before.AppliedState.Generation {
		return status.AppliedState.Generation
	}
	return before.AppliedState.Generation + 1
}

func updateDownloadOperation(status string, code string, message string) map[string]interface{} {
	const operationType = "update-download"
	stageStatuses := map[string]string{
		"update-check":      "ok",
		"update-download":   "ok",
		"update-verify":     "ok",
		"persist-artifacts": "ok",
	}
	if status == "failed" {
		failedStage := updateDownloadFailedStage(code)
		for name := range stageStatuses {
			stageStatuses[name] = "not_started"
		}
		policy, _ := ipc.OperationPolicyForType(operationType)
		for _, name := range policy.Stages {
			stageStatuses[name] = "ok"
			if name == failedStage {
				stageStatuses[name] = "failed"
				break
			}
		}
	}
	policy, _ := ipc.OperationPolicyForType(operationType)
	operation := map[string]interface{}{
		"type":   operationType,
		"action": "update-download",
		"status": status,
		"stages": mutationStagesInOrder(policy.Stages, stageStatuses),
	}
	if code != "" {
		operation["code"] = code
	}
	if message != "" {
		operation["message"] = message
	}
	return operation
}

func updateDownloadFailedStage(code string) string {
	switch code {
	case "UPDATE_CHECK_FAILED", "UPDATE_NOT_AVAILABLE":
		return "update-check"
	case "UPDATE_DOWNLOAD_FAILED":
		return "update-download"
	case "UPDATE_VERIFY_FAILED":
		return "update-verify"
	default:
		return "persist-artifacts"
	}
}

func mutationStages(operationType string, status string, saved bool, runtimeApplyStageStatus string, runtimeApply string, code string, resetReport interface{}) []map[string]interface{} {
	validateStatus, renderStatus := validationStageStatuses(status, saved, code)
	stageStatuses := map[string]string{
		"validate":          validateStatus,
		"render":            renderStatus,
		"runtime-idle":      runtimeIdleStage(status, saved, code),
		"persist-draft":     persistDraftStage(status, saved, code),
		"runtime-apply":     runtimeApplyStageStatus,
		"verify":            verifyStage(status, runtimeApply, resetReport),
		"commit-generation": commitStage(status, runtimeApply, saved),
		"cleanup":           cleanupStage(resetReport),
	}
	policy, _ := ipc.OperationPolicyForType(operationType)
	return mutationStagesInOrder(policy.Stages, stageStatuses)
}

func mutationStagesInOrder(names []string, statuses map[string]string) []map[string]interface{} {
	stages := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		stageStatus := statuses[name]
		if stageStatus == "" {
			stageStatus = "not_started"
		}
		stages = append(stages, map[string]interface{}{"name": name, "status": stageStatus})
	}
	return stages
}

func validationStageStatuses(status string, saved bool, code string) (string, string) {
	if status != "failed" || saved {
		return "ok", "ok"
	}
	switch code {
	case "CONFIG_VALIDATION_FAILED":
		return "failed", "not_started"
	case "CONFIG_RENDER_FAILED":
		return "ok", "failed"
	default:
		return "ok", "ok"
	}
}

func runtimeIdleStage(status string, saved bool, code string) string {
	if status != "failed" || saved {
		return "ok"
	}
	if runtimeIdleFailed(code) {
		return "failed"
	}
	return "not_started"
}

func runtimeIdleFailed(code string) bool {
	return code == "RUNTIME_BUSY" || code == "RESET_IN_PROGRESS"
}

func persistDraftStage(status string, saved bool, code string) string {
	if status == "failed" && !saved && (code == "CONFIG_VALIDATION_FAILED" || code == "CONFIG_RENDER_FAILED" || runtimeIdleFailed(code)) {
		return "not_started"
	}
	return stageStatus(saved, status == "failed")
}

func runtimeApplyStage(reload bool, runtimeApply string) string {
	if runtimeApply == "not_started" {
		return "not_started"
	}
	if reload {
		return runtimeApply
	}
	return "not_requested"
}

func verifyStage(status string, runtimeApply string, resetReport interface{}) string {
	if resetReport != nil {
		return "failed"
	}
	if status == "failed" || status == "saved_not_applied" {
		return "not_started"
	}
	if runtimeApply == "accepted" {
		return "accepted"
	}
	if runtimeApply == "skipped_runtime_stopped" || runtimeApply == "not_requested" {
		return "skipped"
	}
	return "ok"
}

func commitStage(status string, runtimeApply string, saved bool) string {
	if !saved || status == "failed" || status == "saved_not_applied" {
		return "not_started"
	}
	if runtimeApply == "accepted" {
		return "accepted"
	}
	return "ok"
}

func cleanupStage(resetReport interface{}) string {
	if resetReport == nil {
		return "skipped"
	}
	if status := resetReportStatus(resetReport); status != "" {
		return status
	}
	return "failed"
}

func rollbackStatus(saved bool, status string, resetReport interface{}) string {
	if resetReport != nil {
		if resetReportStatus(resetReport) == "ok" {
			return "cleanup_succeeded"
		}
		return "cleanup_incomplete"
	}
	if saved && status == "failed" {
		return "unknown"
	}
	return "not_needed"
}

func resetReportStatus(resetReport interface{}) string {
	if m, ok := resetReport.(interface{ ResetStatus() string }); ok {
		return m.ResetStatus()
	}
	if m, ok := resetReport.(map[string]interface{}); ok {
		if status, ok := m["status"].(string); ok && status != "" {
			return status
		}
	}
	value := reflect.ValueOf(resetReport)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		field := value.FieldByName("Status")
		if field.IsValid() && field.Kind() == reflect.String {
			return field.String()
		}
	}
	return ""
}

func stageStatus(ok bool, failed bool) string {
	if ok {
		return "ok"
	}
	if failed {
		return "failed"
	}
	return "not_started"
}
