package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) currentRuntimeTrafficStats(status runtimev2.Status) runtimev2.TrafficStats {
	if status.AppliedState.Phase == runtimev2.PhaseStopped {
		d.metricsMu.Lock()
		d.traffic = trafficSnapshot{}
		d.metricsMu.Unlock()
		return runtimev2.TrafficStats{}
	}

	txBytes, rxBytes, ok := readRuntimeTrafficCounters()
	if !ok {
		return runtimev2.TrafficStats{}
	}

	now := time.Now()
	d.metricsMu.Lock()
	defer d.metricsMu.Unlock()

	stats := runtimev2.TrafficStats{
		TXBytes: txBytes,
		RXBytes: rxBytes,
	}
	if !d.traffic.checkedAt.IsZero() && now.After(d.traffic.checkedAt) {
		elapsed := now.Sub(d.traffic.checkedAt).Seconds()
		if elapsed > 0 {
			stats.TXRate = ratePerSecond(txBytes-d.traffic.txBytes, elapsed)
			stats.RXRate = ratePerSecond(rxBytes-d.traffic.rxBytes, elapsed)
		}
	}
	d.traffic = trafficSnapshot{
		txBytes:   txBytes,
		rxBytes:   rxBytes,
		checkedAt: now,
	}
	return stats
}

func ratePerSecond(delta int64, elapsed float64) int64 {
	if delta <= 0 || elapsed <= 0 {
		return 0
	}
	return int64(float64(delta) / elapsed)
}

func readRuntimeTrafficCounters() (txBytes int64, rxBytes int64, ok bool) {
	txBytes += iptablesRuleBytes("iptables", "RKNNOVPN_APP", "MARK")
	txBytes += iptablesRuleBytes("ip6tables", "RKNNOVPN_APP", "MARK")

	rxBytes += iptablesRuleBytes("iptables", "RKNNOVPN_DIVERT", "ACCEPT")
	rxBytes += iptablesRuleBytes("ip6tables", "RKNNOVPN_DIVERT", "ACCEPT")
	if rxBytes == 0 {
		rxBytes += iptablesRuleBytes("iptables", "RKNNOVPN_PRE", "RKNNOVPN_DIVERT")
		rxBytes += iptablesRuleBytes("ip6tables", "RKNNOVPN_PRE", "RKNNOVPN_DIVERT")
	}

	return txBytes, rxBytes, txBytes > 0 || rxBytes > 0
}

func iptablesRuleBytes(binary, chain, target string) int64 {
	out, err := core.ExecCommand(binary, "-t", "mangle", "-L", chain, "-v", "-x", "-n")
	if err != nil {
		return 0
	}
	var total int64
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] != target {
			continue
		}
		bytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err == nil && bytes > 0 {
			total += bytes
		}
	}
	return total
}
