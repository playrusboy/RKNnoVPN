package control

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
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

func (c *SubscriptionPreviewCache) Put(preview subscription.PreviewResult, now time.Time) (string, error) {
	if c == nil {
		return "", fmt.Errorf("subscription preview cache is not configured")
	}
	if now.IsZero() {
		now = time.Now()
	}
	previewID, err := newSubscriptionPreviewID()
	if err != nil {
		return "", err
	}
	preview.PreviewID = previewID
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupLocked(now)
	c.previews[previewID] = cachedSubscriptionPreview{preview: preview, createdAt: now}
	return previewID, nil
}

func (c *SubscriptionPreviewCache) Take(previewID string, now time.Time) (subscription.PreviewResult, bool) {
	if c == nil {
		return subscription.PreviewResult{}, false
	}
	previewID = strings.TrimSpace(previewID)
	if previewID == "" {
		return subscription.PreviewResult{}, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupLocked(now)
	cached, ok := c.previews[previewID]
	if !ok {
		return subscription.PreviewResult{}, false
	}
	delete(c.previews, previewID)
	if now.Sub(cached.createdAt) > subscriptionPreviewTTL {
		return subscription.PreviewResult{}, false
	}
	return cached.preview, true
}

func (c *SubscriptionPreviewCache) cleanupLocked(now time.Time) {
	for previewID, cached := range c.previews {
		if now.Sub(cached.createdAt) > subscriptionPreviewTTL {
			delete(c.previews, previewID)
		}
	}
}

func newSubscriptionPreviewID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate subscription preview id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
