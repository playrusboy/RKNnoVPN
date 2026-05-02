package subscription

import profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"

type SubscriptionSource struct {
	ProviderKey string `json:"providerKey"`
	URL         string `json:"url"`
}

func (s SubscriptionSource) NodeSource(nowMillis int64) profiledoc.NodeSource {
	return profiledoc.NodeSource{
		Type:        "SUBSCRIPTION",
		URL:         s.URL,
		ProviderKey: s.ProviderKey,
		LastSeenAt:  nowMillis,
	}
}

type RejectedNode struct {
	Link     string `json:"link,omitempty"`
	Name     string `json:"name,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Server   string `json:"server,omitempty"`
	Port     int    `json:"port,omitempty"`
	Code     string `json:"code"`
	Reason   string `json:"reason"`
}

type PreviewResult struct {
	PreviewID     string                  `json:"previewId,omitempty"`
	Source        SubscriptionSource      `json:"source"`
	Subscription  profiledoc.Subscription `json:"subscription"`
	Nodes         []profiledoc.Node       `json:"nodes"`
	RejectedNodes []RejectedNode          `json:"rejectedNodes"`
	Rejected      int                     `json:"rejected"`
	Added         int                     `json:"added"`
	Updated       int                     `json:"updated"`
	Unchanged     int                     `json:"unchanged"`
	Stale         int                     `json:"stale"`
	ParseFailures int                     `json:"parseFailures"`
	FetchStatus   int                     `json:"-"`
	FetchHeaders  map[string]string       `json:"-"`
}

type RefreshResult struct {
	Source        SubscriptionSource      `json:"source"`
	Profile       profiledoc.Document     `json:"profile"`
	Subscription  profiledoc.Subscription `json:"subscription"`
	Nodes         []profiledoc.Node       `json:"nodes"`
	RejectedNodes []RejectedNode          `json:"rejectedNodes"`
	Merge         map[string]int          `json:"merge"`
	ParseFailures int                     `json:"parseFailures"`
	FetchStatus   int                     `json:"-"`
	FetchHeaders  map[string]string       `json:"-"`
}

type RefreshResponse struct {
	Source        SubscriptionSource      `json:"source"`
	Subscription  profiledoc.Subscription `json:"subscription"`
	Imported      int                     `json:"imported"`
	ParseFailures int                     `json:"parseFailures"`
	Rejected      int                     `json:"rejected"`
	RejectedNodes []RejectedNode          `json:"rejectedNodes"`
	Merge         map[string]int          `json:"merge"`
}
