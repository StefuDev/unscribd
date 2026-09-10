package unscribd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Keep reusable font subsets bounded in memory.
const maxFontCacheBytes = 16 << 20

var fontCache = struct {
	sync.Mutex
	items map[string][]byte
	used  int
}{items: make(map[string][]byte)}

func cachedFont(key string) ([]byte, bool) {
	fontCache.Lock()
	defer fontCache.Unlock()
	data, ok := fontCache.items[key]
	return data, ok
}

func cacheFont(key string, data []byte) {
	if len(data) == 0 || len(data) > maxFontCacheBytes {
		return
	}
	fontCache.Lock()
	defer fontCache.Unlock()
	if old, ok := fontCache.items[key]; ok {
		fontCache.used -= len(old)
	}
	for fontCache.used+len(data) > maxFontCacheBytes && len(fontCache.items) > 0 {
		for oldKey, old := range fontCache.items {
			delete(fontCache.items, oldKey)
			fontCache.used -= len(old)
			break
		}
	}
	fontCache.items[key] = data
	fontCache.used += len(data)
}

func cacheDirectory(configured string) string {
	if configured != "" {
		return configured
	}
	if value := os.Getenv("UNSCRIBD_CACHE_DIR"); value != "" {
		return value
	}
	return filepath.Join(os.TempDir(), "unscribd-cache")
}

func cacheLimit(configured int64) int64 {
	if configured > 0 {
		return configured
	}
	if value := os.Getenv("UNSCRIBD_CACHE_BYTES"); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 256 << 20
}

func cachePath(directory, key string) string {
	digest := sha256.Sum256([]byte(key))
	return filepath.Join(directory, hex.EncodeToString(digest[:])+".cache")
}

func copyFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	part := destination + ".part"
	_ = os.Remove(part)
	if err := os.Link(source, part); err == nil {
		return os.Rename(part, destination)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(part)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(part)
		return closeErr
	}
	return os.Rename(part, destination)
}

func cacheGet(directory, key, destination string) bool {
	if directory == "" {
		return false
	}
	source := cachePath(directory, key)
	info, err := os.Stat(source)
	if err != nil {
		return false
	}
	if time.Since(info.ModTime()) > 24*time.Hour {
		_ = os.Remove(source)
		return false
	}
	if filepath.Clean(source) == filepath.Clean(destination) {
		return true
	}
	if err := copyFile(source, destination); err != nil {
		return false
	}
	_ = os.Chtimes(source, time.Now(), time.Now())
	return true
}

func cacheReadJSON(directory, key string, destination any) bool {
	path := cachePath(directory, key)
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > 24*time.Hour {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, destination) != nil {
		return false
	}
	_ = os.Chtimes(path, time.Now(), time.Now())
	return true
}

func cacheWriteJSON(directory, key string, value any) {
	if os.MkdirAll(directory, 0755) != nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	path := cachePath(directory, key)
	part := path + ".part"
	if os.WriteFile(part, data, 0600) == nil {
		_ = os.Rename(part, path)
	}
}

func cachePut(directory, key, source string, limit int64) {
	if directory == "" {
		return
	}
	if os.MkdirAll(directory, 0755) != nil {
		return
	}
	destination := cachePath(directory, key)
	if _, err := os.Stat(destination); err != nil {
		_ = copyFile(source, destination)
	}
	_ = limit // eviction is batched once per completed asset/PDF stage
}

func trimCache(directory string, limit int64) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	type cacheEntry struct {
		path string
		size int64
		when time.Time
	}
	items := make([]cacheEntry, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		items = append(items, cacheEntry{filepath.Join(directory, entry.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	if total <= limit {
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].when.Before(items[j].when) })
	for _, item := range items {
		if total <= limit {
			break
		}
		if os.Remove(item.path) == nil {
			total -= item.size
		}
	}
}
