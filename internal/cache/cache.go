// Package cache is conclave's opt-in, content-addressed store of provider
// responses. It exists so re-running the same question against the same model
// costs nothing and returns instantly, which is what makes iterating on a
// prompt, a judge choice, or an output format affordable.
//
// Contract:
//   - OPT-IN, never on by default. A silent cache would make conclave lie about
//     freshness: two runs a second apart would look identical even if the model
//     changed underneath. The user asks for it with --cache or CONCLAVE_CACHE_TTL.
//   - The key is a sha256 over (mode, provider, model, full prompt, system
//     prompt). "Full prompt" means the text actually sent, INCLUDING any file or
//     stdin context, so changing a single byte of an attached file is a miss.
//     See docs/adr/ADR-011 for why the key is composed this way.
//   - Only individual provider responses are cached. Judge synthesis is NOT,
//     because a verdict is a function of the SET of responses it saw, and that
//     set is not part of any single provider's key. ADR-011.
//   - Entries live beside the pricing cache at
//     $XDG_CACHE_HOME/conclave/responses/<2-char shard>/<sha256>.json. The shard
//     keeps directory listings small on a store that can grow to thousands of
//     entries.
//   - Every failure is soft. A corrupt entry, an unreadable directory, or a
//     full disk degrades to a cache miss and a live query; it must never fail
//     a user's question.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// DefaultTTL is the freshness window when --cache is given without a value.
// A day is long enough to make an afternoon of prompt iteration free and short
// enough that a stale answer is never quoted as current.
const DefaultTTL = 24 * time.Hour

// Entry is one cached provider response, as stored on disk.
type Entry struct {
	// Key is the sha256 that addresses this entry. Stored so a mismatched or
	// truncated file can be detected instead of being served as an answer.
	Key       string             `json:"key"`
	FetchedAt time.Time          `json:"fetched_at"`
	Mode      string             `json:"mode"` // "cli" or "api"
	Provider  string             `json:"provider"`
	Model     string             `json:"model"`
	Response  string             `json:"response"`
	Metrics   *providers.Metrics `json:"metrics,omitempty"`
}

// Cache is a TTL-bounded response store rooted at a directory.
type Cache struct {
	dir string
	ttl time.Duration
}

// New returns a cache rooted at dir (empty means DefaultDir) with the given
// TTL (zero means DefaultTTL).
func New(dir string, ttl time.Duration) *Cache {
	if dir == "" {
		dir = DefaultDir()
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Cache{dir: dir, ttl: ttl}
}

// DefaultDir is the response store, a sibling of the pricing cache so conclave
// owns exactly one cache root.
func DefaultDir() string { return filepath.Join(pricing.CacheDir(), "responses") }

// Dir reports where this cache reads and writes.
func (c *Cache) Dir() string { return c.dir }

// TTL reports the freshness window in force.
func (c *Cache) TTL() time.Duration { return c.ttl }

// Key derives the content address of one provider call.
//
// Each component is length-prefixed before hashing so no two different tuples
// can produce the same byte stream. Without that, a prompt ending in the next
// field's value could collide with a different split of the same text.
//
// system is the system prompt when a provider grows one. Conclave's Provider
// interface currently takes only a prompt, so callers pass "" — the field is in
// the key from day one so adding system prompts later invalidates old entries
// instead of silently serving answers generated without one.
func Key(mode, provider, model, prompt, system string) string {
	h := sha256.New()
	for _, part := range []string{mode, provider, model, prompt, system} {
		fmt.Fprintf(h, "%d:", len(part))
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// path is the on-disk location for a key.
func (c *Cache) path(key string) string {
	if len(key) < 2 {
		return filepath.Join(c.dir, "_", key+".json")
	}
	return filepath.Join(c.dir, key[:2], key+".json")
}

// Get returns a fresh entry for key. Any problem at all (missing, corrupt,
// expired, key mismatch) is reported as a plain miss.
func (c *Cache) Get(key string) (*Entry, bool) {
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	var e Entry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, false
	}
	if e.Key != key || e.Response == "" {
		return nil, false
	}
	if time.Since(e.FetchedAt) > c.ttl {
		return nil, false
	}
	return &e, true
}

// Put stores an entry. Writes are atomic (temp file + rename) so a concurrent
// conclave never reads a half-written entry.
func (c *Cache) Put(e *Entry) error {
	if e == nil || e.Key == "" || e.Response == "" {
		return nil // nothing worth storing; not an error
	}
	if e.FetchedAt.IsZero() {
		e.FetchedAt = time.Now().UTC()
	}
	p := c.path(e.Key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".resp-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	// Same reason as internal/pricing.writeCache: Windows can refuse a rename
	// over an existing file, and losing the entry only costs a cache miss.
	_ = os.Remove(p)
	return os.Rename(tmpName, p)
}

// Info summarises a store for `conclave cache stats`.
type Info struct {
	Dir     string
	Entries int
	Bytes   int64
	Oldest  time.Time
	Newest  time.Time
}

// Stat walks the store. A missing directory is an empty store, not an error.
func (c *Cache) Stat() (Info, error) {
	info := Info{Dir: c.dir}
	err := filepath.WalkDir(c.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable corner of the store: skip, don't fail
		}
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		info.Entries++
		info.Bytes += fi.Size()
		mt := fi.ModTime()
		if info.Oldest.IsZero() || mt.Before(info.Oldest) {
			info.Oldest = mt
		}
		if mt.After(info.Newest) {
			info.Newest = mt
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return info, err
	}
	return info, nil
}

// Clear removes every entry and returns how many were deleted. It removes the
// store directory wholesale, so it must only ever be pointed at a directory
// conclave owns.
func (c *Cache) Clear() (int, error) {
	info, err := c.Stat()
	if err != nil {
		return 0, err
	}
	if info.Entries == 0 {
		if _, statErr := os.Stat(c.dir); os.IsNotExist(statErr) {
			return 0, nil
		}
	}
	if err := os.RemoveAll(c.dir); err != nil {
		return 0, err
	}
	return info.Entries, nil
}
