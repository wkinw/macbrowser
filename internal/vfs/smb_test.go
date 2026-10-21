package vfs

import "testing"

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
