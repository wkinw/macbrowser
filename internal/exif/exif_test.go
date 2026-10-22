package exif

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func buildTIFF(t *testing.T, date string, ifd0Only bool) []byte {
	t.Helper()
	var b bytes.Buffer
	le := binary.LittleEndian
	value := append([]byte(date), 0)
	b.WriteString("II")
	binary.Write(&b, le, uint16(42))
	binary.Write(&b, le, uint32(8))
	if ifd0Only {
		binary.Write(&b, le, uint16(1))
		binary.Write(&b, le, uint16(0x0132))
		binary.Write(&b, le, uint16(2))
		binary.Write(&b, le, uint32(len(value)))
		binary.Write(&b, le, uint32(26))
		binary.Write(&b, le, uint32(0))
		b.Write(value)
		return b.Bytes()
	}
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

func wrapJPEG(t *testing.T, payload []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	out.Write([]byte{0xFF, 0xD8})
	if payload != nil {
		app1 := append([]byte("Exif\x00\x00"), payload...)
		out.Write([]byte{0xFF, 0xE1})
		var lb [2]byte
		binary.BigEndian.PutUint16(lb[:], uint16(len(app1)+2))
		out.Write(lb[:])
		out.Write(app1)
	}
	out.Write([]byte{0xFF, 0xD9})
	return out.Bytes()
}

func wrapPNG(t *testing.T, payload []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	var lb [4]byte
	binary.BigEndian.PutUint32(lb[:], uint32(len(payload)))
	out.Write(lb[:])
	out.WriteString("eXIf")
	out.Write(payload)
	out.Write([]byte{0, 0, 0, 0})
	out.Write([]byte{0, 0, 0, 0, 'I', 'E', 'N', 'D', 0, 0, 0, 0})
	return out.Bytes()
}

func TestTakenJPEG(t *testing.T) {
	tiff := buildTIFF(t, "2026:08:23 14:22:00", false)
	if got := Taken(wrapJPEG(t, tiff)); got != "2026-08-23 14:22" {
		t.Fatalf("Taken = %q", got)
	}
}

func TestTakenJPEGIFD0Fallback(t *testing.T) {
	tiff := buildTIFF(t, "2025:01:02 03:04:05", true)
	if got := Taken(wrapJPEG(t, tiff)); got != "2025-01-02 03:04" {
		t.Fatalf("Taken = %q", got)
	}
}

func TestTakenPNG(t *testing.T) {
	tiff := buildTIFF(t, "2024:12:31 23:59:58", false)
	if got := Taken(wrapPNG(t, tiff)); got != "2024-12-31 23:59" {
		t.Fatalf("Taken = %q", got)
	}
}

func TestTakenNone(t *testing.T) {
	if got := Taken(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	if got := Taken(wrapJPEG(t, nil)); got != "" {
		t.Fatalf("no exif = %q", got)
	}
	if got := Taken([]byte("not an image")); got != "" {
		t.Fatalf("garbage = %q", got)
	}
	if got := Taken(wrapJPEG(t, buildTIFF(t, "0000:00:00 00:00:00", false))); got != "" {
		t.Fatalf("zero date = %q", got)
	}
}

func TestRatingXMPElement(t *testing.T) {
	data := []byte("<rdf:RDF><rdf:Description><xmp:Rating>3</xmp:Rating></rdf:Description></rdf:RDF>")
	if got := Rating(data); got != 3 {
		t.Fatalf("Rating = %d, want 3", got)
	}
}

func TestRatingXMPAttribute(t *testing.T) {
	data := []byte("<rdf:Description xmlns:xmp='http://ns.adobe.com/xap/1.0/' xmp:Rating='5'/>")
	if got := Rating(data); got != 5 {
		t.Fatalf("Rating = %d, want 5", got)
	}
	if got := Rating([]byte("no rating here")); got != 0 {
		t.Fatalf("no rating = %d", got)
	}
	if got := Rating([]byte("<xmp:Rating>9</xmp:Rating>")); got != 0 {
		t.Fatalf("out of range = %d", got)
	}
	if got := Rating([]byte("<MicrosoftPhoto:Rating>2</MicrosoftPhoto:Rating>")); got != 2 {
		t.Fatalf("ms rating = %d", got)
	}
}

func TestRatingEXIFTag(t *testing.T) {
	var b bytes.Buffer
	le := binary.LittleEndian
	b.WriteString("II")
	binary.Write(&b, le, uint16(42))
	binary.Write(&b, le, uint32(8))
	binary.Write(&b, le, uint16(1))
	binary.Write(&b, le, uint16(0x4746))
	binary.Write(&b, le, uint16(3))
	binary.Write(&b, le, uint32(1))
	binary.Write(&b, le, uint16(4))
	binary.Write(&b, le, uint16(0))
	binary.Write(&b, le, uint32(0))
	if got := Rating(wrapJPEG(t, b.Bytes())); got != 4 {
		t.Fatalf("Rating = %d, want 4", got)
	}
}
