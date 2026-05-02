package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestClashDelaySendsBearerSecret(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/proxies/proxy/delay" {
			t.Fatalf("path = %q, want /proxies/proxy/delay", r.URL.Path)
		}
		if r.URL.Query().Get("url") != "https://probe.example/generate_204" {
			t.Fatalf("url query = %q", r.URL.Query().Get("url"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"delay":123}`))
	}))
	defer server.Close()

	_, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split server address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}

	delay, status, err := testClashDelay(port, "test-secret", "proxy", "https://probe.example/generate_204", 2500)
	if err != nil {
		t.Fatalf("testClashDelay failed: %v", err)
	}
	if status != http.StatusOK || delay != 123 {
		t.Fatalf("status/delay = %d/%d, want 200/123", status, delay)
	}
	if gotAuth != "Bearer test-secret" {
		t.Fatalf("Authorization = %q, want bearer secret", gotAuth)
	}
}

func TestPreferIPv4OrdersIPv4BeforeIPv6(t *testing.T) {
	ordered := preferIPv4([]net.IPAddr{
		{IP: net.ParseIP("2606:4700::6810:84e5")},
		{IP: net.ParseIP("104.16.132.229")},
		{IP: net.ParseIP("2a00:1450:4001:c1f::5e")},
		{IP: net.ParseIP("142.251.20.94")},
	})

	if len(ordered) != 4 {
		t.Fatalf("ordered len = %d, want 4", len(ordered))
	}
	if ordered[0].IP.To4() == nil || ordered[1].IP.To4() == nil {
		t.Fatalf("first two addresses should be IPv4, got %#v", ordered)
	}
	if ordered[2].IP.To4() != nil || ordered[3].IP.To4() != nil {
		t.Fatalf("last two addresses should be IPv6, got %#v", ordered)
	}
}
