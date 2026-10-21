package vfs

import "testing"

func TestParentOf(t *testing.T) {
	cases := map[string]string{
		`C:\`:             "",
		`C:\a\b`:          `C:\a`,
		`C:\a`:            `C:\`,
		`\\srv\share`:     "",
		`\\srv\share\dir`: `\\srv\share`,
		`\\srv\share\a\b`: `\\srv\share\a`,
		`smb://nas`:       "",
		`smb://nas/a/b`:   `smb://nas/a`,
		`smb://nas/a`:     `smb://nas`,
		`smb://nas/a/b/`:  `smb://nas/a`,
		`D:\photos\2026`:  `D:\photos`,
		`D:\photos\`:      `D:\`,
	}
	for in, want := range cases {
		if got := ParentOf(in); got != want {
			t.Errorf("ParentOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanThumb(t *testing.T) {
	for _, n := range []string{"a.jpg", "b.JPEG", "c.PNG", "d.webp", "e.tiff", "f.gif", "g.bmp"} {
		if !CanThumb(n) {
			t.Errorf("CanThumb(%q) = false, want true", n)
		}
	}
	for _, n := range []string{"a.txt", "b.heic", "c.mp4", "noext", "d.svg"} {
		if CanThumb(n) {
			t.Errorf("CanThumb(%q) = true, want false", n)
		}
	}
}

func TestMimeFor(t *testing.T) {
	if got := MimeFor("a.jpg"); got != "image/jpeg" {
		t.Errorf("MimeFor(a.jpg) = %q", got)
	}
	if got := MimeFor("a.heic"); got != "image/heic" {
		t.Errorf("MimeFor(a.heic) = %q", got)
	}
}
