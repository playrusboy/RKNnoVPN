package subscription

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
)

const maxBodyBytes = 4 * 1024 * 1024

var ErrNoSupportedNodes = errors.New("subscription contains no supported nodes")

var fetchBootstrapDNSServers = []string{
	"1.1.1.1:53",
	"8.8.8.8:53",
	"9.9.9.9:53",
}

var androidRootCAPaths = []string{
	"/apex/com.android.conscrypt/cacerts",
	"/system/etc/security/cacerts",
	"/data/misc/keychain/certs-added",
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

type FetcherFunc func(rawURL string) (FetchResult, error)

func (f FetcherFunc) FetchURL(rawURL string) (FetchResult, error) {
	return f(rawURL)
}

type Client struct {
	Fetcher Fetcher
	Now     func() time.Time
}

func NewClient(fetcher Fetcher) Client {
	if fetcher == nil {
		fetcher = FetcherFunc(FetchURL)
	}
	return Client{Fetcher: fetcher, Now: time.Now}
}

func (c Client) Preview(rawURL string, current profiledoc.Document) (PreviewResult, error) {
	parsed, err := c.fetchAndParse(rawURL)
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
	parsed, err := c.fetchAndParse(rawURL)
	if err != nil {
		return RefreshResult{
			Source:       parsed.Source,
			FetchStatus:  parsed.Fetch.Status,
			FetchHeaders: parsed.Fetch.Headers,
		}, err
	}
	if len(parsed.Nodes) == 0 && (parsed.ParseFailures > 0 || len(parsed.Rejected) > 0) {
		return RefreshResult{
			Source:        parsed.Source,
			Subscription:  parsed.Subscription,
			RejectedNodes: parsed.Rejected,
			ParseFailures: parsed.ParseFailures,
			FetchStatus:   parsed.Fetch.Status,
			FetchHeaders:  parsed.Fetch.Headers,
		}, ErrNoSupportedNodes
	}
	next, stats := MergeSubscriptionNodes(current, parsed.Subscription, parsed.Nodes)
	subscription := parsed.Subscription
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
		Source:        parsed.Source,
		Profile:       next,
		Subscription:  subscription,
		Nodes:         parsed.Nodes,
		RejectedNodes: parsed.Rejected,
		Merge:         stats,
		ParseFailures: parsed.ParseFailures,
		FetchStatus:   parsed.Fetch.Status,
		FetchHeaders:  parsed.Fetch.Headers,
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

func (c Client) fetchAndParse(rawURL string) (parsedFetch, error) {
	source, err := NewSubscriptionSource(rawURL)
	if err != nil {
		return parsedFetch{Source: source}, err
	}
	if c.Fetcher == nil {
		c.Fetcher = FetcherFunc(FetchURL)
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	fetched, err := c.Fetcher.FetchURL(source.URL)
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

func FetchURL(rawURL string) (FetchResult, error) {
	var result FetchResult
	source, err := NewSubscriptionSource(rawURL)
	if err != nil {
		return result, err
	}

	req, err := http.NewRequest(http.MethodGet, source.URL, nil)
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
	pool, _ := x509.SystemCertPool()
	return rootCAPoolFromDirs(pool, androidRootCAPaths)
}

func rootCAPoolFromDirs(pool *x509.CertPool, dirs []string) *x509.CertPool {
	if pool == nil {
		pool = x509.NewCertPool()
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			appendCertsFromPEMOrDER(pool, data)
		}
	}
	if len(pool.Subjects()) == 0 {
		return nil
	}
	return pool
}

func appendCertsFromPEMOrDER(pool *x509.CertPool, data []byte) {
	if pool.AppendCertsFromPEM(data) {
		return
	}
	if cert, err := x509.ParseCertificate(data); err == nil {
		pool.AddCert(cert)
		return
	}
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
			pool.AddCert(cert)
		}
	}
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
	ips, err := newBootstrapResolver().LookupIPAddr(ctx, host)
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
