package modcart

import (
	"container/list"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// jarSHA1CacheMax 本地 jar 的 SHA-1 进程内缓存上限（A32）：满了清掉最旧的条目。
const jarSHA1CacheMax = 10000

type sha1CacheKey struct {
	path    string
	size    int64
	modNano int64
}

type sha1CacheEntry struct {
	key sha1CacheKey
	sum string
}

type sha1Cache struct {
	mu      sync.Mutex
	max     int
	order   *list.List
	entries map[sha1CacheKey]*list.Element
}

func newSHA1Cache(maxEntries int) *sha1Cache {
	if maxEntries <= 0 {
		maxEntries = jarSHA1CacheMax
	}
	return &sha1Cache{
		max:     maxEntries,
		order:   list.New(),
		entries: make(map[sha1CacheKey]*list.Element),
	}
}

func (c *sha1Cache) get(k sha1CacheKey) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[k]
	if !ok {
		return "", false
	}
	return el.Value.(sha1CacheEntry).sum, true
}

func (c *sha1Cache) put(k sha1CacheKey, sum string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[k]; ok {
		el.Value = sha1CacheEntry{key: k, sum: sum}
		c.order.MoveToBack(el)
		return
	}
	for c.order.Len() >= c.max {
		oldest := c.order.Front()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(sha1CacheEntry).key)
	}
	el := c.order.PushBack(sha1CacheEntry{key: k, sum: sum})
	c.entries[k] = el
}

var jarSHA1Cache = newSHA1Cache(jarSHA1CacheMax)

// cachedFileSHA1 计算本地 jar 的 SHA-1，按「完整路径 + 大小 + 修改时间」做进程内缓存（A32）。
// 读盘与哈希计算在缓存锁外执行；读取前后若大小或修改时间发生变化，不写入缓存。
func cachedFileSHA1(path string) (string, error) {
	return jarSHA1Cache.fileSHA1(path)
}

func (c *sha1Cache) fileSHA1(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(abs)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	key := sha1CacheKey{path: abs, size: st.Size(), modNano: st.ModTime().UnixNano()}
	if sum, ok := c.get(key); ok {
		return sum, nil
	}

	f, err := openJarForSHA1(abs)
	if err != nil {
		return "", err
	}
	stBefore, err := f.Stat()
	if err != nil {
		f.Close()
		return "", err
	}
	h := sha1.New()
	written, copyErr := io.Copy(h, f)
	stAfter, statAfterErr := f.Stat()
	_ = f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	sum := hex.EncodeToString(h.Sum(nil))

	if statAfterErr == nil && written == stBefore.Size() && stAfter.Size() == key.size && stAfter.ModTime().UnixNano() == key.modNano {
		c.put(key, sum)
	}
	return sum, nil
}
