package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"macbrowser/internal/config"
	"macbrowser/internal/thumbs"
	"macbrowser/internal/vfs"
)

//go:embed all:web
var webAssets embed.FS

type Server struct {
	cfg    *config.Config
	thumbs *thumbs.Cache
	webDir string

	labelMu    sync.Mutex
	labelCache map[string]labelCacheItem
}

type labelCacheItem struct {
	label *vfs.Label
	at    time.Time
}

func New(cfg *config.Config, tc *thumbs.Cache, webDir string) *Server {
	return &Server{cfg: cfg, thumbs: tc, webDir: webDir, labelCache: map[string]labelCacheItem{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.api)
	var sub fs.FS
	var err error
	if s.webDir != "" {
		sub = os.DirFS(s.webDir)
	} else {
		sub, err = fs.Sub(webAssets, "web")
		if err != nil {
			panic(err)
		}
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return mux
}

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("api panic %s: %v", r.URL.Path, rec)
			writeJSONCode(w, http.StatusInternalServerError, map[string]string{"error": "內部錯誤"})
		}
	}()
	switch r.URL.Path {
	case "/api/roots":
		s.handleRoots(w, r)
	case "/api/config":
		s.handleConfig(w, r)
	case "/api/connections":
		s.handleConnections(w, r)
	case "/api/list":
		s.handleList(w, r)
	case "/api/labels":
		s.handleLabels(w, r)
	case "/api/thumb":
		s.handleThumb(w, r)
	case "/api/file":
		s.handleFile(w, r)
	case "/api/find":
		s.handleFind(w, r)
	case "/api/open":
		s.handleOpen(w, r)
	case "/api/openwith":
		s.handleOpenWith(w, r)
	case "/api/pasteos":
		s.handlePasteOS(w, r)
	case "/api/copyos":
		s.handleCopyOS(w, r)
	case "/api/clipseq":
		s.handleClipSeq(w, r)
	case "/api/reveal":
		s.handleReveal(w, r)
	case "/api/mkdir":
		s.handleMkdir(w, r)
	case "/api/rename":
		s.handleRename(w, r)
	case "/api/delete":
		s.handleDelete(w, r)
	case "/api/copy":
		s.handleCopy(w, r)
	case "/api/upload":
		s.handleUpload(w, r)
	case "/api/download":
		s.handleDownload(w, r)
	default:
		writeJSONCode(w, http.StatusNotFound, map[string]string{"error": "未知的 API 路徑"})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONCode(w, http.StatusOK, v)
}

func writeJSONCode(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSONCode(w, code, map[string]string{"error": err.Error()})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSONCode(w, http.StatusBadRequest, map[string]string{"error": "請求格式錯誤：" + err.Error()})
		return false
	}
	return true
}
