package main

import (
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/control"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
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
	groups := control.HandlerGroups{
		App:         appHandlers,
		Audit:       auditHandlers,
		Config:      configHandlers,
		Diagnostics: diagnosticsHandlers,
		Logs:        logHandlers,
		Meta:        metaHandlers,
		Profile:     profileHandlers,
		Runtime:     runtimeHandlers,
		Update:      updateHandlers,
	}
	return control.RegisterContractHandlers(d.ipcServer, groups.ContractHandlers())
}
