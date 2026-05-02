package subscription

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/rootcerts"
)

const maxBodyBytes = 4 * 1024 * 1024

var ErrNoSupportedNodes = errors.New("subscription contains no supported nodes")

var fetchBootstrapDNSServers = []string{
	"1.1.1.1:53",
	"8.8.8.8:53",
	"9.9.9.9:53",
}

var systemLookupIPAddr = net.DefaultResolver.LookupIPAddr

var bootstrapLookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return newBootstrapResolver().LookupIPAddr(ctx, host)
}

type ErrorKind string

const (
	ErrorInvalidParams ErrorKind = "invalid_params"
	ErrorConfig        ErrorKind = "config_error"
	ErrorInternal      ErrorKind = "internal_error"
)

type FetchResult struct {
	Status  int
	Body    string
	Headers map[string]string
}

type Fetcher interface {
	FetchURL(rawURL string) (FetchResult, error)
}

type ContextFetcher interface {
	FetchURLContext(ctx context.Context, rawURL string) (FetchResult, error)
}

type FetcherFunc func(rawURL string) (FetchResult, error)

func (f FetcherFunc) FetchURL(rawURL string) (FetchResult, error) {
	return f(rawURL)
}

func (f FetcherFunc) FetchURLContext(_ context.Context, rawURL string) (FetchResult, error) {
	return f(rawURL)
}

type ContextFetcherFunc func(ctx context.Context, rawURL string) (FetchResult, error)

func (f ContextFetcherFunc) FetchURL(rawURL string) (FetchResult, error) {
	return f(context.Background(), rawURL)
}

func (f ContextFetcherFunc) FetchURLContext(ctx context.Context, rawURL string) (FetchResult, error) {
	return f(ctx, rawURL)
}

type Client struct {
	Fetcher Fetcher
	Now     func() time.Time
}

func NewClient(fetcher Fetcher) Client {
	if fetcher == nil {
		fetcher = ContextFetcherFunc(FetchURLContext)
	}
	return Client{Fetcher: fetcher, Now: time.Now}
}

func (c Client) Preview(rawURL string, current profiledoc.Document) (PreviewResult, error) {
	return c.PreviewContext(context.Background(), rawURL, current)
}

func (c Client) PreviewContext(ctx context.Context, rawURL string, current profiledoc.Document) (PreviewResult, error) {
	parsed, err := c.fetchAndParse(ctx, rawURL)
	if err != nil {
		return PreviewResult{
			Source:       parsed.Source,
			FetchStatus:  parsed.Fetch.Status,
			FetchHeaders: parsed.Fetch.Headers,
		}, err
	}
	_, stats := MergeSubscriptionNodes(current, parsed.Subscription, parsed.Nodes)
	return PreviewResult{
		Source:        parsed.Source,
		Subscription:  parsed.Subscription,
		Nodes:         parsed.Nodes,
		RejectedNodes: parsed.Rejected,
		Rejected:      len(parsed.Rejected),
		Added:         stats["added"],
		Updated:       stats["updated"],
		Unchanged:     stats["unchanged"],
		Stale:         stats["stale"],
		ParseFailures: parsed.ParseFailures,
		FetchStatus:   parsed.Fetch.Status,
		FetchHeaders:  parsed.Fetch.Headers,
	}, nil
}

func (c Client) ApplyRefresh(rawURL string, current profiledoc.Document) (RefreshResult, error) {
	return c.ApplyRefreshContext(context.Background(), rawURL, current)
}

func (c Client) ApplyRefreshContext(ctx context.Context, rawURL string, current profiledoc.Document) (RefreshResult, error) {
	parsed, err := c.fetchAndParse(ctx, rawURL)
	if err != nil {
		return RefreshResult{
			Source:       parsed.Source,
			FetchStatus:  parsed.Fetch.Status,
			FetchHeaders: parsed.Fetch.Headers,
		}, err
	}
	return ApplyPreview(current, PreviewResult{
		Source:        parsed.Source,
		Subscription:  parsed.Subscription,
		Nodes:         parsed.Nodes,
		RejectedNodes: parsed.Rejected,
		Rejected:      len(parsed.Rejected),
		ParseFailures: parsed.ParseFailures,
		FetchStatus:   parsed.Fetch.Status,
		FetchHeaders:  parsed.Fetch.Headers,
	})
}

func ApplyPreview(current profiledoc.Document, preview PreviewResult) (RefreshResult, error) {
	if len(preview.Nodes) == 0 && (preview.ParseFailures > 0 || len(preview.RejectedNodes) > 0) {
		return RefreshResult{
			Source:        preview.Source,
			Subscription:  preview.Subscription,
			RejectedNodes: preview.RejectedNodes,
			ParseFailures: preview.ParseFailures,
			FetchStatus:   preview.FetchStatus,
			FetchHeaders:  preview.FetchHeaders,
		}, ErrNoSupportedNodes
	}
	next, stats := MergeSubscriptionNodes(current, preview.Subscription, preview.Nodes)
	subscription := preview.Subscription
	replaced := false
	for i, existing := range next.Subscriptions {
		if existing.ProviderKey == subscription.ProviderKey {
			subscription.Name = existing.Name
			next.Subscriptions[i] = subscription
			replaced = true
			break
		}
	}
	if !replaced {
		next.Subscriptions = append(next.Subscriptions, subscription)
	}
	return RefreshResult{
		Source:        preview.Source,
		Profile:       next,
		Subscription:  subscription,
		Nodes:         append([]profiledoc.Node(nil), preview.Nodes...),
		RejectedNodes: append([]RejectedNode(nil), preview.RejectedNodes...),
		Merge:         stats,
		ParseFailures: preview.ParseFailures,
		FetchStatus:   preview.FetchStatus,
		FetchHeaders:  preview.FetchHeaders,
	}, nil
}

func (r RefreshResult) Response() RefreshResponse {
	return RefreshResponse{
		Source:        r.Source,
		Subscription:  r.Subscription,
		Imported:      len(r.Nodes),
		ParseFailures: r.ParseFailures,
		Rejected:      len(r.RejectedNodes),
		RejectedNodes: r.RejectedNodes,
		Merge:         r.Merge,
	}
}

type parsedFetch struct {
	Source        SubscriptionSource
	Nodes         []profiledoc.Node
	Subscription  profiledoc.Subscription
	ParseFailures int
	Rejected      []RejectedNode
	Fetch         FetchResult
}

func (c Client) fetchAndParse(ctx context.Context, rawURL string) (parsedFetch, error) {
	source, err := NewSubscriptionSource(rawURL)
	if err != nil {
		return parsedFetch{Source: source}, err
	}
	if c.Fetcher == nil {
		c.Fetcher = ContextFetcherFunc(FetchURLContext)
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	fetched, err := fetchWithContext(ctx, c.Fetcher, source.URL)
	if err != nil {
		return parsedFetch{Source: source, Fetch: fetched}, err
	}
	nodes, sub, failures, rejected := ParseSubscription(fetched.Body, fetched.Headers, source, now.UnixMilli())
	return parsedFetch{
		Source:        source,
		Nodes:         nodes,
		Subscription:  sub,
		ParseFailures: failures,
		Rejected:      rejected,
		Fetch:         fetched,
	}, nil
}

func fetchWithContext(ctx context.Context, fetcher Fetcher, rawURL string) (FetchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return FetchResult{}, err
	}
	if contextFetcher, ok := fetcher.(ContextFetcher); ok {
		return contextFetcher.FetchURLContext(ctx, rawURL)
	}
	result, err := fetcher.FetchURL(rawURL)
	if err != nil {
		return result, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	return result, nil
}

func FetchURL(rawURL string) (FetchResult, error) {
	return FetchURLContext(context.Background(), rawURL)
}

func FetchURLContext(ctx context.Context, rawURL string) (FetchResult, error) {
	var result FetchResult
	source, err := NewSubscriptionSource(rawURL)
	if err != nil {
		return result, err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return result, fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "RKNnoVPN-subscription/2.0")

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = fetchDialContext
	if roots := subscriptionRootCAPool(); roots != nil {
		transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("subscription fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return result, fmt.Errorf("subscription read failed: %w", err)
	}
	if len(body) > maxBodyBytes {
		return result, fmt.Errorf("subscription response is too large")
	}

	headers := make(map[string]string, len(resp.Header))
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FetchResult{Status: resp.StatusCode, Headers: headers}, fmt.Errorf("subscription fetch returned HTTP %d", resp.StatusCode)
	}

	return FetchResult{
		Status:  resp.StatusCode,
		Body:    string(body),
		Headers: headers,
	}, nil
}

func subscriptionRootCAPool() *x509.CertPool {
	return rootcerts.AndroidSystemPool()
}

func ValidateFetchURL(rawURL string) error {
	_, err := NewSubscriptionSource(rawURL)
	return err
}

func ClassifyError(rawURL string, err error) ErrorKind {
	if ValidateFetchURL(rawURL) != nil || rawURL == "" {
		return ErrorInvalidParams
	}
	if errors.Is(err, ErrNoSupportedNodes) {
		return ErrorConfig
	}
	return ErrorInternal
}

func fetchDialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		host = address
		port = ""
	}
	if isDisallowedSourceHost(host) {
		return nil, fmt.Errorf("subscription URL host is local, private, or reserved")
	}
	ips, err := lookupFetchHost(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, resolved := range ips {
		if isDisallowedSourceIP(resolved.IP) {
			return nil, fmt.Errorf("subscription URL resolved to local, private, or reserved address")
		}
	}
	var dialer net.Dialer
	var lastErr error
	for _, resolved := range ips {
		dialAddress := address
		if port != "" {
			dialAddress = net.JoinHostPort(resolved.IP.String(), port)
		}
		conn, err := dialer.DialContext(ctx, network, dialAddress)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("subscription URL host did not resolve")
}

func lookupFetchHost(ctx context.Context, host string) ([]net.IPAddr, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IPAddr{{IP: ip}}, nil
	}

	systemCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	ips, systemErr := systemLookupIPAddr(systemCtx, host)
	cancel()
	if systemErr == nil && len(ips) > 0 {
		return ips, nil
	}

	ips, bootstrapErr := bootstrapLookupIPAddr(ctx, host)
	if bootstrapErr == nil && len(ips) > 0 {
		return ips, nil
	}
	if systemErr != nil && bootstrapErr != nil {
		return nil, fmt.Errorf("system DNS lookup failed: %v; bootstrap DNS lookup failed: %w", systemErr, bootstrapErr)
	}
	if systemErr != nil {
		return nil, systemErr
	}
	if bootstrapErr != nil {
		return nil, bootstrapErr
	}
	return nil, fmt.Errorf("subscription URL host did not resolve")
}

func newBootstrapResolver() *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var lastErr error
			for _, server := range fetchBootstrapDNSServers {
				dialer := net.Dialer{Timeout: 5 * time.Second}
				conn, err := dialer.DialContext(ctx, network, server)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, errors.New("no bootstrap DNS servers configured")
		},
	}
}
