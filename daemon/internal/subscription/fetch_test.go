package subscription

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
)

func TestRefreshResultResponseExposesStableRPCFields(t *testing.T) {
	result := RefreshResult{
		Source: SubscriptionSource{
			ProviderKey: "provider",
			URL:         "https://example.com/sub",
		},
		Subscription: profiledoc.Subscription{
			ProviderKey:   "provider",
			URL:           "https://example.com/sub",
			LastFetchedAt: 123,
		},
		Nodes:         []profiledoc.Node{{ID: "node-1"}, {ID: "node-2"}},
		RejectedNodes: []RejectedNode{{Server: "127.0.0.1", Port: 10808, Code: "subscription_local_endpoint"}},
		ParseFailures: 3,
		Merge:         map[string]int{"added": 2},
	}

	response := result.Response()
	if response.Imported != 2 || response.ParseFailures != 3 || response.Rejected != 1 {
		t.Fatalf("unexpected response counts: %#v", response)
	}
	if len(response.RejectedNodes) != 1 || response.RejectedNodes[0].Server != "127.0.0.1" {
		t.Fatalf("unexpected rejected node metadata: %#v", response.RejectedNodes)
	}
	if response.Source.ProviderKey != "provider" || response.Subscription.LastFetchedAt != 123 {
		t.Fatalf("unexpected response metadata: %#v", response)
	}
	if response.Merge["added"] != 2 {
		t.Fatalf("unexpected merge stats: %#v", response.Merge)
	}
}

func TestPreviewReportsRejectedSubscriptionEndpoints(t *testing.T) {
	client := NewClient(FetcherFunc(func(rawURL string) (FetchResult, error) {
		return FetchResult{
			Status: 200,
			Body: strings.Join([]string{
				"vless://00000000-0000-0000-0000-000000000000@example.com:443#public",
				"vless://00000000-0000-0000-0000-000000000000@127.0.0.1:10808#local",
			}, "\n"),
		}, nil
	}))

	preview, err := client.Preview("https://example.com/sub", profiledoc.Document{})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Nodes) != 1 || preview.Rejected != 1 {
		t.Fatalf("unexpected preview result: %#v", preview)
	}
	if preview.RejectedNodes[0].Code != "subscription_local_endpoint" {
		t.Fatalf("unexpected rejection metadata: %#v", preview.RejectedNodes)
	}
}

func TestPreviewKeepsRejectedMetadataWhenOnlyRejectedAndParseFailures(t *testing.T) {
	client := NewClient(FetcherFunc(func(rawURL string) (FetchResult, error) {
		return FetchResult{
			Status: 200,
			Body: strings.Join([]string{
				"not-a-link",
				"vless://00000000-0000-0000-0000-000000000000@127.0.0.1:10808#local",
			}, "\n"),
		}, nil
	}))

	preview, err := client.Preview("https://example.com/sub", profiledoc.Document{})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Nodes) != 0 || preview.ParseFailures != 1 || preview.Rejected != 1 {
		t.Fatalf("preview should preserve both parse and rejected counts: %#v", preview)
	}
	if len(preview.RejectedNodes) != 1 || preview.RejectedNodes[0].Server != "127.0.0.1" {
		t.Fatalf("preview lost rejected endpoint metadata: %#v", preview.RejectedNodes)
	}
}

func TestApplyRefreshRejectsAllRejectedSubscriptionEndpoints(t *testing.T) {
	client := NewClient(FetcherFunc(func(rawURL string) (FetchResult, error) {
		return FetchResult{
			Status: 200,
			Body:   "vless://00000000-0000-0000-0000-000000000000@127.0.0.1:10808#local",
		}, nil
	}))

	result, err := client.ApplyRefresh("https://example.com/sub", profiledoc.Document{})
	if !errors.Is(err, ErrNoSupportedNodes) {
		t.Fatalf("expected no supported nodes error, got result=%#v err=%v", result, err)
	}
	if len(result.RejectedNodes) != 1 {
		t.Fatalf("expected rejected endpoint in refresh result: %#v", result)
	}
}

func TestPreviewContextHonorsCancelledContext(t *testing.T) {
	var fetchCalls int
	client := NewClient(FetcherFunc(func(rawURL string) (FetchResult, error) {
		fetchCalls++
		return FetchResult{Status: 200}, nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.PreviewContext(ctx, "https://example.com/sub", profiledoc.Document{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if fetchCalls != 0 {
		t.Fatalf("cancelled context should not call fetcher, got %d calls", fetchCalls)
	}
}

func TestBootstrapResolverIgnoresSystemLoopbackDNSServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow listening sockets: %v", err)
		}
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
		close(done)
	}()

	previous := fetchBootstrapDNSServers
	fetchBootstrapDNSServers = []string{listener.Addr().String()}
	t.Cleanup(func() { fetchBootstrapDNSServers = previous })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := newBootstrapResolver().Dial(ctx, "tcp", "[::1]:53")
	if err != nil {
		t.Fatalf("expected resolver to use bootstrap DNS instead of passed loopback address: %v", err)
	}
	_ = conn.Close()
	<-done
}

func TestFetchLookupUsesSystemResolverFirst(t *testing.T) {
	previousSystem := systemLookupIPAddr
	previousBootstrap := bootstrapLookupIPAddr
	systemLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
	}
	bootstrapLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		t.Fatal("bootstrap resolver should not be used when system resolver succeeds")
		return nil, nil
	}
	t.Cleanup(func() {
		systemLookupIPAddr = previousSystem
		bootstrapLookupIPAddr = previousBootstrap
	})

	ips, err := lookupFetchHost(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || !ips[0].IP.Equal(net.ParseIP("203.0.113.10")) {
		t.Fatalf("unexpected resolved IPs: %#v", ips)
	}
}

func TestFetchLookupFallsBackToBootstrapResolver(t *testing.T) {
	previousSystem := systemLookupIPAddr
	previousBootstrap := bootstrapLookupIPAddr
	systemLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return nil, errors.New("system resolver unavailable")
	}
	bootstrapLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.20")}}, nil
	}
	t.Cleanup(func() {
		systemLookupIPAddr = previousSystem
		bootstrapLookupIPAddr = previousBootstrap
	})

	ips, err := lookupFetchHost(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || !ips[0].IP.Equal(net.ParseIP("203.0.113.20")) {
		t.Fatalf("unexpected resolved IPs: %#v", ips)
	}
}

func TestFetchDialRejectsSystemResolvedPrivateIP(t *testing.T) {
	previousSystem := systemLookupIPAddr
	previousBootstrap := bootstrapLookupIPAddr
	systemLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	bootstrapLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		t.Fatal("bootstrap resolver should not be used when system resolver succeeds")
		return nil, nil
	}
	t.Cleanup(func() {
		systemLookupIPAddr = previousSystem
		bootstrapLookupIPAddr = previousBootstrap
	})

	_, err := fetchDialContext(context.Background(), "tcp", "example.com:443")
	if err == nil || !strings.Contains(err.Error(), "local, private, or reserved") {
		t.Fatalf("expected private resolved IP rejection, got %v", err)
	}
}

func TestClassifyError(t *testing.T) {
	if got := ClassifyError("", errors.New("missing")); got != ErrorInvalidParams {
		t.Fatalf("empty URL should be invalid params, got %s", got)
	}
	if got := ClassifyError("ftp://example.com/sub", errors.New("bad scheme")); got != ErrorInvalidParams {
		t.Fatalf("bad URL should be invalid params, got %s", got)
	}
	if got := ClassifyError("https://example.com/sub", ErrNoSupportedNodes); got != ErrorConfig {
		t.Fatalf("unsupported nodes should be config error, got %s", got)
	}
	if got := ClassifyError("https://example.com/sub", errors.New("network down")); got != ErrorInternal {
		t.Fatalf("fetch failure should be internal error, got %s", got)
	}
}
