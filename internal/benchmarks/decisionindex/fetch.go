// Fetch-or-cache for the Decision Index's three JSON files, following
// internal/pricing's rules (ADR-009): cache under
// $XDG_CACHE_HOME/conclave/decision-index/, fresh for CONCLAVE_DECISION_INDEX_TTL
// hours (default 24), atomic writes, and a stale cache beats no data when a
// refresh fails (served with a warning). Bodies are cached verbatim; freshness
// is the file's mtime, so no sidecar metadata can drift from the data.
//
// Unlike pricing there is no background refresh: --frontier is an explicit,
// interactive command, not a per-query side lookup, so one bounded
// synchronous fetch is acceptable.
package decisionindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	defaultTTL = 24 * time.Hour
	// fetchTimeout bounds each file. Longer than pricing's 6s: the index file
	// is ~950 KB from Hugging Face's CDN, and this runs only on --frontier.
	fetchTimeout = 20 * time.Second
	// maxBody guards against a runaway response; the largest file is ~1 MB.
	maxBody = 16 << 20
)

type fetcher struct {
	dir     string
	ttl     time.Duration
	refresh bool
	client  *http.Client
}

// get returns the body for name, from cache when fresh, otherwise fetched
// from url. warn is non-empty when a stale cache stood in for a failed fetch.
func (f fetcher) get(ctx context.Context, name, url string) (body []byte, warn string, err error) {
	path := filepath.Join(f.dir, name)
	cached, cacheErr := os.ReadFile(path)
	var age time.Duration
	if cacheErr == nil {
		if st, err := os.Stat(path); err == nil {
			age = time.Since(st.ModTime())
		}
		if !f.refresh && age <= f.ttl {
			return cached, "", nil
		}
	}
	body, err = f.fetch(ctx, url)
	if err != nil {
		if cacheErr == nil {
			return cached, fmt.Sprintf("decision index: refresh of %s failed, using cache %s old: %v",
				name, age.Round(time.Hour), err), nil
		}
		return nil, "", err
	}
	if werr := writeAtomic(path, body); werr != nil {
		return body, "decision index cache not written: " + werr.Error(), nil
	}
	return body, "", nil
}

func (f fetcher) fetch(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "conclave-cli (+https://github.com/0xDarkMatter/conclave)")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	// A 200 HTML page (a CDN interstitial) must not overwrite a good cache.
	if !json.Valid(b) {
		return nil, errors.New("response from " + url + " is not JSON")
	}
	return b, nil
}

// writeAtomic is temp file + rename so a concurrent conclave never reads a
// half-written file. The remove-before-rename mirrors internal/pricing: some
// Windows setups refuse to rename over an existing file.
func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".di-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	_ = os.Remove(path)
	return os.Rename(name, path)
}

func ttlFromEnv() time.Duration {
	if v := os.Getenv("CONCLAVE_DECISION_INDEX_TTL"); v != "" {
		if h, err := strconv.ParseFloat(v, 64); err == nil && h > 0 {
			return time.Duration(h * float64(time.Hour))
		}
	}
	return defaultTTL
}
