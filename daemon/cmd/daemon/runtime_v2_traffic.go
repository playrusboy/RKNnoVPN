package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

const runtimeTrafficSampleInterval = 3 * time.Second

func (d *daemon) currentRuntimeTrafficStats(status runtimev2.Status) runtimev2.TrafficStats {
	if status.AppliedState.Phase == runtimev2.PhaseStopped {
		d.metricsMu.Lock()
		d.traffic = trafficSnapshot{}
		d.metricsMu.Unlock()
		return runtimev2.TrafficStats{}
	}

	d.metricsMu.Lock()
	defer d.metricsMu.Unlock()
	return d.traffic.stats
}

func (d *daemon) startTrafficSampler() {
	d.metricsMu.Lock()
	if d.trafficSamplerStop != nil {
		d.metricsMu.Unlock()
		return
	}
	stop := make(chan struct{})
	d.trafficSamplerStop = stop
	d.metricsMu.Unlock()

	go d.runTrafficSampler(stop)
}

func (d *daemon) stopTrafficSampler() {
	d.metricsMu.Lock()
	stop := d.trafficSamplerStop
	d.trafficSamplerStop = nil
	d.traffic = trafficSnapshot{}
	d.metricsMu.Unlock()
	if stop != nil {
		close(stop)
	}
}

func (d *daemon) runTrafficSampler(stop <-chan struct{}) {
	d.sampleRuntimeTraffic()
	ticker := time.NewTicker(runtimeTrafficSampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.sampleRuntimeTraffic()
		case <-stop:
			return
		}
	}
}

func (d *daemon) sampleRuntimeTraffic() {
	if !d.isRuntimeRunningOrDegraded() {
		d.metricsMu.Lock()
		d.traffic = trafficSnapshot{}
		d.metricsMu.Unlock()
		return
	}

	txBytes, rxBytes, ok := readRuntimeTrafficCounters()
	if !ok {
		return
	}

	stats := runtimev2.TrafficStats{
		TXBytes: txBytes,
		RXBytes: rxBytes,
	}
	now := time.Now()
	d.metricsMu.Lock()
	defer d.metricsMu.Unlock()
	if !d.traffic.checkedAt.IsZero() && now.After(d.traffic.checkedAt) {
		elapsed := now.Sub(d.traffic.checkedAt).Seconds()
		if elapsed > 0 {
			stats.TXRate = ratePerSecond(txBytes-d.traffic.stats.TXBytes, elapsed)
			stats.RXRate = ratePerSecond(rxBytes-d.traffic.stats.RXBytes, elapsed)
		}
	}
	d.traffic = trafficSnapshot{
		stats:     stats,
		checkedAt: now,
	}
}

func ratePerSecond(delta int64, elapsed float64) int64 {
	if delta <= 0 || elapsed <= 0 {
		return 0
	}
	return int64(float64(delta) / elapsed)
}

func readRuntimeTrafficCounters() (txBytes int64, rxBytes int64, ok bool) {
	for _, binary := range []string{"iptables", "ip6tables"} {
		tx, divert, pre, saveOK := iptablesSaveTrafficBytes(binary)
		if saveOK {
			txBytes += tx
			rxBytes += divert
			if divert == 0 {
				rxBytes += pre
			}
			continue
		}

		txBytes += iptablesRuleBytes(binary, "RKNNOVPN_APP", "MARK")
		divert = iptablesRuleBytes(binary, "RKNNOVPN_DIVERT", "ACCEPT")
		rxBytes += divert
		if divert == 0 {
			rxBytes += iptablesRuleBytes(binary, "RKNNOVPN_PRE", "RKNNOVPN_DIVERT")
		}
	}

	return txBytes, rxBytes, txBytes > 0 || rxBytes > 0
}

func iptablesSaveTrafficBytes(binary string) (txBytes int64, divertBytes int64, preDivertBytes int64, ok bool) {
	out, err := core.ExecCommand(binary+"-save", "-t", "mangle", "-c")
	if err != nil {
		return 0, 0, 0, false
	}
	for _, line := range strings.Split(out, "\n") {
		bytes, chain, target, parsed := parseIptablesSaveCounter(line)
		if !parsed {
			continue
		}
		switch {
		case chain == "RKNNOVPN_APP" && target == "MARK":
			txBytes += bytes
		case chain == "RKNNOVPN_DIVERT" && target == "ACCEPT":
			divertBytes += bytes
		case chain == "RKNNOVPN_PRE" && target == "RKNNOVPN_DIVERT":
			preDivertBytes += bytes
		}
	}
	return txBytes, divertBytes, preDivertBytes, true
}

func parseIptablesSaveCounter(line string) (int64, string, string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[") {
		return 0, "", "", false
	}
	end := strings.Index(line, "]")
	if end <= 1 {
		return 0, "", "", false
	}
	counterParts := strings.SplitN(line[1:end], ":", 2)
	if len(counterParts) != 2 {
		return 0, "", "", false
	}
	bytes, err := strconv.ParseInt(counterParts[1], 10, 64)
	if err != nil || bytes <= 0 {
		return 0, "", "", false
	}
	fields := strings.Fields(strings.TrimSpace(line[end+1:]))
	if len(fields) < 4 || fields[0] != "-A" {
		return 0, "", "", false
	}
	target := ""
	for i := 2; i < len(fields)-1; i++ {
		if fields[i] == "-j" {
			target = fields[i+1]
			break
		}
	}
	if target == "" {
		return 0, "", "", false
	}
	return bytes, fields[1], target, true
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
