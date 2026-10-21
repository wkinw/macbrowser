package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

	tc := thumbs.New(filepath.Join(*dataDir, "thumbs"), *tileCacheMB)
	srv := &http.Server{Handler: server.New(cfg, tc, *webDir).Handler()}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fatal("無法監聽 %s：%v", *addr, err)
	}
	url := "http://" + ln.Addr().String()
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP 服務錯誤：%v", err)
		}
	}()
	fmt.Printf("MacBrowser 已啟動：%s\n", url)
	fmt.Printf("設定目錄：%s\n", *dataDir)

	if *noOpen {
		select {}
	}

	if !*browserOnly {
		if w := webview2.NewWithOptions(webview2.WebViewOptions{
			Debug:     *debug,
			AutoFocus: true,
			DataPath:  filepath.Join(*dataDir, "webview"),
			WindowOptions: webview2.WindowOptions{
				Title:  "MacBrowser — 檔案瀏覽器",
				Width:  1280,
				Height: 840,
				Center: true,
				IconId: 32512,
			},
		}); w != nil {
			w.Navigate(url)
			w.Run()
			return
		}
		ensureConsole()
		log.Printf("找不到 WebView2 執行階段，改用預設瀏覽器開啟")
	}
	openURL(url)
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
