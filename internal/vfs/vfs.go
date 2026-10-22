package vfs

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Label struct {
	Color int      `json:"color"`
	Tags  []string `json:"tags,omitempty"`
}

type Entry struct {
	Name   string    `json:"name"`
	Dir    bool      `json:"dir"`
	Size   int64     `json:"size"`
	Mod    time.Time `json:"mod"`
	Hidden bool      `json:"hidden,omitempty"`
	Thumb  bool      `json:"thumb,omitempty"`
	Path   string    `json:"path,omitempty"`
	Label  *Label    `json:"label,omitempty"`
}

type Place struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Connection struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Share    string `json:"share"`
	Base     string `json:"base"`
	User     string `json:"user"`
	Domain   string `json:"domain"`
	Password string `json:"password"`
}

type FS interface {
	Kind() string
	List(dir string) ([]Entry, error)
	Open(path string) (io.ReadSeekCloser, error)
	Stat(path string) (Entry, error)
	Xattr(path, name string) ([]byte, error)
	WriteXattr(path, name string, data []byte) error
	WriteFile(path string, data []byte) error
	WriteFrom(path string, r io.Reader) error
	Mkdir(path string) error
	Remove(path string, recursive bool) error
	Rename(oldpath, newpath string) error
	Join(dir, name string) string
}

var ErrXattrNotSupported = errors.New("xattr not supported")

type Binding struct {
	FS   FS
	Path string
	Kind string
}

func (b *Binding) Child(name string) string {
	return b.FS.Join(b.Path, name)
}

func (b *Binding) SameStore(o *Binding) bool {
	if b == nil || o == nil || b.Kind != o.Kind {
		return false
	}
	switch f := b.FS.(type) {
	case *smbFS:
		of, ok := o.FS.(*smbFS)
		return ok && f.conn.Name == of.conn.Name
	case localFS:
		_, ok := o.FS.(localFS)
		return ok
	}
	return false
}

type Manager struct {
	mu    sync.RWMutex
	conns map[string]Connection
	pool  *smbPool
}

var mgr = &Manager{conns: map[string]Connection{}, pool: newSMBPool()}

func SetConnections(cs []Connection) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	mgr.conns = make(map[string]Connection, len(cs))
	for _, c := range cs {
		mgr.conns[c.Name] = c
	}
	mgr.pool.prune(mgr.conns)
}

func Resolve(vpath string) (*Binding, error) {
	if vpath == "" {
		vpath = "C:\\"
	}
	if strings.HasPrefix(vpath, "smb://") {
		rest := strings.TrimPrefix(vpath, "smb://")
		name := rest
		rel := ""
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			name = rest[:i]
			rel = rest[i+1:]
		}
		name = strings.TrimSpace(name)
		mgr.mu.RLock()
		conn, ok := mgr.conns[name]
		mgr.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("找不到 SMB 連線「%s」", name)
		}
		return &Binding{FS: &smbFS{conn: conn, pool: mgr.pool}, Path: rel, Kind: "smb"}, nil
	}
	return &Binding{FS: localFS{}, Path: filepath.Clean(vpath), Kind: "local"}, nil
}

func ParentOf(vpath string) string {
	if vpath == "" {
		return ""
	}
	if strings.HasPrefix(vpath, "smb://") {
		rest := strings.TrimPrefix(vpath, "smb://")
		if !strings.Contains(rest, "/") {
			return ""
		}
		trimmed := strings.TrimRight(vpath, "/")
		i := strings.LastIndexByte(trimmed, '/')
		if i < 6 {
			return ""
		}
		return trimmed[:i]
	}
	trimmed := strings.TrimRight(vpath, `\/`)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) == 2 && trimmed[1] == ':' {
		return ""
	}
	if strings.HasPrefix(trimmed, `\\`) {
		parts := strings.Split(strings.TrimPrefix(trimmed, `\\`), `\`)
		if len(parts) <= 2 {
			return ""
		}
		return strings.TrimSuffix(filepath.Dir(trimmed), `\`)
	}
	if strings.HasPrefix(trimmed, `//`) {
		parts := strings.Split(strings.TrimPrefix(trimmed, `//`), `/`)
		if len(parts) <= 2 {
			return ""
		}
		trimmed = strings.ReplaceAll(trimmed, "/", `\`)
		return filepath.Dir(trimmed)
	}
	parent := filepath.Dir(trimmed)
	if parent == trimmed {
		return ""
	}
	return parent
}

var thumbExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".jpe": true, ".jfif": true, ".png": true,
	".gif": true, ".bmp": true, ".webp": true, ".tif": true, ".tiff": true,
}

func CanThumb(name string) bool {
	return thumbExts[strings.ToLower(filepath.Ext(name))]
}

func MimeFor(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if m := mime.TypeByExtension(ext); m != "" {
		return m
	}
	switch ext {
	case ".heic", ".heif":
		return "image/heic"
	case ".svg":
		return "image/svg+xml"
	case ".avif":
		return "image/avif"
	case ".webp":
		return "image/webp"
	case ".tif", ".tiff":
		return "image/tiff"
	case ".mkv":
		return "video/x-matroska"
	}
	return "application/octet-stream"
}
