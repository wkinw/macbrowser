package main

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"macbrowser/internal/config"
	"macbrowser/internal/server"
	"macbrowser/internal/thumbs"
	"macbrowser/internal/vfs"
)

func init() {
	windows.SetConsoleOutputCP(65001)
}

func hasConsole() bool {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil || h == windows.InvalidHandle {
		return false
	}
	var mode uint32
	return windows.GetConsoleMode(h, &mode) == nil
}

func ensureConsole() {
	if !hasConsole() {
		proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("AllocConsole")
		if ret, _, _ := proc.Call(); ret == 0 {
			return
		}
	}
	windows.SetConsoleOutputCP(65001)
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = f
		os.Stderr = f
		log.SetOutput(f)
	}
}

func fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if hasConsole() {
		fmt.Fprintln(os.Stderr, msg)
	} else {
		p, _ := windows.UTF16PtrFromString(msg)
		c, _ := windows.UTF16PtrFromString("MacBrowser")
		windows.MessageBox(0, p, c, windows.MB_OK|windows.MB_ICONERROR)
	}
	os.Exit(1)
}

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "HTTP 服務位址")
	token := flag.String("token", "", "HTTP 存取權杖（預設每次啟動隨機產生；固定值供區域網路存取）")
	dataDir := flag.String("data", defaultDataDir(), "設定與快取目錄")
	browserOnly := flag.Bool("browser", false, "使用預設瀏覽器開啟（不使用內建視窗）")
	noOpen := flag.Bool("noopen", false, "不自動開啟視窗或瀏覽器")
	tileCacheMB := flag.Int("thumbmem", 192, "縮圖記憶體快取上限（MB）")
	webDir := flag.String("webdir", "", "從磁碟目錄載入前端檔案（開發用；預設使用內嵌資源）")
	debug := flag.Bool("debug", false, "除錯模式（開啟開發者工具）")
	flag.Parse()

	if *debug || *noOpen || *browserOnly {
		ensureConsole()
	}

	cfg, err := config.Load(*dataDir)
	if err != nil {
		fatal("讀取設定失敗：%v", err)
	}
	vfs.SetConnections(cfg.ConnectionsSnapshot())

	authToken := strings.TrimSpace(*token)
	if authToken == "" {
		authToken, err = randomToken()
		if err != nil {
			fatal("無法產生存取權杖：%v", err)
		}
	}

	tc := thumbs.New(filepath.Join(*dataDir, "thumbs"), *tileCacheMB)
	app := server.New(cfg, tc, *webDir)
	app.SetAuthToken(authToken)
	srv := &http.Server{Handler: app.Handler()}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fatal("無法監聽 %s：%v", *addr, err)
	}
	baseURL := "http://" + ln.Addr().String()
	browseURL := baseURL + "/?token=" + url.QueryEscape(authToken)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP 服務錯誤：%v", err)
		}
	}()
	fmt.Printf("MacBrowser 已啟動：%s\n", baseURL)
	fmt.Printf("設定目錄：%s\n", *dataDir)
	if *noOpen {
		fmt.Printf("存取網址（含權杖）：%s\n", browseURL)
	}

	if *noOpen {
		select {}
	}

	if !*browserOnly {
		if w := webview2.NewWithOptions(webview2.WebViewOptions{
			Debug:       *debug,
			AutoFocus:   true,
			DisableZoom: true,
			DataPath:    filepath.Join(*dataDir, "webview"),
			WindowOptions: webview2.WindowOptions{
				Title:  "MacBrowser — 檔案瀏覽器",
				Width:  1280,
				Height: 840,
				Center: true,
				IconId: 32512,
			},
		}); w != nil {
			w.Navigate(browseURL)
			w.Run()
			return
		}
		ensureConsole()
		log.Printf("找不到 WebView2 執行階段，改用預設瀏覽器開啟")
	}
	openURL(browseURL)
	fmt.Println("已在瀏覽器開啟。關閉此主控台視窗即可結束程式。")
	select {}
}

func defaultDataDir() string {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		cfgDir = os.TempDir()
	}
	return filepath.Join(cfgDir, "macbrowser")
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func openURL(url string) {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("ShellExecuteW")
	verb, _ := syscall.UTF16PtrFromString("open")
	u, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		fatal("無法開啟瀏覽器：%v", err)
	}
	ret, _, _ := proc.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(u)), 0, 0, 1)
	if ret <= 32 {
		fatal("無法開啟瀏覽器（ShellExecute 代碼 %d），請手動前往 %s", ret, url)
	}
}
