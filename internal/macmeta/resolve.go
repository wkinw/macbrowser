package macmeta

import (
	"io"

	"macbrowser/internal/vfs"
)

type Source interface {
	Xattr(path, name string) ([]byte, error)
	Open(path string) (io.ReadSeekCloser, error)
	Join(dir, name string) string
}

func Resolve(fs Source, dir, name string, hasAppleDouble bool) *vfs.Label {
	full := fs.Join(dir, name)
	if data, err := fs.Xattr(full, UserTagsAttr); err == nil {
		if ut := ParseUserTags(data); ut != nil && ut.Color > 0 {
			return &vfs.Label{Color: ut.Color, Tags: ut.Names}
		}
	}
	if data, err := fs.Xattr(full, FinderInfoAttr); err == nil {
		if c := ColorFromFinderInfo(data); c != 0 {
			return &vfs.Label{Color: c}
		}
	}
	if data, err := fs.Xattr(full, AfpInfoStream); err == nil {
		if c := ColorFromAfpInfo(data); c != 0 {
			return &vfs.Label{Color: c}
		}
	}
	if hasAppleDouble {
		if f, err := fs.Open(fs.Join(dir, "._"+name)); err == nil {
			data, rerr := io.ReadAll(io.LimitReader(f, 4<<20))
			f.Close()
			if rerr == nil {
				if ad := ParseAppleDouble(data); ad != nil {
					if raw, ok := ad.EAs[UserTagsAttr]; ok {
						if ut := ParseUserTags(raw); ut != nil && ut.Color > 0 {
							return &vfs.Label{Color: ut.Color, Tags: ut.Names}
						}
					}
					if raw, ok := ad.EAs[FinderInfoAttr]; ok {
						if c := ColorFromFinderInfo(raw); c != 0 {
							return &vfs.Label{Color: c}
						}
					}
					if c := ColorFromFinderInfo(ad.FinderInfo); c != 0 {
						return &vfs.Label{Color: c}
					}
				}
			}
		}
	}
	return nil
}
