package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"macbrowser/internal/config"
	"macbrowser/internal/thumbs"
	"macbrowser/internal/vfs"
)

//go:embed all:web
var webAssets embed.FS

func cookieNameForToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "mb_auth_" + hex.EncodeToString(sum[:8])
}

type Server struct {
	cfg    *config.Config
	thumbs *thumbs.Cache
	webDir string

	authToken string

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

// SetAuthToken enables access-token authentication. An empty token disables it.
func (s *Server) SetAuthToken(token string) {
	s.authToken = token
}

func equalToken(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func bearerToken(h string) string {
	const prefix = "bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

func (s *Server) denyAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="macbrowser"`)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSONCode(w, http.StatusUnauthorized, map[string]string{"error": "需要存取權杖"})
		return
	}
	http.Error(w, "需要存取權杖", http.StatusUnauthorized)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.authToken
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		if q := r.URL.Query().Get("token"); q != "" {
			if !equalToken(q, token) {
				s.denyAuth(w, r)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     cookieNameForToken(token),
				Value:    base64.RawURLEncoding.EncodeToString([]byte(token)),
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			u := *r.URL
			query := u.Query()
			query.Del("token")
			u.RawQuery = query.Encode()
			http.Redirect(w, r, u.RequestURI(), http.StatusSeeOther)
			return
		}
		if c, err := r.Cookie(cookieNameForToken(token)); err == nil {
			if raw, err := base64.RawURLEncoding.DecodeString(c.Value); err == nil && equalToken(string(raw), token) {
				next.ServeHTTP(w, r)
				return
			}
		}
		if b := bearerToken(r.Header.Get("Authorization")); b != "" && equalToken(b, token) {
			next.ServeHTTP(w, r)
			return
		}
		s.denyAuth(w, r)
	})
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
	return s.auth(mux)
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
	case "/api/ratings":
		s.handleRatings(w, r)
	case "/api/phototime":
		s.handlePhotoTime(w, r)
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
