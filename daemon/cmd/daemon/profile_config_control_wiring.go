package main

import (
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/control"
)

func (d *daemon) auditControlHandlers() control.AuditHandlers {
	return control.AuditHandlers{
		ConfigPath:    d.cfgPath,
		DataDir:       d.dataDir,
		CurrentConfig: d.currentConfig,
		RunHealth:     d.healthMon.RunOnce,
		CoreState: func() string {
			return d.coreMgr.Status().State
		},
		Now: time.Now,
	}
}

func (d *daemon) configControlHandlers() control.ConfigHandlers {
	return control.ConfigHandlers{
		CurrentConfig:         d.currentConfig,
		PersistConfigMutation: d.persistConfigMutationForAction,
		RuntimeStatus:         d.controlRuntimeStatus,
	}
}

func (d *daemon) profileControlHandlers() control.ProfileHandlers {
	return control.ProfileHandlers{
		CurrentConfig:         d.currentConfig,
		PersistConfigMutation: d.persistConfigMutationForAction,
		RuntimeStatus:         d.controlRuntimeStatus,
		Now:                   time.Now,
	}
}
