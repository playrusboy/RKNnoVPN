package main

import (
	"net"
	"testing"
)

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
