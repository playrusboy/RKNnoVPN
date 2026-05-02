package control

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/updater"
)

type UpdateInstallOperationRunner func(func(generation int64) error) (runtimev2.Status, error)

type UpdateHandlers struct {
	Version                       string
	DataDir                       string
	RuntimeWasRunning             func() bool
	RuntimeStatus                 RuntimeStatusFunc
	RunUpdateInstallOperation     UpdateInstallOperationRunner
	SetOperationStep              func(generation int64, name, status, code, detail string)
	StopRuntimeForModuleInstall   func() error
	RestoreRuntimeAfterModuleFail func()
	Logf                          func(format string, args ...interface{})
}

func (h UpdateHandlers) UpdateCheck(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	return h.UpdateCheckContext(context.Background(), params)
}

func (h UpdateHandlers) UpdateCheckContext(ctx context.Context, params *json.RawMessage) (interface{}, *ipc.RPCError) {
	info, err := updater.CheckForUpdateAndPersistWithContext(ctx, h.DataDir, h.currentUpdateVersion(params))
	if err != nil {
		return nil, &ipc.RPCError{
			Code:    ipc.CodeInternalError,
			Message: "update check failed: " + err.Error(),
		}
	}
	return info, nil
}

func (h UpdateHandlers) UpdateDownload(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	return h.UpdateDownloadContext(context.Background(), params)
}

func (h UpdateHandlers) UpdateDownloadContext(ctx context.Context, params *json.RawMessage) (interface{}, *ipc.RPCError) {
	if rpcErr := h.failIfRuntimeOperationActive(); rpcErr != nil {
		return nil, rpcErr
	}
	downloaded, err := updater.RunDownloadTransaction(updater.DownloadTransaction{
		Context:        ctx,
		CurrentVersion: h.currentUpdateVersion(params),
		DataDir:        h.DataDir,
		Logf:           h.logf,
	})
	if err != nil {
		if errors.Is(err, updater.ErrNoUpdateAvailable) {
			return nil, &ipc.RPCError{
				Code:    ipc.CodeInvalidParams,
				Message: err.Error(),
				Data:    updateDownloadErrorData("UPDATE_NOT_AVAILABLE", err.Error()),
			}
		}
		return nil, &ipc.RPCError{
			Code:    ipc.CodeInternalError,
			Message: "download failed: " + err.Error(),
			Data:    updateDownloadErrorData("UPDATE_DOWNLOAD_FAILED", err.Error()),
		}
	}
	return updateDownloadResult(downloaded), nil
}

func (h UpdateHandlers) UpdateInstall(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	return h.UpdateInstallContext(context.Background(), params)
}

func (h UpdateHandlers) UpdateInstallContext(ctx context.Context, params *json.RawMessage) (interface{}, *ipc.RPCError) {
	if rpcErr := rpcErrorFromContext(ctx); rpcErr != nil {
		return nil, rpcErr
	}
	artifacts, err := updater.ResolveVerifiedInstallArtifacts(h.DataDir, params)
	if err != nil {
		return nil, &ipc.RPCError{
			Code:    ipc.CodeInvalidParams,
			Message: err.Error(),
		}
	}
	if rpcErr := rpcErrorFromContext(ctx); rpcErr != nil {
		return nil, rpcErr
	}
	if h.RunUpdateInstallOperation == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "update install operation runner is not configured"}
	}

	wasRunning := false
	if h.RuntimeWasRunning != nil {
		wasRunning = h.RuntimeWasRunning()
	}
	status, err := h.RunUpdateInstallOperation(func(generation int64) error {
		return updater.RunInstallTransaction(updater.InstallTransaction{
			Context:           ctx,
			DataDir:           h.DataDir,
			Generation:        generation,
			Artifacts:         artifacts,
			WasRuntimeRunning: wasRunning,
			Hooks: updater.InstallHooks{
				SetOperationStep: func(name, status, code, detail string) {
					if h.SetOperationStep != nil {
						h.SetOperationStep(generation, name, status, code, detail)
					}
				},
				StopRuntimeForModuleInstall:   h.StopRuntimeForModuleInstall,
				RestoreRuntimeAfterModuleFail: h.RestoreRuntimeAfterModuleFail,
				ScheduleSelfExit: func() {
					go updater.ScheduleSelfExit(updater.SelfExitDelay)
				},
				Logf: h.logf,
			},
		})
	})
	if err != nil {
		return nil, RuntimeRPCError(err)
	}
	return status, nil
}

func (h UpdateHandlers) failIfRuntimeOperationActive() *ipc.RPCError {
	if h.RuntimeStatus == nil {
		return nil
	}
	status, ok := h.RuntimeStatus()
	if !ok || status.ActiveOperation == nil {
		return nil
	}
	return RuntimeRPCError(runtimev2.NewRuntimeBusyError(*status.ActiveOperation))
}

func (h UpdateHandlers) logf(format string, args ...interface{}) {
	if h.Logf != nil {
		h.Logf(format, args...)
		return
	}
	log.Printf("[updater] "+format, args...)
}

func (h UpdateHandlers) currentUpdateVersion(_ *json.RawMessage) string {
	return updater.NormalizeVersionTag(h.Version)
}

func updateDownloadResult(downloaded *updater.DownloadedUpdate) map[string]interface{} {
	result := map[string]interface{}{
		"operation": updateDownloadOperation("downloaded", "", ""),
	}
	if downloaded == nil {
		return result
	}
	result["module_path"] = downloaded.ModulePath
	result["apk_path"] = downloaded.ApkPath
	result["manifest_path"] = downloaded.ManifestPath
	result["checksums"] = downloaded.Checksums
	return result
}

func updateDownloadErrorData(code string, message string) map[string]interface{} {
	return map[string]interface{}{
		"operation": updateDownloadOperation("failed", code, message),
	}
}
