package main

import (
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/control"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) runtimeControlHandlers() control.RuntimeHandlers {
	return control.RuntimeHandlers{
		DataDir: d.dataDir,
		Initialized: func() bool {
			return d.runtimeV2 != nil
		},
		RefreshCompatibility:  d.maybeRefreshRuntimeV2Compatibility,
		RefreshActiveProgress: d.runtimeV2.RefreshActiveProgress,
		Status:                d.runtimeV2.Status,
		RuntimeStats:          d.currentRuntimeTrafficStats,
		IsRunningOrDegraded:   d.isRuntimeRunningOrDegraded,
		CurrentHealth:         d.runtimeV2.CurrentHealth,
		RefreshHealth:         d.runtimeV2.RefreshHealth,
		ApplyDesiredState:     d.applyDesiredStateV2,
		SyncDesiredState:      d.syncRuntimeV2DesiredState,
		Start:                 d.runtimeV2.Start,
		Stop:                  d.runtimeV2.Stop,
		Restart:               d.runtimeV2.Restart,
		Reset:                 d.runtimeV2.Reset,
	}
}

func (d *daemon) controlRuntimeStatus() (runtimev2.Status, bool) {
	if d.runtimeV2 == nil {
		return runtimev2.Status{}, false
	}
	return d.runtimeV2.Status(), true
}

func (d *daemon) isRuntimeRunningOrDegraded() bool {
	return coreStateRunningOrDegraded(d.coreMgr.GetState())
}

func coreStateRunningOrDegraded(state core.State) bool {
	return state == core.StateRunning || state == core.StateDegraded
}
