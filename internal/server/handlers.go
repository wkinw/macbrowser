package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"macbrowser/internal/config"
	"macbrowser/internal/exif"
	"macbrowser/internal/macmeta"
	"macbrowser/internal/thumbs"
	"macbrowser/internal/vfs"
)

func joinVPath(base, name string) string {
	if base == "" {
		return name
	}
	if strings.HasPrefix(base, "smb://") {
		return strings.TrimRight(base, "/") + "/" + name
	}
	return filepath.Join(base, name)
}

func baseNameOf(vpath string) string {
	trimmed := strings.TrimRight(vpath, `\/`)
	if i := strings.LastIndexAny(trimmed, `\/`); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}

func (s *Server) handleRoots(w http.ResponseWriter, r *http.Request) {
	conns := s.cfg.ConnectionsSnapshot()
	type connItem struct {
		Name string `json:"name"`
		Desc string `json:"desc"`
		Path string `json:"path"`
	}
	items := make([]connItem, 0, len(conns))
	for _, c := range conns {
		desc := c.Host
		if c.User != "" {
			desc = c.User + "@" + c.Host
		}
		if c.Share != "" {
			desc += "/" + c.Share
		}
		items = append(items, connItem{Name: c.Name, Desc: desc, Path: "smb://" + c.Name})
	}
	def := s.cfg.LastPath
	writeJSON(w, map[string]any{
		"places":      vfs.LocalPlaces(),
		"drives":      vfs.LocalDrives(),
		"connections": items,
		"default":     def,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"connections": s.cfg.ConnectionsSnapshot(),
			"lastPath":    s.cfg.LastPath,
			"prefs":       s.cfg.Prefs,
		})
	case http.MethodPost:
		var payload struct {
			LastPath *string       `json:"lastPath"`
			Prefs    *config.Prefs `json:"prefs"`
		}
		if !decodeBody(w, r, &payload) {
			return
		}
		if payload.LastPath != nil {
			s.cfg.SetLastPath(*payload.LastPath)
		}
		if payload.Prefs != nil {
			s.cfg.SetPrefs(*payload.Prefs)
		}
		if err := s.cfg.Save(); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})
	default:
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		Action string         `json:"action"`
		Conn   vfs.Connection `json:"connection"`
	}
	if !decodeBody(w, r, &payload) {
		return
	}
	switch payload.Action {
	case "save":
		c := payload.Conn
		c.Name = strings.TrimSpace(c.Name)
		c.Host = strings.TrimSpace(c.Host)
		c.Share = strings.TrimSpace(c.Share)
		c.Base = strings.Trim(strings.TrimSpace(c.Base), `\/`)
		c.User = strings.TrimSpace(c.User)
		c.Domain = strings.TrimSpace(c.Domain)
		if c.Name == "" || c.Host == "" {
			writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "名稱與主機不可為空"})
			return
		}
		if strings.ContainsAny(c.Name, `/\:`) {
			writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "名稱不可包含 / \\ : 字元"})
			return
		}
		if err := s.cfg.UpsertConnection(c); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	case "delete":
		if err := s.cfg.RemoveConnection(payload.Conn.Name); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	default:
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "未知的操作"})
		return
	}
	vfs.SetConnections(s.cfg.ConnectionsSnapshot())
	s.labelMu.Lock()
	s.labelCache = map[string]labelCacheItem{}
	s.labelMu.Unlock()
	writeJSON(w, map[string]any{"connections": s.cfg.ConnectionsSnapshot()})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	b, err := vfs.Resolve(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	entries, err := b.FS.List(b.Path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"path":    p,
		"kind":    b.Kind,
		"parent":  vfs.ParentOf(p),
		"entries": entries,
	})
}

func (s *Server) handleLabels(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleSetLabels(w, r)
		return
	}
	p := r.URL.Query().Get("p")
	b, err := vfs.Resolve(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	entries, err := b.FS.List(b.Path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	const maxScan = 6000
	truncated := false
	if len(entries) > maxScan {
		entries = entries[:maxScan]
		truncated = true
	}
	hasAD := map[string]bool{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name, "._") {
			hasAD[e.Name[2:]] = true
		}
	}
	labels := map[string]*vfs.Label{}
	ratings := map[string]int{}
	var mu sync.Mutex
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, e := range entries {
		if strings.HasPrefix(e.Name, "._") || e.Name == ".DS_Store" {
			continue
		}
		e := e
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() {
				<-sem
				wg.Done()
			}()
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("label panic %s: %v", e.Name, rec)
				}
			}()
			vpath := joinVPath(p, e.Name)
			label := s.labelFor(vpath, b, e.Name, hasAD[e.Name])
			rating := 0
			if !e.Dir {
				var found bool
				rating, found = macmeta.ResolveRating(b.FS, b.Path, e.Name, hasAD[e.Name])
				if !found && vfs.CanThumb(e.Name) {
					rating = s.contentRating(b.FS, b.FS.Join(b.Path, e.Name))
				}
			}
			if label != nil || rating > 0 {
				mu.Lock()
				if label != nil {
					labels[e.Name] = label
				}
				if rating > 0 {
					ratings[e.Name] = rating
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	writeJSON(w, map[string]any{
		"labels":    labels,
		"ratings":   ratings,
		"total":     len(entries),
		"truncated": truncated,
	})
}

func (s *Server) splitBindingPath(b *vfs.Binding) (string, string) {
	return splitByKind(b.Kind, b.Path)
}

func (s *Server) handleSetLabels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
		Color int      `json:"color"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("請求格式錯誤：%w", err))
		return
	}
	if len(req.Paths) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("缺少 paths 參數"))
		return
	}
	if len(req.Paths) > 2000 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("一次最多設定 2000 個項目"))
		return
	}
	if req.Color < 0 || req.Color > 7 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("無效的顏色編號 %d", req.Color))
		return
	}
	var errs []string
	updated := 0
	for _, p := range req.Paths {
		if p == "" {
			continue
		}
		b, err := vfs.Resolve(p)
		if err != nil {
			errs = append(errs, p+"："+err.Error())
			continue
		}
		dir, name := s.splitBindingPath(b)
		if name == "" {
			errs = append(errs, p+"：無效的路徑")
			continue
		}
		isDir := false
		if st, err := b.FS.Stat(b.Path); err == nil {
			isDir = st.Dir
		}
		if err := macmeta.SetLabelNode(b.FS, dir, name, req.Color, isDir); err != nil {
			errs = append(errs, name+"："+err.Error())
			continue
		}
		s.invalidateLabel(p)
		updated++
	}
	writeJSON(w, map[string]any{"ok": len(errs) == 0, "updated": updated, "errors": errs})
}

func (s *Server) handleRatings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "僅支援 POST"})
		return
	}
	var req struct {
		Paths []string `json:"paths"`
		Stars int      `json:"stars"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("請求格式錯誤：%w", err))
		return
	}
	if len(req.Paths) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("缺少 paths 參數"))
		return
	}
	if len(req.Paths) > 2000 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("一次最多設定 2000 個項目"))
		return
	}
	if req.Stars < 0 || req.Stars > 5 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("無效的星等 %d", req.Stars))
		return
	}
	var errs []string
	updated := 0
	for _, p := range req.Paths {
		if p == "" {
			continue
		}
		b, err := vfs.Resolve(p)
		if err != nil {
			errs = append(errs, p+"："+err.Error())
			continue
		}
		dir, name := s.splitBindingPath(b)
		if name == "" {
			errs = append(errs, p+"：無效的路徑")
			continue
		}
		if st, err := b.FS.Stat(b.Path); err == nil && st.Dir {
			errs = append(errs, name+"：資料夾不支援評等")
			continue
		}
		if err := macmeta.SetRating(b.FS, dir, name, req.Stars); err != nil {
			errs = append(errs, name+"："+err.Error())
			continue
		}
		updated++
	}
	writeJSON(w, map[string]any{"ok": len(errs) == 0, "updated": updated, "errors": errs})
}

func (s *Server) contentRating(fs vfs.FS, path string) int {
	f, err := fs.Open(path)
	if err != nil {
		return 0
	}
	buf, err := io.ReadAll(io.LimitReader(f, 256<<10))
	f.Close()
	if err != nil {
		return 0
	}
	return exif.Rating(buf)
}

func (s *Server) handlePhotoTime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "僅支援 POST"})
		return
	}
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("請求格式錯誤：%w", err))
		return
	}
	if len(req.Paths) > 500 {
		req.Paths = req.Paths[:500]
	}
	times := map[string]string{}
	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, p := range req.Paths {
		if p == "" {
			continue
		}
		p := p
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() {
				<-sem
				wg.Done()
			}()
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("phototime panic %s: %v", p, rec)
				}
			}()
			b, err := vfs.Resolve(p)
			if err != nil {
				return
			}
			st, err := b.FS.Stat(b.Path)
			if err != nil || st.Dir {
				return
			}
			f, err := b.FS.Open(b.Path)
			if err != nil {
				return
			}
			buf, rerr := io.ReadAll(io.LimitReader(f, 512*1024))
			f.Close()
			if rerr != nil {
				return
			}
			if t := exif.Taken(buf); t != "" {
				mu.Lock()
				times[p] = t
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	writeJSON(w, map[string]any{"times": times})
}

func (s *Server) labelFor(vpath string, b *vfs.Binding, name string, hasAD bool) *vfs.Label {
	s.labelMu.Lock()
	if it, ok := s.labelCache[vpath]; ok && time.Since(it.at) < 5*time.Minute {
		s.labelMu.Unlock()
		return it.label
	}
	s.labelMu.Unlock()
	label := macmeta.Resolve(b.FS, b.Path, name, hasAD)
	s.labelMu.Lock()
	if len(s.labelCache) > 50000 {
		s.labelCache = map[string]labelCacheItem{}
	}
	s.labelCache[vpath] = labelCacheItem{label: label, at: time.Now()}
	s.labelMu.Unlock()
	return label
}

func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := q.Get("p")
	if p == "" {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "缺少 p 參數"})
		return
	}
	size, _ := strconv.Atoi(q.Get("s"))
	if size <= 0 {
		size = 256
	}
	if size < 64 {
		size = 64
	}
	if size > 1600 {
		size = 1600
	}
	b, err := vfs.Resolve(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	m := q.Get("m")
	z := q.Get("z")
	st, statErr := b.FS.Stat(b.Path)
	if statErr != nil {
		writeErr(w, http.StatusNotFound, statErr)
		return
	}
	if m == "" || z == "" {
		m = strconv.FormatInt(st.Mod.UnixNano(), 10)
		z = strconv.FormatInt(st.Size, 10)
	}
	key := thumbs.Key(p, m, z, strconv.Itoa(size))
	etag := `"` + key + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	data, err := s.thumbs.Get(key, func() ([]byte, error) {
		if st.Dir {
			targets := findPreviewImages(b.FS, b.Path, 4)
			if len(targets) == 0 {
				return nil, thumbs.ErrNoPreview
			}
			images := make([][]byte, 0, len(targets))
			for _, target := range targets {
				f, err := b.FS.Open(target)
				if err != nil {
					continue
				}
				raw, rerr := io.ReadAll(io.LimitReader(f, 128<<20))
				f.Close()
				if rerr == nil && len(raw) > 0 {
					images = append(images, raw)
				}
			}
			return thumbs.RenderFolder(images, size)
		}
		f, err := b.FS.Open(b.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return thumbs.Render(f, size)
	})
	if err != nil {
		writeJSONCode(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	w.Write(data)
}

func findPreviewImages(fs vfs.FS, dir string, limit int) []string {
	if limit <= 0 {
		limit = 1
	}
	type job struct {
		p     string
		depth int
	}
	queue := []job{{p: dir}}
	var out []string
	dirs, files := 0, 0
	for len(queue) > 0 && len(out) < limit && dirs < 40 && files < 400 {
		it := queue[0]
		queue = queue[1:]
		dirs++
		entries, err := fs.List(it.p)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Hidden || strings.HasPrefix(e.Name, "._") {
				continue
			}
			if e.Dir {
				if it.depth < 2 {
					queue = append(queue, job{p: fs.Join(it.p, e.Name), depth: it.depth + 1})
				}
				continue
			}
			files++
			if vfs.CanThumb(e.Name) {
				out = append(out, fs.Join(it.p, e.Name))
				if len(out) >= limit {
					return out
				}
			}
			if files >= 400 {
				break
			}
		}
	}
	return out
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "缺少 p 參數"})
		return
	}
	b, err := vfs.Resolve(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	f, err := b.FS.Open(b.Path)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	defer f.Close()
	st, err := b.FS.Stat(b.Path)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	name := baseNameOf(p)
	w.Header().Set("Content-Type", vfs.MimeFor(name))
	http.ServeContent(w, r, name, st.Mod, f)
}

func (s *Server) handleFind(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	root := q.Get("p")
	keyword := strings.TrimSpace(q.Get("q"))
	if keyword == "" {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "缺少搜尋關鍵字"})
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 5000 {
		limit = 2000
	}
	b, err := vfs.Resolve(root)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	lower := strings.ToLower(keyword)
	type job struct {
		dirV  string
		dirN  string
		depth int
	}
	stack := []job{{dirV: root, dirN: b.Path, depth: 0}}
	const maxDepth = 8
	const maxScan = 150000
	results := []vfs.Entry{}
	scanned := 0
	for len(stack) > 0 && len(results) < limit && scanned < maxScan {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		list, err := b.FS.List(it.dirN)
		if err != nil {
			continue
		}
		for _, e := range list {
			scanned++
			if strings.HasPrefix(e.Name, "._") || e.Name == ".DS_Store" {
				continue
			}
			if e.Hidden && e.Dir {
				continue
			}
			match := strings.Contains(strings.ToLower(e.Name), lower)
			if match {
				e.Path = joinVPath(it.dirV, e.Name)
				label := s.labelFor(e.Path, bindingAt(b, it.dirN), e.Name, false)
				e.Label = label
				results = append(results, e)
				if len(results) >= limit {
					break
				}
			}
			if e.Dir && it.depth < maxDepth {
				stack = append(stack, job{
					dirV:  joinVPath(it.dirV, e.Name),
					dirN:  b.FS.Join(it.dirN, e.Name),
					depth: it.depth + 1,
				})
			}
		}
	}
	writeJSON(w, map[string]any{"entries": results, "scanned": scanned})
}

func bindingAt(b *vfs.Binding, dir string) *vfs.Binding {
	child := *b
	child.Path = dir
	return &child
}

func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		P string `json:"p"`
	}
	if !decodeBody(w, r, &payload) {
		return
	}
	b, err := vfs.Resolve(payload.P)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if b.Kind == "smb" {
		local, err := s.materialize(payload.P, b)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if err := shellOpen(local); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]string{"path": local})
		return
	}
	if err := shellOpen(payload.P); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]string{"path": payload.P})
}

func (s *Server) materialize(vpath string, b *vfs.Binding) (string, error) {
	name := baseNameOf(vpath)
	dir := filepath.Join(os.TempDir(), "macbrowser")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return "", err
	}
	local := filepath.Join(dir, name)
	remote, err := b.FS.Stat(b.Path)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(local); err == nil && st.Size() == remote.Size && !st.ModTime().Before(remote.Mod) {
		return local, nil
	}
	f, err := b.FS.Open(b.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	tmp := local + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, f); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, local); err != nil {
		os.Remove(tmp)
		return "", err
	}
	os.Chtimes(local, remote.Mod, remote.Mod)
	return local, nil
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		P string `json:"p"`
	}
	if !decodeBody(w, r, &payload) {
		return
	}
	if strings.HasPrefix(payload.P, "smb://") {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "僅支援本機檔案"})
		return
	}
	if err := revealInExplorer(payload.P); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleOpenWith(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		P string `json:"p"`
	}
	if !decodeBody(w, r, &payload) {
		return
	}
	b, err := vfs.Resolve(payload.P)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	st, err := b.FS.Stat(b.Path)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if st.Dir {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "資料夾不支援「開啟方式」"})
		return
	}
	local := payload.P
	if b.Kind == "smb" {
		local, err = s.materialize(payload.P, b)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if err := openWith(local); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]string{"path": local})
}

func (s *Server) handleClipSeq(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"seq": clipboardSeq()})
}

func (s *Server) handleCopyOS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		Paths []string `json:"paths"`
		Move  bool     `json:"move"`
	}
	if !decodeBody(w, r, &payload) {
		return
	}
	if len(payload.Paths) == 0 {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "缺少 paths"})
		return
	}
	if len(payload.Paths) > 2000 {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "一次最多 2000 項"})
		return
	}
	cleanupOldClips()
	local := make([]string, 0, len(payload.Paths))
	allLocal := true
	var errs []string
	var clipDir string
	for _, p := range payload.Paths {
		if p == "" {
			continue
		}
		b, err := vfs.Resolve(p)
		if err != nil {
			errs = append(errs, p+"："+err.Error())
			continue
		}
		if b.Kind == "local" {
			local = append(local, b.Path)
			continue
		}
		allLocal = false
		if clipDir == "" {
			clipDir = filepath.Join(os.TempDir(), "macbrowser-clip", strconv.FormatInt(time.Now().UnixNano(), 10))
			if err := os.MkdirAll(clipDir, 0o777); err != nil {
				errs = append(errs, p+"："+err.Error())
				continue
			}
		}
		_, name := splitByKind(b.Kind, b.Path)
		if name == "" {
			errs = append(errs, p+"：無效的路徑")
			continue
		}
		lb, err := vfs.Resolve(clipDir)
		if err != nil {
			errs = append(errs, p+"："+err.Error())
			continue
		}
		name = uniqueDst(lb.FS, lb.Path, name)
		if err := s.copyAny(b, b.Path, lb, lb.FS.Join(lb.Path, name)); err != nil {
			errs = append(errs, name+"："+err.Error())
			continue
		}
		local = append(local, filepath.Join(clipDir, name))
	}
	if len(local) == 0 {
		if clipDir != "" {
			os.RemoveAll(clipDir)
		}
		writeJSONCode(w, http.StatusBadRequest, map[string]any{"ok": false, "copied": 0, "errors": errs})
		return
	}
	move := payload.Move && allLocal
	if err := setClipboardFiles(local, move); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":           len(errs) == 0,
		"copied":       len(local),
		"errors":       errs,
		"seq":          clipboardSeq(),
		"materialized": allLocal == false,
	})
}

func cleanupOldClips() {
	base := filepath.Join(os.TempDir(), "macbrowser-clip")
	des, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, de := range des {
		info, err := de.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > 24*time.Hour {
			os.RemoveAll(filepath.Join(base, de.Name()))
		}
	}
}

func (s *Server) handlePasteOS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		Dest string `json:"dest"`
	}
	if !decodeBody(w, r, &payload) {
		return
	}
	db, err := vfs.Resolve(payload.Dest)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if st, err := db.FS.Stat(db.Path); err != nil || !st.Dir {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "目的不是資料夾"})
		return
	}
	files, err := clipboardFiles()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if len(files) == 0 {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "剪貼簿沒有檔案"})
		return
	}
	sep := string(filepath.Separator)
	destLower := strings.ToLower(db.Path + sep)
	var errs []string
	copied := 0
	for _, f := range files {
		sb, err := vfs.Resolve(f)
		if err != nil {
			errs = append(errs, f+"："+err.Error())
			continue
		}
		st, err := sb.FS.Stat(sb.Path)
		if err != nil {
			errs = append(errs, f+"："+err.Error())
			continue
		}
		name := filepath.Base(strings.TrimRight(sb.Path, `\/`))
		if name == "" {
			errs = append(errs, f+"：名稱無效")
			continue
		}
		if st.Dir && db.Kind == "local" {
			srcLower := strings.ToLower(sb.Path + sep)
			if strings.HasPrefix(destLower, srcLower) {
				errs = append(errs, name+"：不能複製到資料夾自身內")
				continue
			}
		}
		np := uniqueDst(db.FS, db.Path, name)
		if err := s.copyAny(sb, sb.Path, db, db.FS.Join(db.Path, np)); err != nil {
			errs = append(errs, name+"："+err.Error())
			continue
		}
		copied++
	}
	writeJSON(w, map[string]any{"ok": len(errs) == 0, "copied": copied, "errors": errs})
}

func validName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("名稱無效")
	}
	if strings.ContainsAny(name, `/\:*?"<>|`) {
		return fmt.Errorf(`名稱不可包含 \ / : * ? " < > |`)
	}
	return nil
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		P    string `json:"p"`
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &payload) {
		return
	}
	if err := validName(payload.Name); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	b, err := vfs.Resolve(payload.P)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := b.FS.Mkdir(b.Child(payload.Name)); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		P    string `json:"p"`
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &payload) {
		return
	}
	if err := validName(payload.Name); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	parent := vfs.ParentOf(payload.P)
	if parent == "" {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "無法重新命名此位置"})
		return
	}
	b, err := vfs.Resolve(parent)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	old := b.Child(baseNameOf(payload.P))
	if err := b.FS.Rename(old, b.Child(payload.Name)); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.invalidateLabel(payload.P)
	s.invalidateLabel(joinVPath(parent, payload.Name))
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONCode(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		Paths []string `json:"paths"`
	}
	if !decodeBody(w, r, &payload) {
		return
	}
	if len(payload.Paths) == 0 {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "未指定要刪除的項目"})
		return
	}
	var errs []string
	for _, p := range payload.Paths {
		b, err := vfs.Resolve(p)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s：%v", p, err))
			continue
		}
		if err := b.FS.Remove(b.Path, true); err != nil {
			errs = append(errs, fmt.Sprintf("%s：%v", p, err))
			continue
		}
		s.invalidateLabel(p)
	}
	if len(errs) > 0 {
		writeJSONCode(w, http.StatusInternalServerError, map[string]any{"error": strings.Join(errs, "\n"), "errors": errs})
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) invalidateLabel(vpath string) {
	s.labelMu.Lock()
	delete(s.labelCache, vpath)
	s.labelMu.Unlock()
}
