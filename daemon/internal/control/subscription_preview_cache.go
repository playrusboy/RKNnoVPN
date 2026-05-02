package control

import (
	"fmt"
	"sync"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/subscription"
)

const subscriptionPreviewTTL = 5 * time.Minute

type SubscriptionPreviewCache struct {
	mu       sync.Mutex
	previews map[string]cachedSubscriptionPreview
}

type cachedSubscriptionPreview struct {
	preview   subscription.PreviewResult
	createdAt time.Time
}

func NewSubscriptionPreviewCache() *SubscriptionPreviewCache {
	return &SubscriptionPreviewCache{previews: make(map[string]cachedSubscriptionPreview)}
}

func (c *SubscriptionPreviewCache) Put(rawURL string, preview subscription.PreviewResult, now time.Time) {
	if c == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	key := subscriptionPreviewCacheKey(rawURL, preview.Source)
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupLocked(now)
	c.previews[key] = cachedSubscriptionPreview{preview: preview, createdAt: now}
}

func (c *SubscriptionPreviewCache) Take(rawURL string, now time.Time) (subscription.PreviewResult, bool) {
	if c == nil {
		return subscription.PreviewResult{}, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	source, err := subscription.NewSubscriptionSource(rawURL)
	if err != nil {
		return subscription.PreviewResult{}, false
	}
	key := subscriptionPreviewCacheKey(rawURL, source)
	if key == "" {
		return subscription.PreviewResult{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupLocked(now)
	cached, ok := c.previews[key]
	if !ok {
		return subscription.PreviewResult{}, false
	}
	delete(c.previews, key)
	if now.Sub(cached.createdAt) > subscriptionPreviewTTL {
		return subscription.PreviewResult{}, false
	}
	return cached.preview, true
}

func (c *SubscriptionPreviewCache) cleanupLocked(now time.Time) {
	for key, cached := range c.previews {
		if now.Sub(cached.createdAt) > subscriptionPreviewTTL {
			delete(c.previews, key)
		}
	}
}

func subscriptionPreviewCacheKey(rawURL string, source subscription.SubscriptionSource) string {
	if source.URL != "" {
		return source.URL
	}
	if parsed, err := subscription.NewSubscriptionSource(rawURL); err == nil {
		return parsed.URL
	}
	return fmt.Sprintf("raw:%s", rawURL)
}
