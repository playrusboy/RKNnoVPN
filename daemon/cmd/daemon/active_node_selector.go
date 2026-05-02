package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"reflect"
	"strings"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
)

const singboxProxySelectorTag = "proxy"

func activeNodeSelectorSwitchTarget(oldCfg *config.Config, newCfg *config.Config, reloadPlan rootruntime.ReloadPlan) string {
	if oldCfg == nil || newCfg == nil {
		return ""
	}
	if reloadPlan.FullRestart || reloadPlan.NetstackReapplyAfter || len(reloadPlan.ChangedKeys) > 0 {
		return ""
	}
	if newCfg.Proxy.APIPort <= 0 || strings.TrimSpace(newCfg.Proxy.APISecret) == "" {
		return ""
	}
	oldActive := strings.TrimSpace(oldCfg.Profile.ActiveNodeID)
	newActive := strings.TrimSpace(newCfg.Profile.ActiveNodeID)
	if newActive == "" || oldActive == newActive {
		return ""
	}
	if !sameConfigExceptActiveNodeProjection(oldCfg, newCfg) {
		return ""
	}
	profiles := config.ProfilesFromConfigNodes(newCfg)
	if len(profiles) <= 1 {
		return ""
	}
	for _, profile := range profiles {
		if profile.ID == newActive {
			return strings.TrimSpace(profile.Tag)
		}
	}
	return ""
}

func sameConfigExceptActiveNodeProjection(oldCfg *config.Config, newCfg *config.Config) bool {
	oldCopy := *oldCfg
	newCopy := *newCfg
	oldCopy.Profile.ActiveNodeID = ""
	newCopy.Profile.ActiveNodeID = ""
	oldCopy.Node = config.NodeConfig{}
	newCopy.Node = config.NodeConfig{}
	oldCopy.Transport = config.TransportConfig{}
	newCopy.Transport = config.TransportConfig{}
	return reflect.DeepEqual(oldCopy, newCopy)
}

func switchSingboxSelector(cfg *config.Config, selectorTag string, outboundTag string) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.Proxy.APIPort <= 0 {
		return fmt.Errorf("clash API is disabled")
	}
	secret := strings.TrimSpace(cfg.Proxy.APISecret)
	if secret == "" {
		return fmt.Errorf("clash API secret is empty")
	}
	selectorTag = strings.TrimSpace(selectorTag)
	outboundTag = strings.TrimSpace(outboundTag)
	if selectorTag == "" || outboundTag == "" {
		return fmt.Errorf("selector and outbound tags are required")
	}
	payload, err := json.Marshal(map[string]string{"name": outboundTag})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf(
		"http://127.0.0.1:%d/proxies/%s",
		cfg.Proxy.APIPort,
		neturl.PathEscape(selectorTag),
	)
	request, err := http.NewRequest(http.MethodPut, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+secret)

	client := &http.Client{Timeout: 2500 * time.Millisecond}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("clash selector HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
