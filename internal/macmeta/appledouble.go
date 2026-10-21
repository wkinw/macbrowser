package macmeta

import (
	"strings"
)

const (
	adMagicAppleDouble = 0x00051607
	adMagicAppleSingle = 0x00051600

	entryDataFork     = 1
	entryResourceFork = 2
	entryRealName     = 3
	entryComment      = 4
	entryFinderInfo   = 9
)

type AppleDouble struct {
	FinderInfo   []byte
	ResourceFork []byte
	RealName     string
	Comment      string
	DataFork     []byte
	EAs          map[string][]byte
}

func be16(b []byte) int {
	return int(b[0])<<8 | int(b[1])
}

func be32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

type adPart struct {
	id   int
	data []byte
	size int
}

func parseADParts(b []byte) ([]adPart, int) {
	if len(b) < 26 {
		return nil, 0
	}
	magic := be32(b[0:4])
	if magic != adMagicAppleDouble && magic != adMagicAppleSingle {
		return nil, 0
	}
	num := be16(b[24:26])
	if num < 0 || num > 128 || 26+num*12 > len(b) {
		return nil, 0
	}
	parts := make([]adPart, 0, num)
	end := 26 + num*12
	for i := 0; i < num; i++ {
		off := 26 + i*12
		id := int(be32(b[off : off+4]))
		start := int(be32(b[off+4 : off+8]))
		length := int(be32(b[off+8 : off+12]))
		if start < 0 || length < 0 || start+length < start || start+length > len(b) {
			continue
		}
		parts = append(parts, adPart{id: id, data: b[start : start+length]})
		if start+length > end {
			end = start + length
		}
	}
	return parts, end
}

func ParseAppleDouble(b []byte) *AppleDouble {
	parts, _ := parseADParts(b)
	if parts == nil {
		return nil
	}
	ad := &AppleDouble{EAs: map[string][]byte{}}
	for _, p := range parts {
		switch p.id {
		case entryFinderInfo:
			ad.FinderInfo = p.data
		case entryResourceFork:
			ad.ResourceFork = p.data
		case entryRealName:
			ad.RealName = strings.TrimRight(string(p.data), "\x00")
		case entryComment:
			ad.Comment = strings.TrimRight(string(p.data), "\x00")
		case entryDataFork:
			ad.DataFork = p.data
		}
	}
	ad.extractEAs()
	return ad
}

func (ad *AppleDouble) extractEAs() {
	rf := ad.ResourceFork
	if len(rf) < 26 {
		return
	}
	for i := 0; i+26 <= len(rf); i++ {
		if be32(rf[i:i+4]) != adMagicAppleDouble && be32(rf[i:i+4]) != adMagicAppleSingle {
			continue
		}
		parts, size := parseADParts(rf[i:])
		if parts == nil || size <= 0 {
			continue
		}
		var name string
		var value []byte
		for _, p := range parts {
			switch p.id {
			case entryRealName:
				name = strings.TrimRight(string(p.data), "\x00")
			case entryDataFork:
				value = p.data
			case entryResourceFork:
				if value == nil {
					value = p.data
				}
			case entryFinderInfo:
				if len(p.data) >= 10 {
					ad.EAs[FinderInfoAttr] = p.data
				}
			}
		}
		if name != "" && value != nil {
			ad.EAs[name] = value
		}
		i += size - 1
	}
}
