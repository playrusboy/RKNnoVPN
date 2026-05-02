package main

import (
	"context"

	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) testNodeProbesV2(url string, timeoutMS int, nodeIDs []string) []runtimev2.NodeProbeResult {
	return d.testNodeProbesV2Context(context.Background(), url, timeoutMS, nodeIDs)
}

func (d *daemon) testNodeProbesV2Context(ctx context.Context, url string, timeoutMS int, nodeIDs []string) []runtimev2.NodeProbeResult {
	cfg := d.currentConfig()
	state := d.coreMgr.GetState()
	var runtimeHealth runtimev2.HealthSnapshot
	if coreStateRunningOrDegraded(state) {
		runtimeHealth = d.buildRuntimeV2HealthSnapshot(d.healthMon.RunOnce(), false)
	}

	return rootruntime.RunNodeProbes(rootruntime.NodeProbeInput{
		Context:       ctx,
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
