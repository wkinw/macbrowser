//go:build windows

package secret

import (
	"strings"
	"testing"
)

func TestProtectRoundTrip(t *testing.T) {
	plain := "s3cret-密碼-!@#"
	enc, err := Protect(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, Prefix) {
		t.Fatalf("encrypted value = %q", enc)
	}
	if enc == plain {
		t.Fatal("value was not encrypted")
	}
	dec, err := Unprotect(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != plain {
		t.Fatalf("roundtrip = %q, want %q", dec, plain)
	}
}

func TestUnprotectRejectsPlaintext(t *testing.T) {
	if _, err := Unprotect("plain-password"); err == nil {
		t.Fatal("expected error for non-DPAPI value")
	}
}
