//go:build windows

package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func postJSON(t *testing.T, ts string, path string, body string) (*http.Response, map[string]any) {
	t.Helper()
	resp, err := http.Post(ts+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if resp.StatusCode == 200 {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp, out
}

func TestAPICopyMove(t *testing.T) {
	ts, dir := newTestServer(t)
	writeTestJPEG(t, filepath.Join(dir, "a.jpg"))
	os.MkdirAll(filepath.Join(dir, "sub"), 0o777)
	os.MkdirAll(filepath.Join(dir, "tree", "deep"), 0o777)
	os.WriteFile(filepath.Join(dir, "tree", "deep", "x.txt"), []byte("hello"), 0o666)

	resp, out := postJSON(t, ts.URL, "/api/copy",
		`{"paths":[`+jsonStr(filepath.Join(dir, "a.jpg"))+`],"dest":`+jsonStr(filepath.Join(dir, "sub"))+`}`)
	if resp.StatusCode != 200 || out["ok"] != true || out["copied"].(float64) != 1 {
		t.Fatalf("copy result: code=%d out=%v", resp.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "a.jpg")); err != nil {
		t.Fatal("sub/a.jpg not created")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.jpg")); err != nil {
		t.Fatal("source a.jpg should still exist after copy")
	}

	resp, out = postJSON(t, ts.URL, "/api/copy",
		`{"paths":[`+jsonStr(filepath.Join(dir, "a.jpg"))+`],"dest":`+jsonStr(filepath.Join(dir, "sub"))+`}`)
	if resp.StatusCode != 200 || out["ok"] != true {
		t.Fatalf("second copy result: %v", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "a (2).jpg")); err != nil {
		t.Fatal("unique name a (2).jpg not created")
	}

	resp, out = postJSON(t, ts.URL, "/api/copy",
		`{"paths":[`+jsonStr(filepath.Join(dir, "tree"))+`],"dest":`+jsonStr(filepath.Join(dir, "sub"))+`}`)
	if resp.StatusCode != 200 || out["ok"] != true {
		t.Fatalf("dir copy result: %v", out)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "sub", "tree", "deep", "x.txt")); err != nil || string(b) != "hello" {
		t.Fatalf("recursive copy failed: %v %q", err, b)
	}

	resp, out = postJSON(t, ts.URL, "/api/copy",
		`{"paths":[`+jsonStr(filepath.Join(dir, "a.jpg"))+`],"dest":`+jsonStr(filepath.Join(dir, "tree"))+`,"move":true}`)
	if resp.StatusCode != 200 || out["ok"] != true {
		t.Fatalf("move result: %v", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "tree", "a.jpg")); err != nil {
		t.Fatal("tree/a.jpg not found after move")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.jpg")); !os.IsNotExist(err) {
		t.Fatal("a.jpg should be gone after move")
	}

	resp, out = postJSON(t, ts.URL, "/api/copy",
		`{"paths":[`+jsonStr(filepath.Join(dir, "tree", "a.jpg"))+`],"dest":`+jsonStr(filepath.Join(dir, "tree"))+`,"move":true}`)
	if resp.StatusCode != 200 || out["ok"] != false {
		t.Fatalf("same-dir move should fail: %v", out)
	}
	errs, _ := out["errors"].([]any)
	if len(errs) == 0 {
		t.Fatal("expected error for same-dir move")
	}
}

func TestMoveCarriesAppleDoubleSidecar(t *testing.T) {
	ts, dir := newTestServer(t)
	writeTestJPEG(t, filepath.Join(dir, "a.jpg"))
	os.WriteFile(filepath.Join(dir, "._a.jpg"), []byte("AD-a"), 0o666)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o777)

	resp, out := postJSON(t, ts.URL, "/api/copy",
		`{"paths":[`+jsonStr(filepath.Join(dir, "a.jpg"))+`],"dest":`+jsonStr(filepath.Join(dir, "sub"))+`,"move":true}`)
	if resp.StatusCode != 200 || out["ok"] != true {
		t.Fatalf("move result: %v", out)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "sub", "._a.jpg")); err != nil || string(b) != "AD-a" {
		t.Fatalf("sidecar did not follow move: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(dir, "._a.jpg")); !os.IsNotExist(err) {
		t.Fatal("source sidecar should be gone after move")
	}

	writeTestJPEG(t, filepath.Join(dir, "b.jpg"))
	os.WriteFile(filepath.Join(dir, "._b.jpg"), []byte("AD-b"), 0o666)
	writeTestJPEG(t, filepath.Join(dir, "sub", "b.jpg"))
	resp, out = postJSON(t, ts.URL, "/api/copy",
		`{"paths":[`+jsonStr(filepath.Join(dir, "b.jpg"))+`],"dest":`+jsonStr(filepath.Join(dir, "sub"))+`,"move":true}`)
	if resp.StatusCode != 200 || out["ok"] != true {
		t.Fatalf("copy-move result: %v", out)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "sub", "._b (2).jpg")); err != nil || string(b) != "AD-b" {
		t.Fatalf("sidecar of copy-move wrong: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(dir, "._b.jpg")); !os.IsNotExist(err) {
		t.Fatal("source sidecar should be gone after copy-move")
	}
}

func TestAPIUploadDownload(t *testing.T) {
	ts, dir := newTestServer(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("files", "up1.txt")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte("data1"))
	fw2, err := w.CreateFormFile("up:deep/", "up2.txt")
	if err != nil {
		t.Fatal(err)
	}
	fw2.Write([]byte("data2"))
	fw3, err := w.CreateFormFile("up:../", "evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	fw3.Write([]byte("evil"))
	w.Close()

	resp, err := http.Post(ts.URL+"/api/upload?p="+url.QueryEscape(dir), w.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || out["uploaded"].(float64) != 2 {
		t.Fatalf("upload result: code=%d out=%v", resp.StatusCode, out)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "up1.txt")); err != nil || string(b) != "data1" {
		t.Fatalf("up1.txt wrong: %v %q", err, b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "deep", "up2.txt")); err != nil || string(b) != "data2" {
		t.Fatalf("deep/up2.txt wrong: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("path traversal was not blocked")
	}
	errs, _ := out["errors"].([]any)
	if len(errs) == 0 {
		t.Fatal("expected error for traversal filename")
	}

	home := t.TempDir()
	t.Setenv("USERPROFILE", home)

	resp, down := postJSON(t, ts.URL, "/api/download",
		`{"paths":[`+jsonStr(filepath.Join(dir, "up1.txt"))+`,`+jsonStr(filepath.Join(dir, "deep"))+`]}`)
	if resp.StatusCode != 200 || down["ok"] != true {
		t.Fatalf("download result: code=%d out=%v", resp.StatusCode, down)
	}
	if b, err := os.ReadFile(filepath.Join(home, "Downloads", "up1.txt")); err != nil || string(b) != "data1" {
		t.Fatalf("downloaded up1.txt wrong: %v %q", err, b)
	}
	if b, err := os.ReadFile(filepath.Join(home, "Downloads", "deep", "up2.txt")); err != nil || string(b) != "data2" {
		t.Fatalf("downloaded deep/up2.txt wrong: %v %q", err, b)
	}

	resp, down = postJSON(t, ts.URL, "/api/download",
		`{"paths":[`+jsonStr(filepath.Join(dir, "up1.txt"))+`]}`)
	if resp.StatusCode != 200 || down["ok"] != true {
		t.Fatalf("second download result: %v", down)
	}
	if _, err := os.Stat(filepath.Join(home, "Downloads", "up1 (2).txt")); err != nil {
		t.Fatal("unique download name not used")
	}
}
