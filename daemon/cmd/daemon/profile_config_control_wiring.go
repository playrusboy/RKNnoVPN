package main

import (
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/control"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/health"
	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
)

func (d *daemon) auditControlHandlers() control.AuditHandlers {
	return control.AuditHandlers{
		ConfigPath: d.cfgPath,
		DataDir:    d.dataDir,
		CurrentConfig: func() *config.Config {
			d.mu.Lock()
			defer d.mu.Unlock()
			return d.cfg
		},
		RunHealth: func() *health.HealthResult {
			return d.healthMon.RunOnce()
		},
		CoreState: func() string {
			return d.coreMgr.Status().State
		},
		Now: time.Now,
	}
}

func (d *daemon) configControlHandlers() control.ConfigHandlers {
	return control.ConfigHandlers{
		CurrentConfig: func() *config.Config {
			d.mu.Lock()
			defer d.mu.Unlock()
			return d.cfg
		},
		PersistConfigMutation: d.persistProfileConfigMutationForAction,
		RuntimeStatus:         d.controlRuntimeStatus,
	}
}

func (d *daemon) profileControlHandlers() control.ProfileHandlers {
	return control.ProfileHandlers{
		CurrentProfile: func() profiledoc.Document {
			d.mu.Lock()
			defer d.mu.Unlock()
			return profiledoc.FromConfig(d.cfg)
		},
		ApplyProfile: d.applyProfileDocument,
		Now:          time.Now,
	}
}
