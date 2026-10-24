//go:build windows

package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
	"howett.net/plist"

	"macbrowser/internal/config"
	"macbrowser/internal/macmeta"
	"macbrowser/internal/thumbs"
	"macbrowser/internal/vfs"
)

func plistMarshal(tags []string) ([]byte, error) {
	return plist.Marshal(tags, plist.BinaryFormat)
}

func writeTestJPEG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 160, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 160; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y * 2), B: 64, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
}

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

func buildTagsPlist(t *testing.T, tags []string) []byte {
	t.Helper()
	data, err := plistMarshal(tags)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	vfs.SetConnections(nil)
	tc := thumbs.New(t.TempDir(), 64)
	s := New(cfg, tc, "")
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, dir
}

func getJSON(t *testing.T, u string, v any) int {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatalf("decode %s: %v", u, err)
		}
	}
	return resp.StatusCode
}

func TestAPIListAndThumb(t *testing.T) {
	ts, dir := newTestServer(t)
	pic := filepath.Join(dir, "photo.jpg")
	writeTestJPEG(t, pic)
	os.WriteFile(filepath.Join(dir, "._photo.jpg"), []byte("stub"), 0o666)

	var list struct {
		Path    string      `json:"path"`
		Parent  string      `json:"parent"`
		Entries []vfs.Entry `json:"entries"`
	}
	code := getJSON(t, ts.URL+"/api/list?p="+url.QueryEscape(dir), &list)
	if code != 200 {
		t.Fatalf("list code = %d", code)
	}
	var photo *vfs.Entry
	for i := range list.Entries {
		if list.Entries[i].Name == "photo.jpg" {
			photo = &list.Entries[i]
		}
	}
	if photo == nil {
		t.Fatalf("photo.jpg not in %+v", list.Entries)
	}
	if !photo.Thumb {
		t.Fatal("photo.jpg should be thumbable")
	}

	resp, err := http.Get(ts.URL + "/api/thumb?p=" + url.QueryEscape(pic) + "&s=128")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("thumb code = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("thumb content-type = %q", ct)
	}
	data, _ := io.ReadAll(resp.Body)
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatalf("thumb is not JPEG: %v", data[:2])
	}

	resp2, err := http.Get(ts.URL + "/api/thumb?p=" + url.QueryEscape(pic) + "&s=128")
	if err != nil {
		t.Fatal(err)
	}
	data2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if len(data2) != len(data) || len(data2) == 0 {
		t.Fatalf("thumb not cached consistently: %d vs %d bytes", len(data2), len(data))
	}
}

func TestAPIFolderThumb(t *testing.T) {
	ts, dir := newTestServer(t)
	album := filepath.Join(dir, "album")
	sub := filepath.Join(album, "inner")
	if err := os.MkdirAll(sub, 0o777); err != nil {
		t.Fatal(err)
	}
	writeTestJPEG(t, filepath.Join(album, "one.jpg"))
	writeTestJPEG(t, filepath.Join(album, "two.jpg"))
	writeTestJPEG(t, filepath.Join(album, "three.jpg"))
	writeTestJPEG(t, filepath.Join(sub, "pic.jpg"))

	resp, err := http.Get(ts.URL + "/api/thumb?p=" + url.QueryEscape(album) + "&s=128")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("folder thumb code = %d", resp.StatusCode)
	}
	data, _ := io.ReadAll(resp.Body)
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatal("folder thumb is not JPEG")
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("folder thumb decode: %v", err)
	}
	if img.Bounds().Dx() != img.Bounds().Dy() {
		t.Fatalf("folder collage should be square, got %v", img.Bounds())
	}

	emptyDir := filepath.Join(dir, "empty")
	os.MkdirAll(emptyDir, 0o777)
	resp2, err := http.Get(ts.URL + "/api/thumb?p=" + url.QueryEscape(emptyDir) + "&s=128")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 404 {
		t.Fatalf("empty folder thumb code = %d, want 404", resp2.StatusCode)
	}
}

func TestAPILabels(t *testing.T) {
	ts, dir := newTestServer(t)
	tagged := filepath.Join(dir, "tagged.jpg")
	writeTestJPEG(t, tagged)
	writeTestJPEG(t, filepath.Join(dir, "ado.jpg"))
	os.WriteFile(filepath.Join(dir, "plain.jpg"), []byte("notanimage"), 0o666)

	fi := make([]byte, 32)
	fi[9] = 4 << 1
	if err := writeADS(t, tagged, "com.apple.FinderInfo", fi); err != nil {
		t.Skipf("ADS unsupported: %v", err)
	}

	pl := buildTagsPlist(t, []string{"Red\n6", "專案"})
	nested := buildAppleDouble(t, map[int][]byte{
		3: []byte("com.apple.metadata:_kMDItemUserTags"),
		1: pl,
	})
	ad := buildAppleDouble(t, map[int][]byte{2: nested})
	if err := os.WriteFile(filepath.Join(dir, "._ado.jpg"), ad, 0o666); err != nil {
		t.Fatal(err)
	}

	var out struct {
		Labels map[string]*vfs.Label `json:"labels"`
	}
	code := getJSON(t, ts.URL+"/api/labels?p="+url.QueryEscape(dir), &out)
	if code != 200 {
		t.Fatalf("labels code = %d", code)
	}
	if lbl := out.Labels["tagged.jpg"]; lbl == nil || lbl.Color != 4 {
		t.Fatalf("tagged.jpg label = %+v, want color 4 from FinderInfo ADS", lbl)
	}
	if lbl := out.Labels["ado.jpg"]; lbl == nil || lbl.Color != 6 || len(lbl.Tags) != 2 {
		t.Fatalf("ado.jpg label = %+v, want color 6 + tags from AppleDouble", lbl)
	}
	if _, ok := out.Labels["plain.jpg"]; ok {
		t.Fatal("plain.jpg should have no label")
	}
}

func buildAppleDouble(t *testing.T, entries map[int][]byte) []byte {
	t.Helper()
	ids := make([]int, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	hdr := 26 + len(ids)*12
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(0x00051607))
	binary.Write(&buf, binary.BigEndian, uint32(0x00020000))
	buf.Write(make([]byte, 16))
	binary.Write(&buf, binary.BigEndian, uint16(len(ids)))
	off := hdr
	type ent struct{ id, off, ln int }
	ents := make([]ent, 0, len(ids))
	for _, id := range ids {
		d := entries[id]
		ents = append(ents, ent{id, off, len(d)})
		off += len(d)
	}
	for _, e := range ents {
		binary.Write(&buf, binary.BigEndian, uint32(e.id))
		binary.Write(&buf, binary.BigEndian, uint32(e.off))
		binary.Write(&buf, binary.BigEndian, uint32(e.ln))
	}
	for _, id := range ids {
		buf.Write(entries[id])
	}
	return buf.Bytes()
}

func TestAPIFileOps(t *testing.T) {
	ts, dir := newTestServer(t)
	writeTestJPEG(t, filepath.Join(dir, "a.jpg"))

	post := func(path string, body string) *http.Response {
		t.Helper()
		resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := post("/api/mkdir", `{"p":`+jsonStr(dir)+`,"name":"newdir"}`)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("mkdir code = %d", resp.StatusCode)
	}
	if st, err := os.Stat(filepath.Join(dir, "newdir")); err != nil || !st.IsDir() {
		t.Fatal("newdir not created")
	}

	resp = post("/api/rename", `{"p":`+jsonStr(filepath.Join(dir, "a.jpg"))+`,"name":"b.jpg"}`)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("rename code = %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.jpg")); err != nil {
		t.Fatal("b.jpg not found after rename")
	}

	resp = post("/api/delete", `{"paths":[`+jsonStr(filepath.Join(dir, "b.jpg"))+`,`+jsonStr(filepath.Join(dir, "newdir"))+`]}`)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("delete code = %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.jpg")); !os.IsNotExist(err) {
		t.Fatal("b.jpg still exists")
	}
	if _, err := os.Stat(filepath.Join(dir, "newdir")); !os.IsNotExist(err) {
		t.Fatal("newdir still exists")
	}
}

func TestSetLabelsAPI(t *testing.T) {
	ts, dir := newTestServer(t)
	pic := filepath.Join(dir, "pic.jpg")
	pic2 := filepath.Join(dir, "pic2.jpg")
	writeTestJPEG(t, pic)
	writeTestJPEG(t, pic2)

	fi := make([]byte, 32)
	fi[9] = 2 << 1
	if err := os.WriteFile(filepath.Join(dir, "._pic2.jpg"), buildAppleDouble(t, map[int][]byte{9: fi}), 0o666); err != nil {
		t.Fatal(err)
	}

	post := func(body string) *http.Response {
		t.Helper()
		resp, err := http.Post(ts.URL+"/api/labels", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	var setOut struct {
		Ok      bool     `json:"ok"`
		Updated int      `json:"updated"`
		Errors  []string `json:"errors"`
	}

	body := `{"paths":[` + jsonStr(pic) + `,` + jsonStr(pic2) + `],"color":4}`
	resp := post(body)
	if resp.StatusCode != 200 {
		t.Fatalf("set labels code = %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&setOut); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !setOut.Ok || setOut.Updated != 2 || len(setOut.Errors) != 0 {
		t.Fatalf("set result = %+v", setOut)
	}

	var lblOut struct {
		Labels map[string]*vfs.Label `json:"labels"`
	}
	if code := getJSON(t, ts.URL+"/api/labels?p="+url.QueryEscape(dir), &lblOut); code != 200 {
		t.Fatalf("labels code = %d", code)
	}
	if lbl := lblOut.Labels["pic.jpg"]; lbl == nil || lbl.Color != 4 {
		t.Fatalf("pic.jpg label = %+v, want 4", lbl)
	}
	if lbl := lblOut.Labels["pic2.jpg"]; lbl == nil || lbl.Color != 4 {
		t.Fatalf("pic2.jpg label = %+v, want 4", lbl)
	}

	adData, err := os.ReadFile(filepath.Join(dir, "._pic2.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	ad := macmeta.ParseAppleDouble(adData)
	if ad == nil || macmeta.ColorFromFinderInfo(ad.FinderInfo) != 4 {
		t.Fatalf("FinderInfo was not synchronized with UserTags: %+v", ad)
	}
	if ut := macmeta.ParseUserTags(ad.EAs[macmeta.UserTagsAttr]); ut == nil || ut.Color != 4 {
		t.Fatalf("apple double tags = %+v", ut)
	}

	resp = post(`{"paths":[` + jsonStr(pic) + `,` + jsonStr(pic2) + `],"color":0}`)
	if resp.StatusCode != 200 {
		t.Fatalf("clear code = %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&setOut); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !setOut.Ok || setOut.Updated != 2 {
		t.Fatalf("clear result = %+v", setOut)
	}

	lblOut.Labels = nil
	if code := getJSON(t, ts.URL+"/api/labels?p="+url.QueryEscape(dir), &lblOut); code != 200 {
		t.Fatalf("labels after clear code = %d", code)
	}
	if _, ok := lblOut.Labels["pic.jpg"]; ok {
		t.Fatalf("pic.jpg still labeled: %+v", lblOut.Labels["pic.jpg"])
	}
	if lbl, ok := lblOut.Labels["pic2.jpg"]; ok {
		t.Fatalf("pic2.jpg should no longer be labeled: %+v", lbl)
	}
	adData, err = os.ReadFile(filepath.Join(dir, "._pic2.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	ad = macmeta.ParseAppleDouble(adData)
	if ad == nil || macmeta.ColorFromFinderInfo(ad.FinderInfo) != 0 {
		t.Fatalf("FinderInfo was not cleared with UserTags: %+v", ad)
	}
	if _, ok := ad.EAs[macmeta.UserTagsAttr]; ok {
		t.Fatal("UserTags EA should be gone after clear")
	}

	resp = post(`{"paths":[` + jsonStr(pic) + `],"color":9}`)
	if resp.StatusCode != 400 {
		t.Fatalf("bad color code = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
	resp = post(`{"paths":[],"color":4}`)
	if resp.StatusCode != 400 {
		t.Fatalf("empty paths code = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSetFolderLabelAPI(t *testing.T) {
	ts, dir := newTestServer(t)
	album := filepath.Join(dir, "album")
	if err := os.MkdirAll(album, 0o777); err != nil {
		t.Fatal(err)
	}
	var setOut struct {
		Ok      bool     `json:"ok"`
		Updated int      `json:"updated"`
		Errors  []string `json:"errors"`
	}
	resp, err := http.Post(ts.URL+"/api/labels", "application/json", strings.NewReader(`{"paths":[`+jsonStr(album)+`],"color":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("set folder label code = %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&setOut); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !setOut.Ok || setOut.Updated != 1 || len(setOut.Errors) != 0 {
		t.Fatalf("set folder result = %+v", setOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "._album")); err == nil {
		t.Fatal("folder must not get an AppleDouble sidecar")
	}
	var lblOut struct {
		Labels map[string]*vfs.Label `json:"labels"`
	}
	if code := getJSON(t, ts.URL+"/api/labels?p="+url.QueryEscape(dir), &lblOut); code != 200 {
		t.Fatalf("labels code = %d", code)
	}
	if lbl := lblOut.Labels["album"]; lbl == nil || lbl.Color != 3 {
		t.Fatalf("album label = %+v, want color 3", lbl)
	}
	resp, err = http.Post(ts.URL+"/api/labels", "application/json", strings.NewReader(`{"paths":[`+jsonStr(album)+`],"color":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("clear folder label code = %d", resp.StatusCode)
	}
	resp.Body.Close()
	lblOut.Labels = nil
	if code := getJSON(t, ts.URL+"/api/labels?p="+url.QueryEscape(dir), &lblOut); code != 200 {
		t.Fatalf("labels after clear code = %d", code)
	}
	if lbl, ok := lblOut.Labels["album"]; ok && lbl.Color != 0 {
		t.Fatalf("album still labeled: %+v", lbl)
	}
}

func TestBuildDropFiles(t *testing.T) {
	paths := []string{`C:\a\b.jpg`, `C:\中文\x.png`}
	data := buildDropFiles(paths)
	if len(data) < 24 {
		t.Fatalf("too short: %d", len(data))
	}
	if got := binary.LittleEndian.Uint32(data[0:]); got != 20 {
		t.Fatalf("pFiles = %d, want 20", got)
	}
	if got := binary.LittleEndian.Uint32(data[16:]); got != 1 {
		t.Fatalf("fWide = %d, want 1", got)
	}
	units := make([]uint16, 0, (len(data)-20)/2)
	for i := 20; i+1 < len(data); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(data[i:]))
	}
	parts := strings.Split(string(utf16.Decode(units)), "\x00")
	if len(parts) < 3 || parts[0] != paths[0] || parts[1] != paths[1] {
		t.Fatalf("decoded paths = %q", parts)
	}
}

func TestCopyOSAPI(t *testing.T) {
	ts, dir := newTestServer(t)
	pic := filepath.Join(dir, "clip.jpg")
	writeTestJPEG(t, pic)
	resp, err := http.Post(ts.URL+"/api/copyos", "application/json", strings.NewReader(`{"paths":[`+jsonStr(pic)+`],"move":false}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("copyos code = %d", resp.StatusCode)
	}
	var out struct {
		Ok     bool   `json:"ok"`
		Copied int    `json:"copied"`
		Seq    uint32 `json:"seq"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.Ok || out.Copied != 1 {
		t.Fatalf("copyos = %+v", out)
	}
	files, err := clipboardFiles()
	if err == nil && len(files) > 0 {
		found := false
		for _, f := range files {
			if strings.EqualFold(f, pic) {
				found = true
			}
		}
		if !found {
			t.Fatalf("clipboard = %v, want %s", files, pic)
		}
	}
}

func TestAPIFind(t *testing.T) {
	ts, dir := newTestServer(t)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o777)
	os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o777)
	writeTestJPEG(t, filepath.Join(dir, "sub", "deep", "sunset.jpg"))
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o666)

	var out struct {
		Entries []vfs.Entry `json:"entries"`
	}
	code := getJSON(t, ts.URL+"/api/find?p="+url.QueryEscape(dir)+"&q=sunset", &out)
	if code != 200 {
		t.Fatalf("find code = %d", code)
	}
	if len(out.Entries) != 1 || out.Entries[0].Name != "sunset.jpg" {
		t.Fatalf("find results = %+v", out.Entries)
	}
	if !strings.HasSuffix(out.Entries[0].Path, "sunset.jpg") {
		t.Fatalf("find result path = %q", out.Entries[0].Path)
	}

	code = getJSON(t, ts.URL+"/api/find?p="+url.QueryEscape(dir)+"&q=readme", &out)
	if code != 200 || len(out.Entries) != 1 {
		t.Fatalf("find readme: code=%d entries=%+v", code, out.Entries)
	}
}

func TestAPIRootsAndStatic(t *testing.T) {
	ts, _ := newTestServer(t)
	var roots map[string]any
	if code := getJSON(t, ts.URL+"/api/roots", &roots); code != 200 {
		t.Fatalf("roots code = %d", code)
	}
	if roots["drives"] == nil {
		t.Fatal("no drives in roots")
	}
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "MacBrowser") {
		t.Fatalf("index not served: %d %q", resp.StatusCode, body[:min(len(body), 80)])
	}
	resp2, err := http.Get(ts.URL + "/app.js")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("app.js not served: %d", resp2.StatusCode)
	}
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func buildTIFFDateTime(t *testing.T, date string) []byte {
	t.Helper()
	var b bytes.Buffer
	le := binary.LittleEndian
	value := append([]byte(date), 0)
	b.WriteString("II")
	binary.Write(&b, le, uint16(42))
	binary.Write(&b, le, uint32(8))
	binary.Write(&b, le, uint16(1))
	binary.Write(&b, le, uint16(0x8769))
	binary.Write(&b, le, uint16(4))
	binary.Write(&b, le, uint32(1))
	binary.Write(&b, le, uint32(26))
	binary.Write(&b, le, uint32(0))
	binary.Write(&b, le, uint16(1))
	binary.Write(&b, le, uint16(0x9003))
	binary.Write(&b, le, uint16(2))
	binary.Write(&b, le, uint32(len(value)))
	binary.Write(&b, le, uint32(44))
	binary.Write(&b, le, uint32(0))
	b.Write(value)
	return b.Bytes()
}

func writeTestJPEGWithEXIF(t *testing.T, path, date string) {
	t.Helper()
	writeTestJPEG(t, path)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app1 := append([]byte("Exif\x00\x00"), buildTIFFDateTime(t, date)...)
	var out bytes.Buffer
	out.Write(raw[:2])
	out.Write([]byte{0xFF, 0xE1})
	var lb [2]byte
	binary.BigEndian.PutUint16(lb[:], uint16(len(app1)+2))
	out.Write(lb[:])
	out.Write(app1)
	out.Write(raw[2:])
	if err := os.WriteFile(path, out.Bytes(), 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestRatingsAPI(t *testing.T) {
	ts, dir := newTestServer(t)
	pic := filepath.Join(dir, "pic.jpg")
	pic2 := filepath.Join(dir, "pic2.jpg")
	sub := filepath.Join(dir, "sub")
	writeTestJPEG(t, pic)
	writeTestJPEG(t, pic2)
	if err := os.Mkdir(sub, 0o777); err != nil {
		t.Fatal(err)
	}

	post := func(body string) (int, map[string]any) {
		t.Helper()
		resp, err := http.Post(ts.URL+"/api/ratings", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, out
	}

	code, out := post(`{"paths":[` + jsonStr(pic) + `,` + jsonStr(pic2) + `],"stars":3}`)
	if code != 200 || out["ok"] != true || out["updated"] != float64(2) {
		t.Fatalf("set ratings: code=%d out=%+v", code, out)
	}

	var lblOut struct {
		Ratings map[string]int `json:"ratings"`
	}
	if code := getJSON(t, ts.URL+"/api/labels?p="+url.QueryEscape(dir), &lblOut); code != 200 {
		t.Fatalf("labels code = %d", code)
	}
	if lblOut.Ratings["pic.jpg"] != 3 || lblOut.Ratings["pic2.jpg"] != 3 {
		t.Fatalf("ratings = %+v, want 3", lblOut.Ratings)
	}

	if code, _ := post(`{"paths":[` + jsonStr(pic) + `],"stars":7}`); code != 400 {
		t.Fatalf("stars 7 code = %d, want 400", code)
	}
	code, out = post(`{"paths":[` + jsonStr(sub) + `,` + jsonStr(pic) + `],"stars":2}`)
	if code != 200 || out["updated"] != float64(1) {
		t.Fatalf("folder rating: code=%d out=%+v", code, out)
	}
	if errs, _ := out["errors"].([]any); len(errs) == 0 {
		t.Fatalf("folder should produce error: %+v", out)
	}

	code, out = post(`{"paths":[` + jsonStr(pic) + `,` + jsonStr(pic2) + `],"stars":0}`)
	if code != 200 || out["updated"] != float64(2) {
		t.Fatalf("clear ratings: code=%d out=%+v", code, out)
	}
	lblOut.Ratings = nil
	if code := getJSON(t, ts.URL+"/api/labels?p="+url.QueryEscape(dir), &lblOut); code != 200 {
		t.Fatalf("labels after clear = %d", code)
	}
	if len(lblOut.Ratings) != 0 {
		t.Fatalf("ratings after clear = %+v", lblOut.Ratings)
	}
}

func writeTestJPEGWithXMPRating(t *testing.T, path, rating string) {
	t.Helper()
	writeTestJPEG(t, path)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	xmp := `<?xpacket begin="` + "\xEF\xBB\xBF" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:xmp="http://ns.adobe.com/xap/1.0/">` +
		`<xmp:Rating>` + rating + `</xmp:Rating></rdf:Description></rdf:RDF></x:xmpmeta>`
	app1 := append([]byte("http://ns.adobe.com/xap/1.0/\x00"), []byte(xmp)...)
	var out bytes.Buffer
	out.Write(raw[:2])
	out.Write([]byte{0xFF, 0xE1})
	var lb [2]byte
	binary.BigEndian.PutUint16(lb[:], uint16(len(app1)+2))
	out.Write(lb[:])
	out.Write(app1)
	out.Write(raw[2:])
	if err := os.WriteFile(path, out.Bytes(), 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestRatingsFromXMP(t *testing.T) {
	ts, dir := newTestServer(t)
	pic := filepath.Join(dir, "xmp.jpg")
	writeTestJPEGWithXMPRating(t, pic, "1")

	var lblOut struct {
		Ratings map[string]int `json:"ratings"`
	}
	if code := getJSON(t, ts.URL+"/api/labels?p="+url.QueryEscape(dir), &lblOut); code != 200 {
		t.Fatalf("labels code = %d", code)
	}
	if lblOut.Ratings["xmp.jpg"] != 1 {
		t.Fatalf("xmp rating = %+v, want 1", lblOut.Ratings)
	}

	resp, err := http.Post(ts.URL+"/api/ratings", "application/json", strings.NewReader(`{"paths":[`+jsonStr(pic)+`],"stars":0}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("clear code = %d", resp.StatusCode)
	}
	lblOut.Ratings = nil
	if code := getJSON(t, ts.URL+"/api/labels?p="+url.QueryEscape(dir), &lblOut); code != 200 {
		t.Fatalf("labels after clear = %d", code)
	}
	if _, ok := lblOut.Ratings["xmp.jpg"]; ok {
		t.Fatalf("cleared XMP rating must not reappear: %+v", lblOut.Ratings)
	}
}

func TestPhotoTimeAPI(t *testing.T) {
	ts, dir := newTestServer(t)
	pic := filepath.Join(dir, "pic.jpg")
	plain := filepath.Join(dir, "plain.jpg")
	writeTestJPEGWithEXIF(t, pic, "2026:08:23 14:22:00")
	writeTestJPEG(t, plain)

	body := `{"paths":[` + jsonStr(pic) + `,` + jsonStr(plain) + `,` + jsonStr(dir) + `]}`
	resp, err := http.Post(ts.URL+"/api/phototime", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("phototime code = %d", resp.StatusCode)
	}
	var out struct {
		Times map[string]string `json:"times"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if got := out.Times[pic]; got != "2026-08-23 14:22" {
		t.Fatalf("pic time = %q, want 2026-08-23 14:22", got)
	}
	if _, ok := out.Times[plain]; ok {
		t.Fatalf("plain.jpg should have no time: %+v", out.Times)
	}
	if len(out.Times) != 1 {
		t.Fatalf("times = %+v", out.Times)
	}
}

func TestTimelineAPICollectsAndSortsRecursivePhotos(t *testing.T) {
	ts, dir := newTestServer(t)
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	newer := filepath.Join(dir, "newer.jpg")
	older := filepath.Join(sub, "older.jpg")
	writeTestJPEGWithEXIF(t, newer, "2026:08:23 14:22:00")
	writeTestJPEGWithEXIF(t, older, "2025:01:02 09:30:00")
	if err := os.WriteFile(filepath.Join(sub, "notes.txt"), []byte("not a photo"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Entries []struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			Taken string `json:"taken"`
		} `json:"entries"`
		Truncated  bool `json:"truncated"`
		NextOffset int  `json:"nextOffset"`
		Total      int  `json:"total"`
		HasMore    bool `json:"hasMore"`
	}
	if code := getJSON(t, ts.URL+"/api/timeline?p="+url.QueryEscape(dir)+"&limit=1", &out); code != 200 {
		t.Fatalf("timeline code = %d", code)
	}
	if out.Truncated {
		t.Fatal("small test tree should not be truncated")
	}
	if len(out.Entries) != 1 || out.Total != 2 || out.NextOffset != 1 || !out.HasMore {
		t.Fatalf("first page = %+v, want one of two photos and hasMore", out)
	}
	if out.Entries[0].Path != newer || out.Entries[0].Taken != "2026-08-23 14:22" {
		t.Fatalf("newest photo = %+v", out.Entries[0])
	}
	if code := getJSON(t, ts.URL+"/api/timeline?p="+url.QueryEscape(dir)+"&offset=1&limit=1", &out); code != 200 {
		t.Fatalf("timeline second page code = %d", code)
	}
	if len(out.Entries) != 1 || out.Total != 2 || out.NextOffset != 2 || out.HasMore {
		t.Fatalf("second page = %+v, want final one-photo page", out)
	}
	if out.Entries[0].Path != older || out.Entries[0].Taken != "2025-01-02 09:30" {
		t.Fatalf("older photo = %+v", out.Entries[0])
	}
}
