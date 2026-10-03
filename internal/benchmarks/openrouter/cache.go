// This file owns the disk boundary for OpenRouter benchmark data (ADR-018).
// Cache envelopes persist only normalized public data plus fetch time; request
// credentials are structurally absent. Writes use a same-directory temporary
// file and rename so concurrent readers never observe a partial JSON document.
// Freshness follows ADR-009: 24 hours unless CONCLAVE_BENCHMARKS_TTL overrides.
package openrouter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	feedCacheFile   = "feed.json"
	modelsCacheFile = "decision-models.json"
)

type feedCache struct {
	FetchedAt time.Time `json:"fetched_at"`
	Feed      *Feed     `json:"feed"`
}

func (c *feedCache) fetchedAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.FetchedAt
}

type modelsCache struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Models    []DecisionModel `json:"models"`
}

func (c *modelsCache) fetchedAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.FetchedAt
}

type settings struct {
	cacheDir string
	ttl      time.Duration
	client   *http.Client
}

func (s settings) cachePath(name string) string { return filepath.Join(s.cacheDir, name) }

func resolveSettings(opts Options) (settings, error) {
	cacheDir := strings.TrimSpace(opts.CacheDir)
	if cacheDir == "" {
		cacheDir = defaultCacheDir()
	}
	ttl := opts.TTL
	if ttl == 0 {
		ttl = ttlFromEnv()
	}
	client := &http.Client{Timeout: fetchTimeout}
	if opts.HTTPClient != nil {
		var ok bool
		client, ok = opts.HTTPClient.(*http.Client)
		if !ok || client == nil {
			return settings{}, fmt.Errorf("openrouter benchmarks HTTPClient must be *http.Client, got %T", opts.HTTPClient)
		}
	}
	return settings{cacheDir: cacheDir, ttl: ttl, client: client}, nil
}

func readFeedCache(path string) (*feedCache, error) {
	var cached feedCache
	found, err := readJSONCache(path, &cached)
	if err != nil || !found || cached.Feed == nil {
		return nil, err
	}
	return &cached, nil
}

func readModelsCache(path string) (*modelsCache, error) {
	var cached modelsCache
	found, err := readJSONCache(path, &cached)
	if err != nil || !found || cached.Models == nil {
		return nil, err
	}
	return &cached, nil
}

func readJSONCache(path string, destination any) (bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return false, fmt.Errorf("corrupt openrouter benchmark cache %s: %w", filepath.Base(path), err)
	}
	return true, nil
}

// writeJSONCache's envelope is part of the disk wire format: fetched_at plus
// normalized feed/models only. Options must never be added to this payload,
// because Options contains OPENROUTER_API_KEY material.
func writeJSONCache(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".openrouter-benchmarks-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Windows can reject replacement renames. The brief missing-file window is
	// safe because readers interpret it as a cache miss and fetch public data.
	_ = os.Remove(path)
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func cacheFresh(fetchedAt time.Time, ttl time.Duration) bool {
	return !fetchedAt.IsZero() && time.Since(fetchedAt) <= ttl
}

func disabled() bool {
	value := strings.TrimSpace(os.Getenv("CONCLAVE_NO_PRICING"))
	return value != "" && value != "0" && !strings.EqualFold(value, "false")
}

func ttlFromEnv() time.Duration {
	if value := strings.TrimSpace(os.Getenv("CONCLAVE_BENCHMARKS_TTL")); value != "" {
		if hours, err := strconv.ParseFloat(value, 64); err == nil && hours > 0 {
			return time.Duration(hours * float64(time.Hour))
		}
	}
	return defaultTTL
}

func defaultCacheDir() string {
	if root := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); root != "" {
		return filepath.Join(root, "conclave", "openrouter-benchmarks")
	}
	if root, err := os.UserCacheDir(); err == nil && root != "" {
		return filepath.Join(root, "conclave", "openrouter-benchmarks")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "conclave", "openrouter-benchmarks")
	}
	return filepath.Join(os.TempDir(), "conclave", "openrouter-benchmarks")
}
