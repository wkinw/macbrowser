package vfs

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestAFPInfoFinderInfoRoundTrip(t *testing.T) {
	fi := make([]byte, 32)
	fi[9] = 4
	wrapped, err := afpInfoWithFinderInfo(nil, fi)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrapped) != smbAFPInfoSize {
		t.Fatalf("AFP_AfpInfo length = %d, want %d", len(wrapped), smbAFPInfoSize)
	}
	if got := binary.LittleEndian.Uint32(wrapped[:4]); got != 0x41465000 {
		t.Fatalf("AFP signature = %#x", got)
	}
	if got := binary.LittleEndian.Uint32(wrapped[4:8]); got != 0x00000100 {
		t.Fatalf("AFP version = %#x", got)
	}
	gotFI, ok := finderInfoFromAFPInfo(wrapped)
	if !ok || !bytes.Equal(gotFI, fi) {
		t.Fatalf("unwrapped FinderInfo = %x, want %x", gotFI, fi)
	}

	existing := bytes.Repeat([]byte{0x5a}, smbAFPInfoSize)
	updated, err := afpInfoWithFinderInfo(existing, fi)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(updated[:smbAFPInfoOffset], existing[:smbAFPInfoOffset]) {
		t.Fatal("updating FinderInfo changed AFP_AfpInfo header fields")
	}
	if !bytes.Equal(updated[smbAFPInfoOffset+32:], existing[smbAFPInfoOffset+32:]) {
		t.Fatal("updating FinderInfo changed AFP_AfpInfo trailing fields")
	}
}

func TestAFPInfoRejectsInvalidFinderInfoLength(t *testing.T) {
	if _, err := afpInfoWithFinderInfo(nil, make([]byte, 31)); err == nil {
		t.Fatal("expected invalid FinderInfo length to be rejected")
	}
}

func TestSMBPathMapping(t *testing.T) {
	f := &smbFS{conn: Connection{Name: "nas", Host: "h", Share: "media", Base: "Photos"}}
	cases := []struct {
		native string
		share  string
		rel    string
		full   string
	}{
		{"", "media", "", "Photos"},
		{"sub", "media", "sub", "Photos/sub"},
		{"sub/pic.jpg", "media", "sub/pic.jpg", "Photos/sub/pic.jpg"},
	}
	for _, c := range cases {
		share, rel := f.split(c.native)
		if share != c.share || rel != c.rel {
			t.Errorf("split(%q) = %q,%q want %q,%q", c.native, share, rel, c.share, c.rel)
		}
		if got := f.full(share, rel); got != c.full {
			t.Errorf("full(%q,%q) = %q want %q", share, rel, got, c.full)
		}
	}
	if got := toSMB(f.full("media", "sub/pic.jpg")); got != `Photos\sub\pic.jpg` {
		t.Errorf("toSMB = %q", got)
	}
	if got := toSMB(""); got != "." {
		t.Errorf("toSMB(empty) = %q, want .", got)
	}
}

func TestSMBSharelessMapping(t *testing.T) {
	f := &smbFS{conn: Connection{Name: "srv", Host: "h"}}
	share, rel := f.split("")
	if share != "" || rel != "" {
		t.Errorf("split(empty) = %q,%q", share, rel)
	}
	share, rel = f.split("share1")
	if share != "share1" || rel != "" {
		t.Errorf("split(share1) = %q,%q", share, rel)
	}
	share, rel = f.split("share1/dir/file.txt")
	if share != "share1" || rel != "dir/file.txt" {
		t.Errorf("split deep = %q,%q", share, rel)
	}
	if got := f.full(share, rel); got != "dir/file.txt" {
		t.Errorf("full = %q", got)
	}
	if got := toSMB(f.full(share, rel)); got != `dir\file.txt` {
		t.Errorf("toSMB = %q", got)
	}
}

func TestSameStore(t *testing.T) {
	la := &Binding{FS: localFS{}, Path: `C:\a`, Kind: "local"}
	lb := &Binding{FS: localFS{}, Path: `D:\b`, Kind: "local"}
	if !la.SameStore(lb) {
		t.Error("local bindings should share a store")
	}
	s1 := &Binding{FS: &smbFS{conn: Connection{Name: "nas"}}, Path: "a", Kind: "smb"}
	s2 := &Binding{FS: &smbFS{conn: Connection{Name: "nas"}}, Path: "b", Kind: "smb"}
	s3 := &Binding{FS: &smbFS{conn: Connection{Name: "other"}}, Path: "a", Kind: "smb"}
	if !s1.SameStore(s2) {
		t.Error("same SMB connection should share a store")
	}
	if s1.SameStore(s3) {
		t.Error("different SMB connections must not share a store")
	}
	if la.SameStore(s1) || s1.SameStore(la) {
		t.Error("local and SMB must not share a store")
	}
}

type rfDst struct {
	readFromCalled bool
}

func (d *rfDst) Write(p []byte) (int, error) { return len(p), nil }

func (d *rfDst) ReadFrom(r io.Reader) (int64, error) {
	d.readFromCalled = true
	return 0, nil
}

type wtSrc struct {
	data          []byte
	off           int
	writeToCalled bool
}

func (s *wtSrc) Read(p []byte) (int, error) {
	if s.off >= len(s.data) {
		return 0, io.EOF
	}
	n := copy(p, s.data[s.off:])
	s.off += n
	return n, nil
}

func (s *wtSrc) WriteTo(w io.Writer) (int64, error) {
	s.writeToCalled = true
	return 0, nil
}

func TestCopyPlainBypassesReadFromWriteTo(t *testing.T) {
	d := &rfDst{}
	s := &wtSrc{data: []byte("hello")}
	n, err := copyPlain(d, s)
	if err != nil {
		t.Fatalf("copyPlain: %v", err)
	}
	if n != 5 {
		t.Fatalf("copied %d bytes, want 5", n)
	}
	if d.readFromCalled {
		t.Error("copyPlain must not use destination ReaderFrom (server-side copy)")
	}
	if s.writeToCalled {
		t.Error("copyPlain must not use source WriterTo (server-side copy)")
	}
	if !bytes.Equal(s.data, []byte("hello")) {
		t.Error("source mutated")
	}
}
