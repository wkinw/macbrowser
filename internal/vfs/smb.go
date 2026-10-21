package vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hirochachacha/go-smb2"
)

const attrHiddenFlag = 0x2

type smbPool struct {
	mu    sync.Mutex
	conns map[string]*smbConn
}

type smbConn struct {
	mu     sync.Mutex
	conn   Connection
	sess   *smb2.Session
	shares map[string]*smb2.Share
}

func newSMBPool() *smbPool {
	return &smbPool{conns: map[string]*smbConn{}}
}

func (p *smbPool) get(conn Connection) *smbConn {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.conns[conn.Name]
	if !ok || c.conn != conn {
		if ok {
			go c.close()
		}
		c = &smbConn{conn: conn, shares: map[string]*smb2.Share{}}
		p.conns[conn.Name] = c
	}
	return c
}

func (p *smbPool) drop(name string) {
	p.mu.Lock()
	c, ok := p.conns[name]
	delete(p.conns, name)
	p.mu.Unlock()
	if ok {
		go c.close()
	}
}

func (p *smbPool) prune(keep map[string]Connection) {
	p.mu.Lock()
	var dead []*smbConn
	for name, c := range p.conns {
		if _, ok := keep[name]; !ok {
			dead = append(dead, c)
			delete(p.conns, name)
		}
	}
	p.mu.Unlock()
	for _, c := range dead {
		go c.close()
	}
}

func (c *smbConn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess != nil {
		c.sess.Logoff()
		c.sess = nil
	}
	c.shares = map[string]*smb2.Share{}
}

func (c *smbConn) session() (*smb2.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionLocked()
}

func (c *smbConn) sessionLocked() (*smb2.Session, error) {
	if c.sess != nil {
		return c.sess, nil
	}
	port := c.conn.Port
	if port <= 0 {
		port = 445
	}
	addr := net.JoinHostPort(c.conn.Host, fmt.Sprint(port))
	nc, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("無法連線到 %s：%w", addr, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     c.conn.User,
			Password: c.conn.Password,
			Domain:   c.conn.Domain,
		},
	}
	sess, err := d.DialContext(ctx, nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("SMB 登入失敗（%s）：%w", c.conn.Host, err)
	}
	c.sess = sess
	return sess, nil
}

func (c *smbConn) share(name string) (*smb2.Share, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if sh, ok := c.shares[name]; ok {
		return sh, nil
	}
	sess, err := c.sessionLocked()
	if err != nil {
		return nil, err
	}
	sh, err := sess.Mount(name)
	if err != nil {
		return nil, fmt.Errorf("無法掛載共享「%s」：%w", name, err)
	}
	c.shares[name] = sh
	return sh, nil
}

func (c *smbConn) dropShare(name string) {
	c.mu.Lock()
	sh, ok := c.shares[name]
	delete(c.shares, name)
	c.mu.Unlock()
	if ok {
		go sh.Umount()
	}
}

type smbFS struct {
	conn Connection
	pool *smbPool
}

func (f *smbFS) Kind() string { return "smb" }

func (f *smbFS) AppleDoubleSidecar() bool { return true }

func (f *smbFS) Join(dir, name string) string { return path.Join(dir, name) }

func (f *smbFS) split(native string) (string, string) {
	native = strings.Trim(path.Clean("/"+strings.ReplaceAll(native, "\\", "/")), "/")
	if f.conn.Share != "" {
		return f.conn.Share, native
	}
	if native == "" {
		return "", ""
	}
	i := strings.IndexByte(native, '/')
	if i < 0 {
		return native, ""
	}
	return native[:i], native[i+1:]
}

func (f *smbFS) full(share, rel string) string {
	if share != f.conn.Share {
		return rel
	}
	return path.Join(f.conn.Base, rel)
}

func toSMB(p string) string {
	p = strings.ReplaceAll(p, "/", "\\")
	p = strings.Trim(p, "\\")
	if p == "" {
		return "."
	}
	return p
}

func isConnBroken(err error) bool {
	var te *smb2.TransportError
	if errors.As(err, &te) {
		return true
	}
	var ne *net.OpError
	return errors.As(err, &ne)
}

func (f *smbFS) run(share string, fn func(*smb2.Share) error) error {
	c := f.pool.get(f.conn)
	sh, err := c.share(share)
	if err != nil {
		return err
	}
	err = fn(sh)
	if err != nil && isConnBroken(err) {
		f.pool.drop(f.conn.Name)
		c2 := f.pool.get(f.conn)
		sh2, err2 := c2.share(share)
		if err2 != nil {
			return err2
		}
		return fn(sh2)
	}
	return err
}

func (f *smbFS) List(dir string) ([]Entry, error) {
	share, rel := f.split(dir)
	if share == "" {
		return f.listShares()
	}
	full := toSMB(f.full(share, rel))
	var fis []fs.FileInfo
	err := f.run(share, func(sh *smb2.Share) error {
		var e error
		fis, e = sh.ReadDir(full)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(fis))
	for _, fi := range fis {
		e := Entry{Name: fi.Name(), Dir: fi.IsDir(), Size: fi.Size(), Mod: fi.ModTime()}
		e.Hidden = hiddenName(fi.Name()) || smbHidden(fi)
		e.Thumb = !e.Dir && CanThumb(e.Name)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func smbHidden(fi fs.FileInfo) bool {
	if st, ok := fi.(*smb2.FileStat); ok {
		return st.FileAttributes&attrHiddenFlag != 0
	}
	return false
}

func (f *smbFS) listShares() ([]Entry, error) {
	sess, err := f.pool.get(f.conn).session()
	if err != nil {
		return nil, err
	}
	names, err := sess.ListSharenames()
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := make([]Entry, 0, len(names))
	for _, n := range names {
		if strings.HasSuffix(n, "$") || n == "IPC$" {
			continue
		}
		out = append(out, Entry{
			Name: n,
			Dir:  true,
			Path: "smb://" + f.conn.Name + "/" + n,
			Mod:  time.Now(),
		})
	}
	return out, nil
}

func (f *smbFS) Open(p string) (io.ReadSeekCloser, error) {
	share, rel := f.split(p)
	full := toSMB(f.full(share, rel))
	var file *smb2.File
	err := f.run(share, func(sh *smb2.Share) error {
		var e error
		file, e = sh.Open(full)
		return e
	})
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (f *smbFS) Stat(p string) (Entry, error) {
	share, rel := f.split(p)
	full := toSMB(f.full(share, rel))
	var fi fs.FileInfo
	err := f.run(share, func(sh *smb2.Share) error {
		var e error
		fi, e = sh.Stat(full)
		return e
	})
	if err != nil {
		return Entry{}, err
	}
	name := path.Base(strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/"))
	return Entry{
		Name:   name,
		Dir:    fi.IsDir(),
		Size:   fi.Size(),
		Mod:    fi.ModTime(),
		Hidden: smbHidden(fi),
		Thumb:  !fi.IsDir() && CanThumb(name),
	}, nil
}

func smbStreamSuffixes(name string) []string {
	alt := strings.ReplaceAll(name, ":", "\uF022")
	out := []string{":" + name, ":" + name + ":$DATA"}
	if alt != name {
		out = append(out, ":"+alt+":$DATA", ":"+alt)
	}
	return out
}

func (f *smbFS) Xattr(p, name string) ([]byte, error) {
	share, rel := f.split(p)
	if share == "" {
		return nil, ErrXattrNotSupported
	}
	full := toSMB(f.full(share, rel))
	var data []byte
	err := f.run(share, func(sh *smb2.Share) error {
		var last error = ErrXattrNotSupported
		for _, suffix := range smbStreamSuffixes(name) {
			b, e := sh.ReadFile(full + suffix)
			if e == nil && len(b) > 0 {
				data = b
				return nil
			}
			if e != nil {
				last = e
			}
		}
		return last
	})
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrXattrNotSupported
	}
	return data, nil
}

func (f *smbFS) WriteXattr(p, name string, data []byte) error {
	share, rel := f.split(p)
	if share == "" {
		return ErrXattrNotSupported
	}
	full := toSMB(f.full(share, rel))
	var last error = ErrXattrNotSupported
	ok := false
	for _, suffix := range smbStreamSuffixes(name) {
		err := f.run(share, func(sh *smb2.Share) error {
			file, err := sh.OpenFile(full+suffix, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
			if err != nil {
				return err
			}
			_, werr := file.Write(data)
			cerr := file.Close()
			if werr != nil {
				return werr
			}
			return cerr
		})
		if err == nil {
			ok = true
		} else {
			last = err
		}
	}
	if ok {
		return nil
	}
	return last
}

func (f *smbFS) WriteFile(p string, data []byte) error {
	share, rel := f.split(p)
	full := toSMB(f.full(share, rel))
	return f.run(share, func(sh *smb2.Share) error { return sh.WriteFile(full, data, 0o666) })
}

func (f *smbFS) WriteFrom(p string, r io.Reader) error {
	share, rel := f.split(p)
	full := toSMB(f.full(share, rel))
	return f.run(share, func(sh *smb2.Share) error {
		file, err := sh.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
		if err != nil {
			return err
		}
		_, cerr := io.Copy(file, r)
		if err := file.Close(); err != nil && cerr == nil {
			cerr = err
		}
		return cerr
	})
}

func (f *smbFS) Mkdir(p string) error {
	share, rel := f.split(p)
	full := toSMB(f.full(share, rel))
	return f.run(share, func(sh *smb2.Share) error { return sh.Mkdir(full, 0o777) })
}

func (f *smbFS) Remove(p string, recursive bool) error {
	share, rel := f.split(p)
	full := toSMB(f.full(share, rel))
	if full == "." {
		return fmt.Errorf("無法刪除共享根目錄")
	}
	return f.run(share, func(sh *smb2.Share) error {
		if recursive {
			return sh.RemoveAll(full)
		}
		return sh.Remove(full)
	})
}

func (f *smbFS) Rename(oldpath, newpath string) error {
	oldShare, oldRel := f.split(oldpath)
	newShare, newRel := f.split(newpath)
	if oldShare != newShare {
		return fmt.Errorf("無法跨共享重新命名")
	}
	oldFull := toSMB(f.full(oldShare, oldRel))
	newFull := toSMB(f.full(newShare, newRel))
	return f.run(oldShare, func(sh *smb2.Share) error { return sh.Rename(oldFull, newFull) })
}
