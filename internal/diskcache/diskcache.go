// Package diskcache keeps downloaded files on disk under a size limit, so a
// picture fetched once is not fetched again after it leaves memory or after
// the app restarts. It drops the file used longest ago when it is over the
// limit.
package diskcache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/elianiva/meiro/internal/fsutil"
)

const extension = ".bin"

type entry struct {
	size int64
	used time.Time
}

// Cache is a directory of files named by the hash of their key. It is safe
// for concurrent use.
type Cache struct {
	dir   string
	limit int64

	mu    sync.Mutex
	files map[string]entry
	size  int64
}

// Open uses dir, creating it when missing, and counts what it already holds
// by file size and modification time. limit is in bytes.
func Open(dir string, limit int64) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	c := &Cache{dir: dir, limit: limit, files: make(map[string]entry)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), extension) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		c.files[e.Name()] = entry{size: info.Size(), used: info.ModTime()}
		c.size += info.Size()
	}
	c.mu.Lock()
	c.trimLocked("")
	c.mu.Unlock()
	return c, nil
}

func name(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16]) + extension
}

// Has reports whether key is held, without reading it.
func (c *Cache) Has(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.files[name(key)]
	return ok
}

// Get returns the file kept under key and marks it used.
func (c *Cache) Get(key string) ([]byte, bool) {
	file := name(key)
	c.mu.Lock()
	e, ok := c.files[file]
	if ok {
		e.used = time.Now()
		c.files[file] = e
	}
	c.mu.Unlock()
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(c.dir, file))
	if err != nil {
		c.mu.Lock()
		c.forgetLocked(file)
		c.mu.Unlock()
		return nil, false
	}
	// The time on disk orders files after a restart.
	_ = os.Chtimes(filepath.Join(c.dir, file), e.used, e.used)
	return data, true
}

// Put keeps data under key, then drops the files used longest ago while the
// cache is over its limit. A file larger than the limit is not kept.
func (c *Cache) Put(key string, data []byte) {
	if int64(len(data)) > c.limit {
		return
	}
	file := name(key)
	if err := fsutil.WriteFileAtomic(filepath.Join(c.dir, file), data, 0o600); err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forgetLocked(file)
	c.files[file] = entry{size: int64(len(data)), used: time.Now()}
	c.size += int64(len(data))
	c.trimLocked(file)
}

func (c *Cache) forgetLocked(file string) {
	if old, ok := c.files[file]; ok {
		c.size -= old.size
		delete(c.files, file)
	}
}

// trimLocked removes least recently used files, other than keep, once the
// cache is over its limit. It makes room for a tenth of the limit more than
// it needs, so a full cache sorts its files once per run of writes rather
// than on every one.
func (c *Cache) trimLocked(keep string) {
	if c.size <= c.limit {
		return
	}
	target := c.limit - c.limit/10
	names := make([]string, 0, len(c.files))
	for file := range c.files {
		if file != keep {
			names = append(names, file)
		}
	}
	sort.Slice(names, func(i, j int) bool { return c.files[names[i]].used.Before(c.files[names[j]].used) })
	for _, file := range names {
		if c.size <= target {
			return
		}
		_ = os.Remove(filepath.Join(c.dir, file))
		c.forgetLocked(file)
	}
}
