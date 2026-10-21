//go:build windows

package macmeta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"macbrowser/internal/config"
	"macbrowser/internal/vfs"
)

func TestNasLabelProbe(t *testing.T) {
	if os.Getenv("MB_NAS_PROBE") == "" {
		t.Skip("set MB_NAS_PROBE=1 to run SMB roundtrip probe")
	}
	dataDir := filepath.Join(os.Getenv("AppData"), "macbrowser")
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Skipf("config: %v", err)
	}
	lp := cfg.LastPath
	if !strings.HasPrefix(lp, "smb://") {
		t.Skipf("lastPath is not smb: %q", lp)
	}
	vfs.SetConnections(cfg.ConnectionsSnapshot())
	b, err := vfs.Resolve(lp)
	if err != nil {
		t.Skipf("resolve %q: %v", lp, err)
	}
	name := fmt.Sprintf("mb_probe_%d.jpg", time.Now().UnixNano())
	full := b.FS.Join(b.Path, name)
	if err := b.FS.WriteFile(full, []byte("x")); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer b.FS.Remove(full, false)
	defer b.FS.Remove(b.FS.Join(b.Path, "._"+name), false)

	if err := SetLabel(b.FS, b.Path, name, 6); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	if lbl := Resolve(b.FS, b.Path, name, false); lbl == nil || lbl.Color != 6 {
		t.Fatalf("label = %+v, want color 6", lbl)
	}
	if f, err := b.FS.Open(b.FS.Join(b.Path, "._"+name)); err != nil {
		t.Error("expected AppleDouble sidecar on SMB target")
	} else {
		f.Close()
	}
	if err := SetLabel(b.FS, b.Path, name, 0); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if lbl := Resolve(b.FS, b.Path, name, false); lbl != nil && lbl.Color != 0 {
		t.Fatalf("after clear label = %+v", lbl)
	}

	dirName := fmt.Sprintf("mbprobe_dir_%d", time.Now().UnixNano())
	dirPath := b.FS.Join(b.Path, dirName)
	if err := b.FS.Mkdir(dirPath); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer b.FS.Remove(dirPath, true)
	if err := SetLabelNode(b.FS, b.Path, dirName, 4, true); err != nil {
		t.Fatalf("folder SetLabelNode: %v", err)
	}
	if lbl := Resolve(b.FS, b.Path, dirName, false); lbl == nil || lbl.Color != 4 {
		t.Fatalf("folder label = %+v, want color 4", lbl)
	}
	if err := SetLabelNode(b.FS, b.Path, dirName, 0, true); err != nil {
		t.Fatalf("folder clear: %v", err)
	}
	if lbl := Resolve(b.FS, b.Path, dirName, false); lbl != nil && lbl.Color != 0 {
		t.Fatalf("after clear folder label = %+v", lbl)
	}
}
