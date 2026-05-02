package main

import (
	"testing"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func TestParseIptablesSaveCounter(t *testing.T) {
	line := `[12:3456] -A RKNNOVPN_APP -m owner --uid-owner 1000 -j MARK --set-xmark 0x1/0x1`

	bytes, chain, target, ok := parseIptablesSaveCounter(line)
	if !ok {
		t.Fatal("expected iptables-save rule to parse")
	}
	if bytes != 3456 || chain != "RKNNOVPN_APP" || target != "MARK" {
		t.Fatalf("unexpected parsed counter: bytes=%d chain=%q target=%q", bytes, chain, target)
	}
}

func TestCurrentRuntimeTrafficStatsReturnsCachedSnapshot(t *testing.T) {
	d := &daemon{}
	want := runtimev2.TrafficStats{TXBytes: 10, RXBytes: 20, TXRate: 3, RXRate: 4}
	d.traffic = trafficSnapshot{stats: want, checkedAt: time.Now()}

	got := d.currentRuntimeTrafficStats(runtimev2.Status{
		AppliedState: runtimev2.AppliedState{Phase: runtimev2.PhaseRunning},
	})
	if got != want {
		t.Fatalf("traffic stats = %#v, want %#v", got, want)
	}
}

func TestCurrentRuntimeTrafficStatsClearsStoppedSnapshot(t *testing.T) {
	d := &daemon{}
	d.traffic = trafficSnapshot{
		stats:     runtimev2.TrafficStats{TXBytes: 10, RXBytes: 20},
		checkedAt: time.Now(),
	}

	got := d.currentRuntimeTrafficStats(runtimev2.Status{
		AppliedState: runtimev2.AppliedState{Phase: runtimev2.PhaseStopped},
	})
	if got != (runtimev2.TrafficStats{}) {
		t.Fatalf("stopped traffic stats = %#v, want zero", got)
	}
	if d.traffic != (trafficSnapshot{}) {
		t.Fatalf("stopped traffic snapshot was not cleared: %#v", d.traffic)
	}
}
