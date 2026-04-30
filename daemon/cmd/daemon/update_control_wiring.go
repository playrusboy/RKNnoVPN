package main

import (
	"log"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/control"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) updateControlHandlers() control.UpdateHandlers {
	return control.UpdateHandlers{
		Version:           Version,
		DataDir:           d.dataDir,
		RuntimeStatus:     d.controlRuntimeStatus,
		RuntimeWasRunning: d.isRuntimeRunningOrDegraded,
		RunUpdateInstallOperation: func(fn func(generation int64) error) (runtimev2.Status, error) {
			return d.runtimeV2.RunOperation(runtimev2.OperationUpdateInstall, runtimev2.PhaseStopping, fn)
		},
		SetOperationStep: func(generation int64, name, status, code, detail string) {
			d.runtimeV2.SetActiveOperationStep(generation, name, status, code, detail)
		},
		StopRuntimeForModuleInstall: func() error {
			if err := d.failIfResetInProgress(); err != nil {
				return err
			}
			d.stopSubsystems()
			if err := d.coreMgr.Stop(); err != nil {
				log.Printf("[updater] warning: failed to stop core: %v", err)
			}
			return nil
		},
		RestoreRuntimeAfterModuleFail: d.restoreCurrentRuntimeAfterFailedUpdate,
		RuntimeError:                  control.RuntimeRPCError,
		Logf: func(format string, args ...interface{}) {
			log.Printf("[updater] "+format, args...)
		},
	}
}

func (d *daemon) restoreCurrentRuntimeAfterFailedUpdate() {
	if err := d.failIfResetInProgress(); err != nil {
		log.Printf("[updater] skipping runtime restore while reset is active: %v", err)
		return
	}

	cfg := d.currentConfig()
	if cfg == nil {
		return
	}
	profile := cfg.ResolveProfile()
	if profile.Address == "" {
		return
	}
	if err := d.coreMgr.Start(profile); err != nil {
		log.Printf("[updater] warning: failed to restore previous runtime after failed update: %v", err)
		return
	}
	d.rescueMgr.Reset()
	d.startSubsystems()
}
