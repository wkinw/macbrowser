package macmeta

import (
	"bytes"
	"encoding/binary"
	"io"
	"sort"
	"strings"
	"testing"

	"howett.net/plist"

	"macbrowser/internal/vfs"
)

func buildAD(t *testing.T, entries map[int][]byte) []byte {
	t.Helper()
	ids := make([]int, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	hdr := 26 + len(ids)*12
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(adMagicAppleDouble))
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

func plistTags(t *testing.T, tags []string) []byte {
	t.Helper()
	data, err := plist.Marshal(tags, plist.BinaryFormat)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseUserTagsBinary(t *testing.T) {
	data := plistTags(t, []string{"Red\n6", "專案\n2", "plain"})
	ut := ParseUserTags(data)
	if ut == nil {
		t.Fatal("ParseUserTags returned nil")
	}
	if ut.Color != 6 {
		t.Fatalf("color = %d, want 6", ut.Color)
	}
	if len(ut.Names) != 3 || ut.Names[0] != "Red" || ut.Names[1] != "專案" || ut.Names[2] != "plain" {
		t.Fatalf("names = %v", ut.Names)
	}
}

func TestParseUserTagsXML(t *testing.T) {
	xml := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><array><string>Blue&#10;4</string><string>work</string></array></plist>`)
	ut := ParseUserTags(xml)
	if ut == nil || ut.Color != 4 || len(ut.Names) != 2 || ut.Names[0] != "Blue" {
		t.Fatalf("xml parse failed: %+v", ut)
	}
}

func TestParseUserTagsGarbage(t *testing.T) {
	if ut := ParseUserTags([]byte{0x00, 0x01, 0x02, 0xff}); ut != nil && ut.Color != 0 {
		t.Fatalf("garbage produced color %d", ut.Color)
	}
}

func TestFinderInfoColor(t *testing.T) {
	fi := make([]byte, 32)
	fi[9] = 3 << 1
	if c := ColorFromFinderInfo(fi); c != 3 {
		t.Fatalf("ColorFromFinderInfo = %d, want 3", c)
	}
	afp := make([]byte, 48)
	afp[0] = 1
	copy(afp[16:], fi)
	if c := ColorFromAfpInfo(afp); c != 3 {
		t.Fatalf("ColorFromAfpInfo = %d, want 3", c)
	}
	fi[9] = 0
	if c := ColorFromFinderInfo(fi); c != 0 {
		t.Fatalf("no color = %d, want 0", c)
	}
}

func TestParseAppleDouble(t *testing.T) {
	pl := plistTags(t, []string{"Green\n2"})
	nested := buildAD(t, map[int][]byte{
		3: []byte(UserTagsAttr),
		1: pl,
	})
	fi := make([]byte, 32)
	fi[9] = 7 << 1
	ad := ParseAppleDouble(buildAD(t, map[int][]byte{
		9: fi,
		2: nested,
		4: []byte("hello comment"),
	}))
	if ad == nil {
		t.Fatal("ParseAppleDouble returned nil")
	}
	if c := ColorFromFinderInfo(ad.FinderInfo); c != 7 {
		t.Fatalf("finderinfo color = %d, want 7", c)
	}
	if ad.Comment != "hello comment" {
		t.Fatalf("comment = %q", ad.Comment)
	}
	raw, ok := ad.EAs[UserTagsAttr]
	if !ok {
		t.Fatalf("tag EA not extracted, EAs = %v", ad.EAs)
	}
	ut := ParseUserTags(raw)
	if ut == nil || ut.Color != 2 || ut.Names[0] != "Green" {
		t.Fatalf("nested tags = %+v", ut)
	}
}

func TestParseAppleDoubleInvalid(t *testing.T) {
	if ad := ParseAppleDouble([]byte("not an appledouble file")); ad != nil {
		t.Fatalf("expected nil, got %+v", ad)
	}
	if ad := ParseAppleDouble(nil); ad != nil {
		t.Fatalf("expected nil for empty input")
	}
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

type fakeFS struct {
	xattrs      map[string][]byte
	files       map[string][]byte
	noXwf       bool
	rejectColon bool
	forceAD     bool
}

func (f *fakeFS) AppleDoubleSidecar() bool { return f.forceAD }

func (f *fakeFS) Join(dir, name string) string { return dir + "|" + name }

func (f *fakeFS) Xattr(path, name string) ([]byte, error) {
	if v, ok := f.xattrs[name+"@"+path]; ok {
		return v, nil
	}
	return nil, vfs.ErrXattrNotSupported
}

func (f *fakeFS) Open(path string) (io.ReadSeekCloser, error) {
	if v, ok := f.files[path]; ok {
		return nopCloser{bytes.NewReader(v)}, nil
	}
	return nil, vfs.ErrXattrNotSupported
}

func (f *fakeFS) WriteXattr(path, name string, data []byte) error {
	if f.noXwf || (f.rejectColon && strings.Contains(name, ":")) {
		return vfs.ErrXattrNotSupported
	}
	f.xattrs[name+"@"+path] = data
	return nil
}

func (f *fakeFS) WriteFile(path string, data []byte) error {
	f.files[path] = data
	return nil
}

func TestResolveFromXattr(t *testing.T) {
	fs := &fakeFS{
		xattrs: map[string][]byte{
			UserTagsAttr + "@dir|pic.jpg": plistTags(t, []string{"Red\n6"}),
		},
		files: map[string][]byte{},
	}
	lbl := Resolve(fs, "dir", "pic.jpg", false)
	if lbl == nil || lbl.Color != 6 {
		t.Fatalf("label = %+v, want color 6", lbl)
	}
}

func TestResolveFromAppleDouble(t *testing.T) {
	fi := make([]byte, 32)
	fi[9] = 5 << 1
	fs := &fakeFS{
		xattrs: map[string][]byte{},
		files: map[string][]byte{
			"dir|._pic.jpg": buildAD(t, map[int][]byte{9: fi}),
		},
	}
	lbl := Resolve(fs, "dir", "pic.jpg", true)
	if lbl == nil || lbl.Color != 5 {
		t.Fatalf("label = %+v, want color 5", lbl)
	}
	lbl = Resolve(fs, "dir", "pic.jpg", false)
	if lbl != nil {
		t.Fatalf("without hasAppleDouble should be nil, got %+v", lbl)
	}
}

func TestResolveNone(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}}
	if lbl := Resolve(fs, "dir", "pic.jpg", true); lbl != nil {
		t.Fatalf("expected nil, got %+v", lbl)
	}
}

func TestStripColorSuffix(t *testing.T) {
	cases := []struct {
		in    string
		name  string
		color int
	}{
		{"Red\n6", "Red", 6},
		{"專案\n2", "專案", 2},
		{"plain", "plain", 0},
		{"x\ny", "x\ny", 0},
		{"a\n9", "a\n9", 0},
	}
	for _, c := range cases {
		n, col := stripColorSuffix(c.in)
		if n != c.name || col != c.color {
			t.Fatalf("stripColorSuffix(%q) = %q,%d want %q,%d", c.in, n, col, c.name, c.color)
		}
	}
}

func TestBuildUserTags(t *testing.T) {
	data, err := BuildUserTags(6)
	if err != nil {
		t.Fatal(err)
	}
	ut := ParseUserTags(data)
	if ut == nil || ut.Color != 6 || len(ut.Names) != 1 || ut.Names[0] != "Red" {
		t.Fatalf("tags = %+v", ut)
	}
	data, err = BuildUserTags(0)
	if err != nil {
		t.Fatal(err)
	}
	if ut := ParseUserTags(data); ut != nil && ut.Color != 0 {
		t.Fatalf("clear tags color = %+v", ut)
	}
}

func TestSetLabelFinderInfoXattr(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}}
	if err := SetLabel(fs, "dir", "pic.jpg", 5); err != nil {
		t.Fatal(err)
	}
	raw := fs.xattrs[FinderInfoAttr+"@dir|pic.jpg"]
	if c := ColorFromFinderInfo(raw); c != 5 {
		t.Fatalf("FinderInfo color = %d, want 5", c)
	}
	if _, ok := fs.files["dir|._pic.jpg"]; ok {
		t.Fatal("should not create AppleDouble when xattr works")
	}
	lbl := Resolve(fs, "dir", "pic.jpg", false)
	if lbl == nil || lbl.Color != 5 {
		t.Fatalf("resolve = %+v, want 5", lbl)
	}
	if err := SetLabel(fs, "dir", "pic.jpg", 0); err != nil {
		t.Fatal(err)
	}
	if lbl := Resolve(fs, "dir", "pic.jpg", false); lbl != nil && lbl.Color != 0 {
		t.Fatalf("after clear = %+v", lbl)
	}
}

func TestSetLabelTagsRejectedWritesSidecar(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}, rejectColon: true}
	if err := SetLabel(fs, "dir", "pic.jpg", 6); err != nil {
		t.Fatal(err)
	}
	if c := ColorFromFinderInfo(fs.xattrs[FinderInfoAttr+"@dir|pic.jpg"]); c != 6 {
		t.Fatalf("FinderInfo color = %d, want 6", c)
	}
	ad := ParseAppleDouble(fs.files["dir|._pic.jpg"])
	if ad == nil || ParseUserTags(ad.EAs[UserTagsAttr]) == nil {
		t.Fatal("sidecar with tags expected when tags xattr write fails")
	}
	lbl := Resolve(fs, "dir", "pic.jpg", true)
	if lbl == nil || lbl.Color != 6 {
		t.Fatalf("resolve = %+v, want 6", lbl)
	}
}

func TestSetLabelForcedSidecar(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}, forceAD: true}
	if err := SetLabel(fs, "dir", "pic.jpg", 3); err != nil {
		t.Fatal(err)
	}
	if fs.xattrs[UserTagsAttr+"@dir|pic.jpg"] == nil {
		t.Fatal("xattr tags missing")
	}
	ad := ParseAppleDouble(fs.files["dir|._pic.jpg"])
	if ad == nil {
		t.Fatal("sidecar expected with forced AD")
	}
	if ut := ParseUserTags(ad.EAs[UserTagsAttr]); ut == nil || ut.Color != 3 {
		t.Fatalf("sidecar tags = %+v", ut)
	}
	if err := SetLabel(fs, "dir", "pic.jpg", 0); err != nil {
		t.Fatal(err)
	}
	if ad := ParseAppleDouble(fs.files["dir|._pic.jpg"]); ad == nil {
		t.Fatal("sidecar should be updated on clear")
	}
}

func TestSetLabelAppleDoubleFallback(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}, noXwf: true}
	if err := SetLabel(fs, "dir", "pic.jpg", 6); err != nil {
		t.Fatal(err)
	}
	adData, ok := fs.files["dir|._pic.jpg"]
	if !ok {
		t.Fatal("AppleDouble not created")
	}
	ad := ParseAppleDouble(adData)
	if ad == nil {
		t.Fatal("written AppleDouble unparsable")
	}
	if c := ColorFromFinderInfo(ad.FinderInfo); c != 6 {
		t.Fatalf("ad FinderInfo color = %d, want 6", c)
	}
	if ut := ParseUserTags(ad.EAs[UserTagsAttr]); ut == nil || ut.Color != 6 {
		t.Fatalf("ad tags = %+v", ut)
	}
	lbl := Resolve(fs, "dir", "pic.jpg", true)
	if lbl == nil || lbl.Color != 6 {
		t.Fatalf("resolve = %+v, want 6", lbl)
	}
	if err := SetLabel(fs, "dir", "pic.jpg", 0); err != nil {
		t.Fatal(err)
	}
	ad = ParseAppleDouble(fs.files["dir|._pic.jpg"])
	if ad == nil {
		t.Fatal("AppleDouble removed unexpectedly")
	}
	if c := ColorFromFinderInfo(ad.FinderInfo); c != 0 {
		t.Fatalf("after clear color = %d, want 0", c)
	}
	if _, ok := ad.EAs[UserTagsAttr]; ok {
		t.Fatal("UserTags EA should be dropped on clear")
	}
	if lbl := Resolve(fs, "dir", "pic.jpg", true); lbl != nil && lbl.Color != 0 {
		t.Fatalf("after clear resolve = %+v", lbl)
	}
}

func TestSetLabelPreservesAppleDoubleExtras(t *testing.T) {
	fi := make([]byte, 32)
	fi[9] = 2 << 1
	keep := []byte("keep-me")
	nested := buildAD(t, map[int][]byte{
		3: []byte("com.apple.quarantine"),
		1: keep,
	})
	fs := &fakeFS{
		xattrs: map[string][]byte{},
		files: map[string][]byte{
			"dir|._pic.jpg": buildAD(t, map[int][]byte{
				9: fi,
				4: []byte("my comment"),
				2: nested,
			}),
		},
		noXwf: true,
	}
	if err := SetLabel(fs, "dir", "pic.jpg", 3); err != nil {
		t.Fatal(err)
	}
	ad := ParseAppleDouble(fs.files["dir|._pic.jpg"])
	if ad == nil {
		t.Fatal("AppleDouble unparsable after update")
	}
	if c := ColorFromFinderInfo(ad.FinderInfo); c != 3 {
		t.Fatalf("color = %d, want 3", c)
	}
	if ad.Comment != "my comment" {
		t.Fatalf("comment lost: %q", ad.Comment)
	}
	if !bytes.Equal(ad.EAs["com.apple.quarantine"], keep) {
		t.Fatalf("quarantine EA lost: %v", ad.EAs)
	}
	if ut := ParseUserTags(ad.EAs[UserTagsAttr]); ut == nil || ut.Color != 3 {
		t.Fatalf("tags = %+v", ut)
	}
}

func TestSetLabelExistingADUpdatedWhenXattrWorks(t *testing.T) {
	fi := make([]byte, 32)
	fi[9] = 2 << 1
	fs := &fakeFS{
		xattrs: map[string][]byte{},
		files: map[string][]byte{
			"dir|._pic.jpg": buildAD(t, map[int][]byte{9: fi}),
		},
	}
	if err := SetLabel(fs, "dir", "pic.jpg", 4); err != nil {
		t.Fatal(err)
	}
	if c := ColorFromFinderInfo(fs.xattrs[FinderInfoAttr+"@dir|pic.jpg"]); c != 4 {
		t.Fatalf("xattr color = %d, want 4", c)
	}
	ad := ParseAppleDouble(fs.files["dir|._pic.jpg"])
	if ad == nil || ColorFromFinderInfo(ad.FinderInfo) != 4 {
		t.Fatalf("existing AppleDouble not updated: %+v", ad)
	}
}

func TestSetLabelFolderXattrOnly(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}, forceAD: true}
	if err := SetLabelNode(fs, "dir", "folder", 2, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := fs.files["dir|._folder"]; ok {
		t.Fatal("folders must not get an AppleDouble sidecar")
	}
	if ut := ParseUserTags(fs.xattrs[UserTagsAttr+"@dir|folder"]); ut == nil || ut.Color != 2 {
		t.Fatalf("tags = %+v", ut)
	}
	lbl := Resolve(fs, "dir", "folder", false)
	if lbl == nil || lbl.Color != 2 {
		t.Fatalf("resolve = %+v, want 2", lbl)
	}
}

func TestSetLabelFolderUnsupported(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}, noXwf: true}
	if err := SetLabelNode(fs, "dir", "folder", 3, true); err == nil {
		t.Fatal("folder label should fail without xattr support")
	}
	if len(fs.files) != 0 {
		t.Fatalf("no sidecar expected, got %v", fs.files)
	}
}

func TestSetLabelRejectsBadColor(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}}
	if err := SetLabel(fs, "dir", "pic.jpg", 8); err == nil {
		t.Fatal("color 8 should fail")
	}
	if err := SetLabel(fs, "dir", "pic.jpg", -1); err == nil {
		t.Fatal("color -1 should fail")
	}
}

func TestSetLabelClearWithoutADNoop(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}, noXwf: true}
	if err := SetLabel(fs, "dir", "pic.jpg", 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := fs.files["dir|._pic.jpg"]; ok {
		t.Fatal("clearing a label should not create an AppleDouble")
	}
}
