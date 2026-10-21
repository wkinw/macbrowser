package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"macbrowser/internal/vfs"
)

type Prefs struct {
	TileSize     int    `json:"tileSize"`
	SortKey      string `json:"sortKey"`
	SortDir      string `json:"sortDir"`
	ShowHidden   bool   `json:"showHidden"`
	ShowLabels   bool   `json:"showLabels"`
	FoldersFirst bool   `json:"foldersFirst"`
	HoverDelay   int    `json:"hoverDelay"`
}

type Config struct {
	Connections []vfs.Connection `json:"connections"`
	LastPath    string           `json:"lastPath"`
	Prefs       Prefs            `json:"prefs"`

	mu   sync.Mutex
	path string
}

func defaultPrefs() Prefs {
	return Prefs{
		TileSize:     168,
		SortKey:      "name",
		SortDir:      "asc",
		ShowLabels:   true,
		FoldersFirst: true,
		HoverDelay:   500,
	}
}

func normalizePrefs(p *Prefs) {
	def := defaultPrefs()
	if p.TileSize <= 0 {
		p.TileSize = def.TileSize
	}
	if p.SortKey == "" {
		p.SortKey = "name"
	}
	if p.SortDir == "" {
		p.SortDir = "asc"
	}
	if p.HoverDelay <= 0 {
		p.HoverDelay = def.HoverDelay
	}
	if p.HoverDelay > 60000 {
		p.HoverDelay = 60000
	}
}

func Load(dir string) (*Config, error) {
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return nil, err
	}
	c := &Config{path: filepath.Join(dir, "config.json"), Prefs: defaultPrefs()}
	data, err := os.ReadFile(c.path)
	if err == nil {
		if err := json.Unmarshal(data, c); err != nil {
			c.Connections = nil
			c.Prefs = defaultPrefs()
		}
	}
	normalizePrefs(&c.Prefs)
	return c, nil
}

func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *Config) ConnectionsSnapshot() []vfs.Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]vfs.Connection, len(c.Connections))
	copy(out, c.Connections)
	return out
}

func (c *Config) UpsertConnection(conn vfs.Connection) error {
	c.mu.Lock()
	found := false
	for i := range c.Connections {
		if c.Connections[i].Name == conn.Name {
			c.Connections[i] = conn
			found = true
			break
		}
	}
	if !found {
		c.Connections = append(c.Connections, conn)
	}
	c.mu.Unlock()
	return c.Save()
}

func (c *Config) RemoveConnection(name string) error {
	c.mu.Lock()
	out := c.Connections[:0]
	for _, x := range c.Connections {
		if x.Name != name {
			out = append(out, x)
		}
	}
	c.Connections = out
	c.mu.Unlock()
	return c.Save()
}

func (c *Config) SetLastPath(p string) {
	c.mu.Lock()
	c.LastPath = p
	c.mu.Unlock()
}

func (c *Config) SetPrefs(p Prefs) {
	normalizePrefs(&p)
	c.mu.Lock()
	c.Prefs = p
	c.mu.Unlock()
}
