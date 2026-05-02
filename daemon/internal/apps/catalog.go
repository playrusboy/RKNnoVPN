package apps

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultPackagesListPath = "/data/system/packages.list"

type Catalog struct {
	mu    sync.Mutex
	cache appCatalogCache
}

type appCatalogCache struct {
	path  string
	mtime time.Time
	size  int64
	apps  []Info
}

type Info struct {
	PackageName string  `json:"packageName"`
	AppName     string  `json:"appName"`
	UID         int     `json:"uid"`
	IsSystemApp bool    `json:"isSystemApp"`
	Category    string  `json:"category"`
	ApkPath     *string `json:"apkPath,omitempty"`
	VersionName *string `json:"versionName,omitempty"`
	Enabled     bool    `json:"enabled"`
}

func (c *Catalog) LoadInstalled(path string) ([]Info, error) {
	if c == nil {
		return LoadInstalled(path)
	}

	path = packagesListPath(path)

	c.mu.Lock()
	defer c.mu.Unlock()

	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat packages.list: %w", err)
	}

	if c.cache.path == path &&
		c.cache.size == stat.Size() &&
		c.cache.mtime.Equal(stat.ModTime()) {
		return cloneInfos(c.cache.apps), nil
	}

	apps, err := LoadInstalled(path)
	if err != nil {
		return nil, err
	}

	c.cache = appCatalogCache{
		path:  path,
		mtime: stat.ModTime(),
		size:  stat.Size(),
		apps:  cloneInfos(apps),
	}
	return cloneInfos(apps), nil
}

func LoadInstalled(path string) ([]Info, error) {
	path = packagesListPath(path)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read packages.list: %w", err)
	}

	apps := make([]Info, 0)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		uid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}

		dataDir := ""
		if len(fields) >= 4 {
			dataDir = fields[3]
		}

		isSystem := strings.HasPrefix(dataDir, "/system/") ||
			strings.HasPrefix(dataDir, "/vendor/") ||
			strings.HasPrefix(dataDir, "/product/") ||
			strings.HasPrefix(dataDir, "/system_ext/")

		apps = append(apps, Info{
			PackageName: fields[0],
			AppName:     prettyPackageLabel(fields[0]),
			UID:         uid,
			IsSystemApp: isSystem,
			Category:    classify(fields[0], isSystem),
			Enabled:     true,
		})
	}

	return apps, nil
}

func ResolveUID(apps []Info, uid int) (Info, bool) {
	var fallback *Info
	for _, app := range apps {
		if app.UID == uid {
			return app, true
		}
		if fallback == nil && app.UID%100000 == uid%100000 {
			appCopy := app
			fallback = &appCopy
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return Info{}, false
}

func packagesListPath(path string) string {
	if path == "" {
		return DefaultPackagesListPath
	}
	return path
}

func cloneInfos(apps []Info) []Info {
	if apps == nil {
		return nil
	}
	cloned := make([]Info, len(apps))
	copy(cloned, apps)
	for i := range cloned {
		if apps[i].ApkPath != nil {
			apkPath := *apps[i].ApkPath
			cloned[i].ApkPath = &apkPath
		}
		if apps[i].VersionName != nil {
			versionName := *apps[i].VersionName
			cloned[i].VersionName = &versionName
		}
	}
	return cloned
}

func prettyPackageLabel(packageName string) string {
	last := packageName
	if idx := strings.LastIndex(packageName, "."); idx != -1 && idx+1 < len(packageName) {
		last = packageName[idx+1:]
	}
	last = strings.ReplaceAll(last, "_", " ")
	last = strings.ReplaceAll(last, "-", " ")
	if last == "" {
		return packageName
	}
	return strings.ToUpper(last[:1]) + last[1:]
}

func classify(packageName string, isSystem bool) string {
	if isSystem {
		return "SYSTEM"
	}

	lower := strings.ToLower(packageName)
	switch {
	case strings.Contains(lower, "telegram"),
		strings.Contains(lower, "whatsapp"),
		strings.Contains(lower, "discord"),
		strings.Contains(lower, "signal"),
		strings.Contains(lower, "messenger"):
		return "MESSAGING"
	case strings.Contains(lower, "youtube"),
		strings.Contains(lower, "netflix"),
		strings.Contains(lower, "twitch"),
		strings.Contains(lower, "video"):
		return "VIDEO"
	case strings.Contains(lower, "spotify"),
		strings.Contains(lower, "music"),
		strings.Contains(lower, "audio"):
		return "AUDIO"
	case strings.Contains(lower, "chrome"),
		strings.Contains(lower, "firefox"),
		strings.Contains(lower, "browser"),
		strings.Contains(lower, "opera"),
		strings.Contains(lower, "brave"):
		return "BROWSER"
	case strings.Contains(lower, "game"):
		return "GAME"
	case strings.Contains(lower, "bank"),
		strings.Contains(lower, "wallet"),
		strings.Contains(lower, "finance"),
		strings.Contains(lower, "sber"),
		strings.Contains(lower, "tinkoff"):
		return "PRODUCTIVITY"
	case strings.Contains(lower, "social"),
		strings.Contains(lower, "twitter"),
		strings.Contains(lower, "instagram"),
		strings.Contains(lower, "reddit"),
		strings.Contains(lower, "facebook"),
		strings.Contains(lower, "vk"):
		return "SOCIAL"
	default:
		return "OTHER"
	}
}
