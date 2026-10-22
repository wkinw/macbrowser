package macmeta

import (
	"fmt"
	"io"

	"howett.net/plist"

	"macbrowser/internal/exif"
)

const RatingAttr = "com.apple.metadata:kMDItemStarRating"

func BuildRating(stars int) ([]byte, error) {
	if stars < 0 || stars > 5 {
		return nil, fmt.Errorf("無效的星等 %d", stars)
	}
	return plist.Marshal(uint64(stars), plist.BinaryFormat)
}

func ParseRating(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	var u uint64
	if _, err := plist.Unmarshal(data, &u); err == nil {
		if u > 5 {
			u = 5
		}
		return int(u)
	}
	var f float64
	if _, err := plist.Unmarshal(data, &f); err == nil {
		if f <= 0 {
			return 0
		}
		if f > 5 {
			f = 5
		}
		return int(f + 0.5)
	}
	return 0
}

func ResolveRating(fs Source, dir, name string, hasAD bool) (int, bool) {
	full := fs.Join(dir, name)
	if data, err := fs.Xattr(full, RatingAttr); err == nil {
		return ParseRating(data), true
	}
	if hasAD {
		if f, err := fs.Open(fs.Join(dir, "._"+name)); err == nil {
			data, rerr := io.ReadAll(io.LimitReader(f, 4<<20))
			f.Close()
			if rerr == nil {
				if ad := ParseAppleDouble(data); ad != nil {
					if raw, ok := ad.EAs[RatingAttr]; ok {
						return ParseRating(raw), true
					}
				}
			}
		}
	}
	return 0, false
}

func SetRating(fs Writer, dir, name string, stars int) error {
	data, err := BuildRating(stars)
	if err != nil {
		return err
	}
	full := fs.Join(dir, name)
	forceAD := false
	if aw, ok := fs.(interface{ AppleDoubleSidecar() bool }); ok {
		forceAD = aw.AppleDoubleSidecar()
	}
	if err := fs.WriteXattr(full, RatingAttr, data); err == nil {
		ad := readAD(fs, dir, name)
		if forceAD || ad != nil {
			return writeADRating(fs, dir, name, stars, ad)
		}
		return nil
	}
	if err := writeADRating(fs, dir, name, stars, readAD(fs, dir, name)); err != nil {
		return fmt.Errorf("此位置無法儲存評等：%w", err)
	}
	return nil
}

func writeADRating(fs Writer, dir, name string, stars int, ad *AppleDouble) error {
	if ad == nil && stars == 0 && contentRating(fs, fs.Join(dir, name)) == 0 {
		return nil
	}
	fi := []byte(nil)
	comment := ""
	eas := map[string][]byte{}
	if ad != nil {
		fi = ad.FinderInfo
		comment = ad.Comment
		for k, v := range ad.EAs {
			eas[k] = v
		}
	}
	if len(fi) == 0 {
		if raw, err := fs.Xattr(fs.Join(dir, name), FinderInfoAttr); err == nil {
			fi = raw
		}
	}
	data, err := BuildRating(stars)
	if err != nil {
		return err
	}
	eas[RatingAttr] = data
	return fs.WriteFile(fs.Join(dir, "._"+name), BuildAppleDouble(fi, eas, comment))
}

func contentRating(fs Source, path string) int {
	f, err := fs.Open(path)
	if err != nil {
		return 0
	}
	buf, err := io.ReadAll(io.LimitReader(f, 256<<10))
	f.Close()
	if err != nil {
		return 0
	}
	return exif.Rating(buf)
}
