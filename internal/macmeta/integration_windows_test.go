//go:build windows

package macmeta

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"

	"macbrowser/internal/vfs"
)

func writeADS(t *testing.T, path, stream string, data []byte) error {
	t.Helper()
	full := `\\?\` + path + ":" + stream
	ptr, err := syscall.UTF16PtrFromString(full)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(ptr,
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.CREATE_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var written uint32
	return windows.WriteFile(h, data, &written, nil)
}

func writeTestJPEG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 2), G: uint8(y * 3), B: 128, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
}

func TestLocalColonStreamUnsupported(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	dir := t.TempDir()
	pic := filepath.Join(dir, "pic.jpg")
	writeTestJPEG(t, pic)
	pl := plistTags(t, []string{"Red\n6"})
	err := writeADS(t, pic, UserTagsAttr, pl)
	if err == nil {
		t.Log("此 Windows 版本允許含冒號的串流名稱")
	} else {
		t.Logf("NTFS 不允許含冒號的串流名稱（預期行為）：%v", err)
	}
	b, err := vfs.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	lbl := Resolve(b.FS, b.Path, "pic.jpg", false)
	if lbl != nil {
		t.Fatalf("no label expected when colon streams are rejected, got %+v", lbl)
	}
}

func TestLocalUserTagsStreamRoundtrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	dir := t.TempDir()
	pic := filepath.Join(dir, "pic4.jpg")
	writeTestJPEG(t, pic)

	b, err := vfs.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetLabel(b.FS, b.Path, "pic4.jpg", 6); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	lbl := Resolve(b.FS, b.Path, "pic4.jpg", false)
	if lbl == nil || lbl.Color != 6 {
		t.Fatalf("label = %+v, want color 6", lbl)
	}
	raw, err := os.ReadFile(`\\?\` + pic + ":com.apple.metadata\uF022_kMDItemUserTags:$DATA")
	if err != nil {
		t.Fatalf("U+F022 stream missing: %v", err)
	}
	ut := ParseUserTags(raw)
	if ut == nil || ut.Color != 6 {
		t.Fatalf("stream tags = %+v, want color 6", ut)
	}
	if _, err := os.Stat(filepath.Join(dir, "._pic4.jpg")); err == nil {
		t.Fatal("sidecar should not exist when stream write succeeds")
	}
	if err := SetLabel(b.FS, b.Path, "pic4.jpg", 0); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if lbl := Resolve(b.FS, b.Path, "pic4.jpg", false); lbl != nil && lbl.Color != 0 {
		t.Fatalf("after clear label = %+v", lbl)
	}
}

func TestResolveLocalFinderInfoADS(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	dir := t.TempDir()
	pic := filepath.Join(dir, "pic2.jpg")
	writeTestJPEG(t, pic)

	fi := make([]byte, 32)
	fi[9] = 4 << 1
	if err := writeADS(t, pic, FinderInfoAttr, fi); err != nil {
		t.Skipf("ADS not supported on this volume: %v", err)
	}

	b, err := vfs.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	lbl := Resolve(b.FS, b.Path, "pic2.jpg", false)
	if lbl == nil || lbl.Color != 4 {
		t.Fatalf("label = %+v, want color 4", lbl)
	}
}

func TestLocalFolderLabelRoundtrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	dir := t.TempDir()
	folder := filepath.Join(dir, "album")
	if err := os.MkdirAll(folder, 0o777); err != nil {
		t.Fatal(err)
	}
	b, err := vfs.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetLabelNode(b.FS, b.Path, "album", 3, true); err != nil {
		t.Fatalf("SetLabelNode: %v", err)
	}
	lbl := Resolve(b.FS, b.Path, "album", false)
	if lbl == nil || lbl.Color != 3 {
		t.Fatalf("folder label = %+v, want color 3", lbl)
	}
	raw, err := os.ReadFile(`\\?\` + folder + ":com.apple.metadata\uF022_kMDItemUserTags:$DATA")
	if err != nil {
		t.Fatalf("folder U+F022 stream missing: %v", err)
	}
	if ut := ParseUserTags(raw); ut == nil || ut.Color != 3 {
		t.Fatalf("folder stream tags = %+v, want color 3", ut)
	}
	if _, err := os.Stat(filepath.Join(dir, "._album")); err == nil {
		t.Fatal("folder should not get an AppleDouble sidecar")
	}
	if err := SetLabelNode(b.FS, b.Path, "album", 0, true); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if lbl := Resolve(b.FS, b.Path, "album", false); lbl != nil && lbl.Color != 0 {
		t.Fatalf("after clear folder label = %+v", lbl)
	}
}

func TestResolveLocalAppleDoubleFallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	dir := t.TempDir()
	pic := filepath.Join(dir, "pic3.jpg")
	writeTestJPEG(t, pic)

	fi := make([]byte, 32)
	fi[9] = 7 << 1
	ad := buildAD(t, map[int][]byte{9: fi})
	if err := os.WriteFile(filepath.Join(dir, "._pic3.jpg"), ad, 0o666); err != nil {
		t.Fatal(err)
	}

	b, err := vfs.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	lbl := Resolve(b.FS, b.Path, "pic3.jpg", true)
	if lbl == nil || lbl.Color != 7 {
		t.Fatalf("label = %+v, want color 7", lbl)
	}
}
