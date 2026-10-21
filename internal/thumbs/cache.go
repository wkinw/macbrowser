package thumbs

import (
	"container/list"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Cache struct {
	dir      string
	mu       sync.Mutex
	mem      map[string]*list.Element
	lru      *list.List
	memBytes int64
	maxMem   int64
	inflight map[string]*genCall
	failed   map[string]time.Time
}

type memItem struct {
	key  string
	data []byte
}

type genCall struct {
	done chan struct{}
	data []byte
	err  error
}

func New(dir string, maxMemMB int) *Cache {
	os.MkdirAll(dir, 0o777)
	return &Cache{
		dir:      dir,
		mem:      map[string]*list.Element{},
		lru:      list.New(),
		maxMem:   int64(maxMemMB) << 20,
		inflight: map[string]*genCall{},
		failed:   map[string]time.Time{},
	}
}

func Key(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *Cache) filePath(key string) string {
	return filepath.Join(c.dir, key[:2], key[2:4], key+".jpg")
}

func (c *Cache) Get(key string, gen func() ([]byte, error)) ([]byte, error) {
	if data := c.getMem(key); data != nil {
		return data, nil
	}
	if data := c.getDisk(key); data != nil {
		c.putMem(key, data)
		return data, nil
	}

	c.mu.Lock()
	if t, ok := c.failed[key]; ok && time.Since(t) < time.Minute {
		c.mu.Unlock()
		return nil, os.ErrNotExist
	}
	if call, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.data, call.err
	}
	call := &genCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	data, err := gen()

	c.mu.Lock()
	delete(c.inflight, key)
	if err != nil {
		c.failed[key] = time.Now()
	}
	c.mu.Unlock()
	call.data, call.err = data, err
	close(call.done)

	if err != nil {
		return nil, err
	}
	c.putMem(key, data)
	c.putDisk(key, data)
	return data, nil
}

func (c *Cache) getMem(key string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.mem[key]
	if !ok {
		return nil
	}
	c.lru.MoveToFront(el)
	return el.Value.(*memItem).data
}

func (c *Cache) putMem(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.mem[key]; ok {
		c.lru.MoveToFront(el)
		return
	}
	item := &memItem{key: key, data: data}
	el := c.lru.PushFront(item)
	c.mem[key] = el
	c.memBytes += int64(len(data))
	for c.memBytes > c.maxMem && c.lru.Len() > 0 {
		last := c.lru.Back()
		if last == nil {
			break
		}
		mi := last.Value.(*memItem)
		c.lru.Remove(last)
		delete(c.mem, mi.key)
		c.memBytes -= int64(len(mi.data))
	}
}

func (c *Cache) getDisk(key string) []byte {
	data, err := os.ReadFile(c.filePath(key))
	if err != nil || len(data) == 0 {
		return nil
	}
	return data
}

func (c *Cache) putDisk(key string, data []byte) {
	p := c.filePath(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o666); err != nil {
		return
	}
	os.Rename(tmp, p)
}
