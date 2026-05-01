package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/rootcerts"
)

func testTCPConnect(host string, port int, timeout time.Duration) (int64, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start).Milliseconds(), nil
}

func testClashDelay(apiPort int, outboundTag string, testURL string, timeoutMS int) (int64, int, error) {
	if apiPort <= 0 {
		return 0, 0, fmt.Errorf("api_disabled")
	}
	if outboundTag == "" {
		return 0, 0, fmt.Errorf("outbound tag is empty")
	}
	values := neturl.Values{}
	values.Set("timeout", strconv.Itoa(timeoutMS))
	values.Set("url", testURL)
	endpoint := fmt.Sprintf(
		"http://127.0.0.1:%d/proxies/%s/delay?%s",
		apiPort,
		neturl.PathEscape(outboundTag),
		values.Encode(),
	)
	client := &http.Client{Timeout: time.Duration(timeoutMS+1000) * time.Millisecond}
	resp, err := client.Get(endpoint)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, resp.StatusCode, fmt.Errorf("clash delay HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		Delay int64 `json:"delay"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, resp.StatusCode, fmt.Errorf("parse clash delay response: %w", err)
	}
	return parsed.Delay, resp.StatusCode, nil
}

type urlProbeMetrics struct {
	LatencyMS     int64
	StatusCode    int
	ResponseBytes int64
	ThroughputBps int64
}

func testTransparentURLProbe(cfg *config.Config, testURL string, timeoutMS int) (urlProbeMetrics, error) {
	var metrics urlProbeMetrics
	if cfg == nil {
		return metrics, fmt.Errorf("config is nil")
	}
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	parsed, err := neturl.Parse(strings.TrimSpace(testURL))
	if err != nil {
		return metrics, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return metrics, fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}

	timeout := time.Duration(timeoutMS) * time.Millisecond
	dnsPort := cfg.Proxy.DNSPort
	if dnsPort == 0 {
		dnsPort = 10856
	}
	mark := cfg.Proxy.Mark
	if mark == 0 {
		mark = 0x2023
	}

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: timeout}
			return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(dnsPort)))
		},
	}
	dialer := &net.Dialer{
		Timeout:  timeout,
		Resolver: resolver,
		Control: func(network, address string, conn syscall.RawConn) error {
			var sockErr error
			if err := conn.Control(func(fd uintptr) {
				sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark)
			}); err != nil {
				return err
			}
			return sockErr
		},
	}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         ipv4FirstMarkedDialContext(dialer, resolver, timeout),
		TLSHandshakeTimeout: timeout,
		DisableKeepAlives:   true,
	}
	if roots := rootcerts.AndroidSystemPool(); roots != nil {
		transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	}
	defer transport.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), timeout+time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return metrics, err
	}
	req.Header.Set("User-Agent", "RKNnoVPN/health")

	start := time.Now()
	resp, err := (&http.Client{Timeout: timeout + time.Second, Transport: transport}).Do(req)
	if err != nil {
		return metrics, err
	}
	defer resp.Body.Close()
	bytesRead, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024*1024))
	elapsed := time.Since(start)
	metrics.LatencyMS = elapsed.Milliseconds()
	metrics.StatusCode = resp.StatusCode
	metrics.ResponseBytes = bytesRead
	if bytesRead > 0 && elapsed > 0 {
		metrics.ThroughputBps = int64(float64(bytesRead) / elapsed.Seconds())
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return metrics, fmt.Errorf("transparent URL probe HTTP %d", resp.StatusCode)
	}
	return metrics, nil
}

func ipv4FirstMarkedDialContext(base *net.Dialer, resolver *net.Resolver, timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) != nil {
			return base.DialContext(ctx, network, address)
		}
		resolveCtx, cancel := context.WithTimeout(ctx, timeout)
		addrs, lookupErr := resolver.LookupIPAddr(resolveCtx, host)
		cancel()
		if lookupErr != nil || len(addrs) == 0 {
			return base.DialContext(ctx, network, address)
		}

		ordered := preferIPv4(addrs)
		errs := make([]error, 0, len(ordered))
		for _, addr := range ordered {
			dialCtx, dialCancel := context.WithTimeout(ctx, timeout)
			conn, dialErr := base.DialContext(dialCtx, network, net.JoinHostPort(addr.IP.String(), port))
			dialCancel()
			if dialErr == nil {
				return conn, nil
			}
			errs = append(errs, dialErr)
		}
		return nil, errors.Join(errs...)
	}
}

func preferIPv4(addrs []net.IPAddr) []net.IPAddr {
	ordered := make([]net.IPAddr, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IP.To4() != nil {
			ordered = append(ordered, addr)
		}
	}
	for _, addr := range addrs {
		if addr.IP.To4() == nil {
			ordered = append(ordered, addr)
		}
	}
	return ordered
}
