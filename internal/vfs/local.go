package vfs

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

type localFS struct{}

func (localFS) Kind() string { return "local" }

func (localFS) Join(dir, name string) string { return filepath.Join(dir, name) }

func (localFS) List(dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		e := Entry{Name: de.Name(), Dir: de.IsDir()}
		if info, err := de.Info(); err == nil {
			e.Size = info.Size()
			e.Mod = info.ModTime()
		}
		full := filepath.Join(dir, de.Name())
		e.Hidden = hiddenName(de.Name()) || isHidden(full)
		e.Thumb = !e.Dir && CanThumb(de.Name())
		out = append(out, e)
	}
	return out, nil
}

func (localFS) Open(p string) (io.ReadSeekCloser, error) { return os.Open(p) }

func (localFS) Stat(p string) (Entry, error) {
	st, err := os.Stat(p)
	if err != nil {
		return Entry{}, err
	}
	return Entry{
		Name:   filepath.Base(p),
		Dir:    st.IsDir(),
		Size:   st.Size(),
		Mod:    st.ModTime(),
		Hidden: hiddenName(filepath.Base(p)) || isHidden(p),
		Thumb:  !st.IsDir() && CanThumb(p),
	}, nil
}

func (localFS) Xattr(p, name string) ([]byte, error) { return readStream(p, name) }

func (localFS) WriteXattr(p, name string, data []byte) error { return writeStream(p, name, data) }

func (localFS) WriteFile(p string, data []byte) error {
	if err := os.WriteFile(p, data, 0o666); err != nil {
		return err
	}
	if strings.HasPrefix(filepath.Base(p), "._") {
		setHiddenAttr(p)
	}
	return nil
}

func (localFS) WriteFrom(p string, r io.Reader) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if strings.HasPrefix(filepath.Base(p), "._") {
		setHiddenAttr(p)
	}
	return nil
}

func (localFS) Mkdir(p string) error { return os.Mkdir(p, 0o777) }

func (localFS) Remove(p string, recursive bool) error {
	if recursive {
		return os.RemoveAll(p)
	}
	return os.Remove(p)
}

func (localFS) Rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

func hiddenName(name string) bool {
	if len(name) == 0 {
		return false
	}
	return name[0] == '.' || name[0] == '$'
}
