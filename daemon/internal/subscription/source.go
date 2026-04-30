package subscription

import (
	"fmt"
	"net"
	neturl "net/url"
	"strings"

	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
)

func providerKeyFor(rawURL string) string {
	return strings.ToLower(strings.TrimSpace(rawURL))
}

func NewSubscriptionSource(rawURL string) (SubscriptionSource, error) {
	var source SubscriptionSource
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return source, fmt.Errorf("url is required")
	}
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return source, fmt.Errorf("invalid URL: %w", err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return source, fmt.Errorf("subscription URL scheme must be http or https")
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return source, fmt.Errorf("subscription URL host is required")
	}
	if isDisallowedSourceHost(host) {
		return source, fmt.Errorf("subscription URL host is local, private, or reserved")
	}
	source.URL = parsed.String()
	source.ProviderKey = providerKeyFor(source.URL)
	return source, nil
}

func isDisallowedSourceHost(host string) bool {
	return profiledoc.IsDisallowedSubscriptionEndpoint(host)
}

func isDisallowedSourceIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return profiledoc.IsDisallowedSubscriptionEndpoint(ip.String())
}
