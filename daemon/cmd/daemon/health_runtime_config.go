package main

import (
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/health"
)

type healthRuntimeConfig struct {
	Interval           time.Duration
	Threshold          int
	TProxyPort         int
	DNSPort            int
	RouteMark          int
	CheckURL           string
	DNSProbeDomains    []string
	DNSIsHardReadiness bool
	Timeout            time.Duration
}

func newHealthRuntimeConfig(cfg *config.Config) healthRuntimeConfig {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	interval := time.Duration(cfg.Health.IntervalSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	threshold := cfg.Health.Threshold
	if threshold < 1 {
		threshold = 3
	}
	tproxyPort := cfg.Proxy.TProxyPort
	if tproxyPort == 0 {
		tproxyPort = 10853
	}
	dnsPort := cfg.Proxy.DNSPort
	if dnsPort == 0 {
		dnsPort = 10856
	}
	routeMark := cfg.Proxy.Mark
	if routeMark == 0 {
		routeMark = 0x2023
	}
	timeout := time.Duration(cfg.Health.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return healthRuntimeConfig{
		Interval:           interval,
		Threshold:          threshold,
		TProxyPort:         tproxyPort,
		DNSPort:            dnsPort,
		RouteMark:          routeMark,
		CheckURL:           cfg.Health.URL,
		DNSProbeDomains:    append([]string(nil), cfg.Health.DNSProbeDomains...),
		DNSIsHardReadiness: cfg.Health.DNSIsHardReadiness,
		Timeout:            timeout,
	}
}

func applyHealthRuntimeConfig(monitor *health.HealthMonitor, cfg healthRuntimeConfig) {
	if monitor == nil {
		return
	}
	monitor.SetConfig(
		cfg.Interval,
		cfg.Threshold,
		cfg.TProxyPort,
		cfg.DNSPort,
		cfg.RouteMark,
		cfg.CheckURL,
		cfg.DNSProbeDomains,
		cfg.DNSIsHardReadiness,
		cfg.Timeout,
	)
}
