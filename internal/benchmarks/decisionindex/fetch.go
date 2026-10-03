// Fetch-or-cache for the Decision Index's three JSON files, following
// internal/pricing's rules (ADR-009): cache under
// $XDG_CACHE_HOME/conclave/decision-index/<Edition>/, fresh for
// CONCLAVE_DECISION_INDEX_TTL hours (default 24, clamped to 1 minute..30
// days), atomic writes, and a stale cache beats no data when a refresh fails
// (served with a warning). Bodies are cached verbatim; freshness is the
// file's mtime, so no sidecar metadata can drift from the data.
//
// Invariant: NOTHING reaches the cache unvalidated. The upstream pair
// (methodology + index) is fetched into memory as one generation, validated
// and fully computed, and only then written; the mirror is identity-checked
// before it is written. A refresh that is valid JSON but a corrupt edition
// therefore leaves the previous cache in place and serves it with a warning.
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
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	defaultTTL = 24 * time.Hour
	// TTL bounds: below a minute every call refetches ~1.2 MB from Hugging
	// Face; past 30 days a cache outlives any plausible edition. The bounds
	// also keep the hours->Duration conversion far from int64 overflow, which
	// once turned +Inf into a negative TTL and made every cache stale.
	minTTL = time.Minute
	maxTTL = 30 * 24 * time.Hour
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

// path keys every cached file by edition (<dir>/v0.2.1/<name>), so an
// edition bump can never read the previous edition's files as its own.
func (f fetcher) path(name string) string { return filepath.Join(f.dir, Edition, name) }

// cached returns a cached body and its age; ok is false when it is absent.
func (f fetcher) cached(name string) (body []byte, age time.Duration, ok bool) {
	p := f.path(name)
	body, err := os.ReadFile(p)
	if err != nil {
		return nil, 0, false
	}
	if st, err := os.Stat(p); err == nil {
		age = time.Since(st.ModTime())
	}
	return body, age, true
}

// generation returns the computed upstream edition: from cache when both
// files are present, valid and fresh; otherwise fetched, validated, computed
// and only then cached. warns carries stale-cache and cache-write notices.
func (f fetcher) generation(ctx context.Context, base string) (g *computed, warns []string, err error) {
	cm, ageM, okM := f.cached(MethodologyFile)
	ci, ageI, okI := f.cached(IndexFile)
	age := max(ageM, ageI)
	var prev *computed
	if okM && okI {
		// Re-validated on every read: a hand-edited or half-migrated cache
		// is treated as absent rather than trusted.
		prev, _ = compute(cm, ci)
	}
	if prev != nil && !f.refresh && age <= f.ttl {
		return prev, nil, nil
	}
	meth, err := f.fetch(ctx, base+MethodologyFile)
	var idx []byte
	if err == nil {
		idx, err = f.fetch(ctx, base+IndexFile)
	}
	if err == nil {
		g, err = compute(meth, idx)
	}
	if err != nil {
		if prev != nil {
			return prev, []string{fmt.Sprintf("decision index %s: refresh failed, using cache %s old: %v",
				Edition, age.Round(time.Minute), err)}, nil
		}
		return nil, nil, err
	}
	// Two atomic renames, not one: Windows cannot rename a directory over an
	// existing one. A crash between them leaves a mixed pair, which the
	// re-validation above either accepts (both files are the pinned
	// edition's) or treats as no cache and refetches.
	for _, w := range []struct {
		name string
		body []byte
	}{{IndexFile, idx}, {MethodologyFile, meth}} {
		if werr := writeAtomic(f.path(w.name), w.body); werr != nil {
			warns = append(warns, "decision index cache not written: "+werr.Error())
			break
		}
	}
	return g, warns, nil
}

// get returns one independently cached body (the mirror), from cache when
// present, valid and fresh, otherwise fetched from url. valid runs on the
// fetched body BEFORE it can replace the cache. warn is non-empty when a
// stale cache stood in for a failed or invalid refresh.
func (f fetcher) get(ctx context.Context, name, url string, valid func([]byte) error) (body []byte, warn string, err error) {
	cached, age, ok := f.cached(name)
	if ok && valid(cached) != nil {
		ok = false
	}
	if ok && !f.refresh && age <= f.ttl {
		return cached, "", nil
	}
	body, err = f.fetch(ctx, url)
	if err == nil {
		err = valid(body)
	}
	if err != nil {
		if ok {
			return cached, fmt.Sprintf("decision index: refresh of %s failed, using cache %s old: %v",
				name, age.Round(time.Minute), err), nil
		}
		return nil, "", err
	}
	if werr := writeAtomic(f.path(name), body); werr != nil {
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
	// A 200 HTML page (a CDN interstitial) is not even a candidate; semantic
	// validation is the caller's (generation, get).
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

// ttlFromEnv reads CONCLAVE_DECISION_INDEX_TTL in hours. Unparseable,
// non-finite or non-positive values fall back to the default; the rest are
// clamped to [minTTL, maxTTL] in float hours BEFORE conversion, because
// time.Duration(+Inf) overflows to a negative duration.
func ttlFromEnv() time.Duration {
	v := os.Getenv("CONCLAVE_DECISION_INDEX_TTL")
	if v == "" {
		return defaultTTL
	}
	h, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(h) || math.IsInf(h, 0) || h <= 0 {
		return defaultTTL
	}
	switch {
	case h >= maxTTL.Hours():
		return maxTTL
	case h <= minTTL.Hours():
		return minTTL
	}
	return time.Duration(h * float64(time.Hour))
}
