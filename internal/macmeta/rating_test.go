package macmeta

import "testing"

func TestRatingBuildParse(t *testing.T) {
	for s := 0; s <= 5; s++ {
		data, err := BuildRating(s)
		if err != nil {
			t.Fatal(err)
		}
		if got := ParseRating(data); got != s {
			t.Fatalf("roundtrip %d = %d", s, got)
		}
	}
	if _, err := BuildRating(6); err == nil {
		t.Fatal("stars 6 should fail")
	}
	if _, err := BuildRating(-1); err == nil {
		t.Fatal("stars -1 should fail")
	}
	if got := ParseRating([]byte("junk")); got != 0 {
		t.Fatalf("garbage = %d", got)
	}
}

func TestSetRatingXattr(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}}
	if err := SetRating(fs, "dir", "pic.jpg", 4); err != nil {
		t.Fatal(err)
	}
	if got, ok := ResolveRating(fs, "dir", "pic.jpg", false); got != 4 || !ok {
		t.Fatalf("resolve = %d/%v, want 4/true", got, ok)
	}
	if _, ok := fs.files["dir|._pic.jpg"]; ok {
		t.Fatal("should not create AppleDouble when xattr works")
	}
	if err := SetRating(fs, "dir", "pic.jpg", 0); err != nil {
		t.Fatal(err)
	}
	if got, ok := ResolveRating(fs, "dir", "pic.jpg", false); got != 0 || !ok {
		t.Fatalf("after clear = %d/%v", got, ok)
	}
}

func TestSetRatingSidecarFallback(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}, noXwf: true}
	if err := SetRating(fs, "dir", "pic.jpg", 5); err != nil {
		t.Fatal(err)
	}
	ad := ParseAppleDouble(fs.files["dir|._pic.jpg"])
	if ad == nil || ParseRating(ad.EAs[RatingAttr]) != 5 {
		t.Fatal("sidecar rating expected when xattr write fails")
	}
	if got, ok := ResolveRating(fs, "dir", "pic.jpg", true); got != 5 || !ok {
		t.Fatalf("resolve = %d/%v, want 5/true", got, ok)
	}
}

func TestSetRatingForcedSidecar(t *testing.T) {
	fs := &fakeFS{xattrs: map[string][]byte{}, files: map[string][]byte{}, forceAD: true}
	if err := SetRating(fs, "dir", "pic.jpg", 2); err != nil {
		t.Fatal(err)
	}
	ad := ParseAppleDouble(fs.files["dir|._pic.jpg"])
	if ad == nil || ParseRating(ad.EAs[RatingAttr]) != 2 {
		t.Fatal("forced sidecar rating expected")
	}
	if err := SetRating(fs, "dir", "pic.jpg", 0); err != nil {
		t.Fatal(err)
	}
	ad = ParseAppleDouble(fs.files["dir|._pic.jpg"])
	if ad == nil {
		t.Fatal("sidecar should be kept on clear")
	}
	if raw, ok := ad.EAs[RatingAttr]; !ok || ParseRating(raw) != 0 {
		t.Fatal("rating should be zeroed in sidecar on clear")
	}
}

func TestSetRatingClearXMPCreatesSidecar(t *testing.T) {
	fs := &fakeFS{
		xattrs: map[string][]byte{},
		files: map[string][]byte{
			"dir|pic.jpg": []byte("<xmp:Rating>1</xmp:Rating>"),
		},
		noXwf: true,
	}
	if err := SetRating(fs, "dir", "pic.jpg", 0); err != nil {
		t.Fatal(err)
	}
	ad := ParseAppleDouble(fs.files["dir|._pic.jpg"])
	if ad == nil {
		t.Fatal("sidecar expected to suppress embedded XMP rating")
	}
	if raw, ok := ad.EAs[RatingAttr]; !ok || ParseRating(raw) != 0 {
		t.Fatal("zero rating record expected in sidecar")
	}
	if got, ok := ResolveRating(fs, "dir", "pic.jpg", true); got != 0 || !ok {
		t.Fatalf("resolve = %d/%v, want 0/true", got, ok)
	}
}

func TestSetRatingKeepsSidecarExtras(t *testing.T) {
	fi := make([]byte, 32)
	fi[9] = 3 << 1
	tags := plistTags(t, []string{"Red\n6"})
	old := BuildAppleDouble(fi, map[string][]byte{UserTagsAttr: tags}, "hello")
	fs := &fakeFS{
		xattrs: map[string][]byte{},
		files:  map[string][]byte{"dir|._pic.jpg": old},
	}
	if err := SetRating(fs, "dir", "pic.jpg", 1); err != nil {
		t.Fatal(err)
	}
	ad := ParseAppleDouble(fs.files["dir|._pic.jpg"])
	if ad == nil {
		t.Fatal("sidecar missing")
	}
	if ColorFromFinderInfo(ad.FinderInfo) != 3 {
		t.Fatalf("FinderInfo color = %d, want 3", ColorFromFinderInfo(ad.FinderInfo))
	}
	if ut := ParseUserTags(ad.EAs[UserTagsAttr]); ut == nil || ut.Color != 6 {
		t.Fatalf("tags = %+v", ut)
	}
	if ParseRating(ad.EAs[RatingAttr]) != 1 {
		t.Fatal("rating not merged into sidecar")
	}
	if ad.Comment != "hello" {
		t.Fatalf("comment = %q", ad.Comment)
	}
}
