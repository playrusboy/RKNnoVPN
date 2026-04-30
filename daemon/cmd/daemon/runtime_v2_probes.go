package main

import (
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) testNodeProbesV2(url string, timeoutMS int, nodeIDs []string) []runtimev2.NodeProbeResult {
	cfg := d.currentConfig()
	state := d.coreMgr.GetState()
	var runtimeHealth runtimev2.HealthSnapshot
	if coreStateRunningOrDegraded(state) {
		runtimeHealth = d.buildRuntimeV2HealthSnapshot(d.healthMon.RunOnce(), false)
	}

	return rootruntime.RunNodeProbes(rootruntime.NodeProbeInput{
		Config:        cfg,
		State:         state,
		RuntimeHealth: runtimeHealth,
		URL:           url,
		TimeoutMS:     timeoutMS,
		NodeIDs:       nodeIDs,
		APIPort:       cfg.Proxy.APIPort,
		IO:            rootProbeIO{d: d},
	})
}
