package main

import (
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/control"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
)

func (d *daemon) registerHandlers() error {
	appHandlers := control.AppHandlers{}
	auditHandlers := d.auditControlHandlers()
	logHandlers := control.LogHandlers{DataDir: d.dataDir}
	metaHandlers := control.MetaHandlers{
		Version: Version,
		DataDir: d.dataDir,
		Exec:    core.ExecCommand,
	}
	configHandlers := d.configControlHandlers()
	diagnosticsHandlers := d.diagnosticsControlHandlers()
	profileHandlers := d.profileControlHandlers()
	runtimeHandlers := d.runtimeControlHandlers()
	updateHandlers := d.updateControlHandlers()
	return control.RegisterContractHandlers(d.ipcServer, map[string]ipc.Handler{
		"app.list":                  appHandlers.AppList,
		"app.resolveUid":            appHandlers.ResolveUID,
		"audit":                     auditHandlers.Audit,
		"backend.applyDesiredState": runtimeHandlers.BackendApplyDesiredState,
		"backend.reset":             runtimeHandlers.BackendReset,
		"backend.restart":           runtimeHandlers.BackendRestart,
		"backend.start":             runtimeHandlers.BackendStart,
		"backend.status":            runtimeHandlers.BackendStatus,
		"backend.stop":              runtimeHandlers.BackendStop,
		"config-import":             configHandlers.ConfigImport,
		"config-list":               configHandlers.ConfigList,
		"diagnostics.health":        runtimeHandlers.DiagnosticsHealth,
		"diagnostics.testNodes":     runtimeHandlers.DiagnosticsTestNodes,
		"diagnostics.report":        diagnosticsHandlers.DiagnosticsReport,
		"ipc.contract":              metaHandlers.IPCContract,
		"logs":                      logHandlers.Logs,
		"profile.apply":             profileHandlers.ProfileApply,
		"profile.get":               profileHandlers.ProfileGet,
		"profile.importNodes":       profileHandlers.ProfileImportNodes,
		"profile.setActiveNode":     profileHandlers.ProfileSetActiveNode,
		"self-check":                diagnosticsHandlers.SelfCheck,
		"subscription.preview":      profileHandlers.SubscriptionPreview,
		"subscription.refresh":      profileHandlers.SubscriptionRefresh,
		"update-check":              updateHandlers.UpdateCheck,
		"update-download":           updateHandlers.UpdateDownload,
		"update-install":            updateHandlers.UpdateInstall,
		"version":                   metaHandlers.VersionInfo,
	})
}
