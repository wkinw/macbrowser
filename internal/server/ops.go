package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"macbrowser/internal/macmeta"
	"macbrowser/internal/vfs"
)

func splitByKind(kind, p string) (string, string) {
	if kind == "smb" {
		p = strings.TrimRight(p, "/")
		if i := strings.LastIndexByte(p, '/'); i >= 0 {
			return p[:i], p[i+1:]
		}
		return "", p
	}
	return filepath.Dir(p), filepath.Base(p)
}

func existsB(fs vfs.FS, p string) bool {
	_, err := fs.Stat(p)
	return err == nil
}

func uniqueDst(fs vfs.FS, dir, name string) string {
	if !existsB(fs, fs.Join(dir, name)) {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; i < 1000; i++ {
		c := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if !existsB(fs, fs.Join(dir, c)) {
			return c
		}
	}
	return fmt.Sprintf("%s (%d)%s", stem, time.Now().Unix(), ext)
}

func (s *Server) moveSidecar(fs vfs.FS, src, dst string) {
	if !existsB(fs, src) {
		return
	}
	if err := fs.Rename(src, dst); err == nil {
		return
	}
	r, err := fs.Open(src)
	if err != nil {
		return
	}
	defer r.Close()
	if err := fs.WriteFrom(dst, r); err == nil {
		_ = fs.Remove(src, false)
	}
}

func (s *Server) copyMeta(sb *vfs.Binding, sp string, db *vfs.Binding, dp string) {
	for _, xn := range []string{macmeta.UserTagsAttr, macmeta.FinderInfoAttr} {
		if raw, err := sb.FS.Xattr(sp, xn); err == nil {
			_ = db.FS.WriteXattr(dp, xn, raw)
		}
	}
	sd, sn := splitByKind(sb.Kind, sp)
	dd, dn := splitByKind(db.Kind, dp)
	if sn == "" {
		return
	}
	ad := sb.FS.Join(sd, "._"+sn)
	if !existsB(sb.FS, ad) {
		return
	}
	r, err := sb.FS.Open(ad)
	if err != nil {
		return
	}
	defer r.Close()
	_ = db.FS.WriteFrom(db.FS.Join(dd, "._"+dn), r)
}

func (s *Server) copyAny(sb *vfs.Binding, sp string, db *vfs.Binding, dp string) error {
	st, err := sb.FS.Stat(sp)
	if err != nil {
		return err
	}
	if st.Dir {
		if err := db.FS.Mkdir(dp); err != nil {
			if st2, e2 := db.FS.Stat(dp); e2 != nil || !st2.Dir {
				return err
			}
		}
		entries, err := sb.FS.List(sp)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := s.copyAny(sb, sb.FS.Join(sp, e.Name), db, db.FS.Join(dp, e.Name)); err != nil {
				return fmt.Errorf("%s：%w", e.Name, err)
			}
		}
		return nil
	}
	r, err := sb.FS.Open(sp)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := db.FS.WriteFrom(dp, r); err != nil {
		return err
	}
	s.copyMeta(sb, sp, db, dp)
	return nil
}

func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("僅支援 POST"))
		return
	}
	var req struct {
		Paths []string `json:"paths"`
		Dest  string   `json:"dest"`
		Move  bool     `json:"move"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if len(req.Paths) == 0 || req.Dest == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("缺少 paths 或 dest"))
		return
	}
	if len(req.Paths) > 2000 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("一次最多 2000 項"))
		return
	}
	db, err := vfs.Resolve(req.Dest)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if st, err := db.FS.Stat(db.Path); err != nil || !st.Dir {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("目的不是資料夾"))
		return
	}
	var errs []string
	done := 0
	for _, p := range req.Paths {
		if p == "" {
			continue
		}
		sb, err := vfs.Resolve(p)
		if err != nil {
			errs = append(errs, p+"："+err.Error())
			continue
		}
		sd, sn := splitByKind(sb.Kind, sb.Path)
		if sn == "" {
			errs = append(errs, p+"：無效的來源")
			continue
		}
		sp := sb.FS.Join(sd, sn)
		sameDir := sb.Kind == db.Kind && sd == db.Path
		if req.Move && sameDir {
			errs = append(errs, sn+"：來源與目的相同")
			continue
		}
		if req.Move {
			np := db.FS.Join(db.Path, sn)
			if !existsB(db.FS, np) && sb.Kind == db.Kind {
				if err := sb.FS.Rename(sp, np); err == nil {
					s.moveSidecar(sb.FS, sb.FS.Join(sd, "._"+sn), db.FS.Join(db.Path, "._"+sn))
					s.invalidateLabel(p)
					done++
					continue
				}
			}
		}
		name := uniqueDst(db.FS, db.Path, sn)
		np := db.FS.Join(db.Path, name)
		if err := s.copyAny(sb, sp, db, np); err != nil {
			errs = append(errs, sn+"："+err.Error())
			continue
		}
		if req.Move {
			if err := sb.FS.Remove(sp, true); err != nil {
				errs = append(errs, sn+"：複製成功但刪除來源失敗："+err.Error())
				continue
			}
			adS := sb.FS.Join(sd, "._"+sn)
			if existsB(db.FS, db.FS.Join(db.Path, "._"+name)) {
				_ = sb.FS.Remove(adS, false)
			}
			s.invalidateLabel(p)
		}
		done++
	}
	writeJSON(w, map[string]any{"ok": len(errs) == 0, "copied": done, "errors": errs})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("僅支援 POST"))
		return
	}
	p := r.URL.Query().Get("p")
	if p == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("缺少 p 參數"))
		return
	}
	b, err := vfs.Resolve(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if st, err := b.FS.Stat(b.Path); err != nil || !st.Dir {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("目的不是資料夾"))
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("請求格式錯誤：%w", err))
		return
	}
	var errs []string
	count := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			errs = append(errs, "讀取中斷："+err.Error())
			break
		}
		name := part.FileName()
		if name == "" {
			part.Close()
			continue
		}
		rel := ""
		if fn := part.FormName(); strings.HasPrefix(fn, "up:") {
			rel = fn[3:]
		}
		segs := []string{}
		bad := false
		for _, seg := range strings.Split(strings.ReplaceAll(rel, "\\", "/"), "/") {
			if seg == "" || seg == "." {
				continue
			}
			if seg == ".." {
				bad = true
				break
			}
			segs = append(segs, seg)
		}
		if bad {
			part.Close()
			errs = append(errs, rel+name+"：無效的路徑")
			continue
		}
		cur := b.Path
		for _, seg := range segs {
			cur = b.FS.Join(cur, seg)
			_ = b.FS.Mkdir(cur)
		}
		dst := b.FS.Join(cur, name)
		if err := b.FS.WriteFrom(dst, part); err != nil {
			errs = append(errs, strings.Join(append(segs, name), "/")+"："+err.Error())
		} else {
			count++
		}
		part.Close()
	}
	writeJSON(w, map[string]any{"ok": len(errs) == 0, "uploaded": count, "errors": errs})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("僅支援 POST"))
		return
	}
	var req struct {
		Paths []string `json:"paths"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if len(req.Paths) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("缺少 paths"))
		return
	}
	if len(req.Paths) > 2000 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("一次最多 2000 項"))
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("找不到使用者資料夾：%w", err))
		return
	}
	dl := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dl, 0o777); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	db, err := vfs.Resolve(dl)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var errs []string
	var out []string
	for _, p := range req.Paths {
		if p == "" {
			continue
		}
		sb, err := vfs.Resolve(p)
		if err != nil {
			errs = append(errs, p+"："+err.Error())
			continue
		}
		sd, sn := splitByKind(sb.Kind, sb.Path)
		if sn == "" {
			errs = append(errs, p+"：無效的來源")
			continue
		}
		name := uniqueDst(db.FS, db.Path, sn)
		np := db.FS.Join(db.Path, name)
		if err := s.copyAny(sb, sb.FS.Join(sd, sn), db, np); err != nil {
			errs = append(errs, sn+"："+err.Error())
			continue
		}
		out = append(out, np)
	}
	writeJSON(w, map[string]any{"ok": len(errs) == 0, "dir": dl, "paths": out, "errors": errs})
}
