// Package rootfs unpacks, configures and removes distro root filesystems.
package rootfs

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Extractor unpacks tar archives (plain, gzip, xz or zstd) into Root.
//
// Unlike `proot --link2symlink tar`, it needs no proot and leaves a
// relocatable tree: hard links become copies when Android refuses them,
// and every path is resolved inside Root, so an archive's symlinks can
// never make us write outside it.
type Extractor struct {
	Root string
	// Progress gets (compressed bytes read, total) across all archives.
	Progress func(cur, total int64)
	Total    int64
	// Layers turns on OCI whiteout handling (.wh.* files).
	Layers bool

	done     int64
	dirModes map[string]os.FileMode
	written  map[string]bool
	realDirs map[string]bool
	Warnings []string
}

func (x *Extractor) init() {
	if x.dirModes == nil {
		x.dirModes = map[string]os.FileMode{}
		x.realDirs = map[string]bool{}
	}
}

type countReader struct {
	r   io.Reader
	x   *Extractor
	cur int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.cur += int64(n)
	if c.x.Progress != nil && n > 0 {
		c.x.Progress(c.x.done+c.cur, c.x.Total)
	}
	return n, err
}

// ExtractFile unpacks one archive; call several times for image layers,
// then Finish.
func (x *Extractor) ExtractFile(p string) error {
	x.init()
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	st, _ := f.Stat()
	cr := &countReader{r: f, x: x}
	br := bufio.NewReaderSize(cr, 1<<20)
	magic, _ := br.Peek(6)
	var r io.Reader = br
	switch {
	case len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		gz, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		r = gz
	case len(magic) >= 6 && string(magic) == "\xfd7zXZ\x00":
		xr, err := xz.NewReader(br)
		if err != nil {
			return err
		}
		r = xr
	case len(magic) >= 4 && magic[0] == 0x28 && magic[1] == 0xb5 && magic[2] == 0x2f && magic[3] == 0xfd:
		zr, err := zstd.NewReader(br)
		if err != nil {
			return err
		}
		defer zr.Close()
		r = zr
	}
	x.written = map[string]bool{}
	err = x.extract(tar.NewReader(r))
	if st != nil {
		x.done += st.Size()
	}
	return err
}

func cleanName(n string) (string, bool) {
	n = path.Clean("/" + n)[1:]
	if n == "" || n == "." {
		return "", false
	}
	return n, true
}

func (x *Extractor) extract(tr *tar.Reader) error {
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name, ok := cleanName(h.Name)
		if !ok {
			continue
		}
		base := path.Base(name)
		dir := path.Dir(name)
		if x.Layers && strings.HasPrefix(base, ".wh.") {
			if err := x.whiteout(dir, base); err != nil {
				x.warn("whiteout %s: %v", name, err)
			}
			continue
		}
		if err := x.entry(tr, h, name); err != nil {
			x.warn("%s: %v", name, err)
		}
	}
}

func (x *Extractor) warn(f string, a ...any) {
	if len(x.Warnings) < 200 {
		x.Warnings = append(x.Warnings, fmt.Sprintf(f, a...))
	}
}

func (x *Extractor) whiteout(dir, base string) error {
	d, err := x.resolve(dir, true)
	if err != nil {
		return err
	}
	if base == ".wh..wh..opq" {
		// Opaque directory: hide everything lower layers put here.
		entries, _ := os.ReadDir(d)
		rel := strings.TrimPrefix(strings.TrimPrefix(d, x.Root), "/")
		for _, e := range entries {
			if !x.written[path.Join(rel, e.Name())] {
				removeAll(filepath.Join(d, e.Name()))
			}
		}
		return nil
	}
	target := strings.TrimPrefix(base, ".wh.")
	if target == "" || target == "." || target == ".." {
		return nil
	}
	return removeAll(filepath.Join(d, target))
}

func (x *Extractor) entry(tr *tar.Reader, h *tar.Header, name string) error {
	switch h.Typeflag {
	case tar.TypeDir:
		p, err := x.resolve(name, true)
		if err != nil {
			return err
		}
		if st, err := os.Lstat(p); err == nil && !st.IsDir() {
			removeAll(p)
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
		x.dirModes[p] = h.FileInfo().Mode().Perm() | 0o700
		x.realDirs[p] = true
		x.written[name] = true
		return nil
	case tar.TypeReg, tar.TypeRegA:
		p, err := x.parentThen(name)
		if err != nil {
			return err
		}
		removeAll(p)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		f.Close()
		os.Chmod(p, h.FileInfo().Mode()&os.ModePerm|0o600|(h.FileInfo().Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky)))
		os.Chtimes(p, h.ModTime, h.ModTime)
		x.written[name] = true
		return nil
	case tar.TypeSymlink:
		p, err := x.parentThen(name)
		if err != nil {
			return err
		}
		removeAll(p)
		delete(x.realDirs, p)
		x.written[name] = true
		return os.Symlink(h.Linkname, p)
	case tar.TypeLink:
		target, ok := cleanName(h.Linkname)
		if !ok {
			return errors.New("bad hard link")
		}
		src, err := x.resolve(target, true)
		if err != nil {
			return err
		}
		p, err := x.parentThen(name)
		if err != nil {
			return err
		}
		removeAll(p)
		x.written[name] = true
		// Android forbids hard links in app storage; copy instead.
		if err := os.Link(src, p); err == nil {
			return nil
		}
		return copyFile(src, p)
	default:
		// Device nodes and FIFOs can't be made without root; proot binds
		// the real /dev at launch.
		return nil
	}
}

// parentThen resolves the parent directory (creating it) and returns the
// path for the final component, which is never followed.
func (x *Extractor) parentThen(name string) (string, error) {
	dir, err := x.resolve(path.Dir(name), true)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, path.Base(name)), nil
}

// resolve maps an archive path to a host path inside Root, following
// symlinks the way the distro will see them (absolute links point at
// Root, ".." stops at Root).
func (x *Extractor) resolve(name string, followLast bool) (string, error) {
	x.init()
	return resolveIn(x.Root, name, followLast, x.realDirs)
}

func resolveIn(root, name string, followLast bool, cache map[string]bool) (string, error) {
	parts := strings.Split(name, "/")
	cur := ""
	hops := 0
	for i := 0; i < len(parts); i++ {
		c := parts[i]
		if c == "" || c == "." {
			continue
		}
		if c == ".." {
			cur = path.Dir(cur)
			if cur == "." || cur == "/" {
				cur = ""
			}
			continue
		}
		next := path.Join(cur, c)
		host := filepath.Join(root, next)
		last := i == len(parts)-1
		if last && !followLast || cache != nil && cache[host] {
			cur = next
			continue
		}
		st, err := os.Lstat(host)
		if err != nil || st.Mode()&os.ModeSymlink == 0 {
			if err == nil && st.IsDir() && cache != nil {
				cache[host] = true
			}
			cur = next
			continue
		}
		hops++
		if hops > 40 {
			return "", errors.New("too many symlinks")
		}
		link, err := os.Readlink(host)
		if err != nil {
			return "", err
		}
		rest := strings.Join(parts[i+1:], "/")
		if strings.HasPrefix(link, "/") {
			cur = ""
			parts = strings.Split(strings.TrimPrefix(link, "/")+"/"+rest, "/")
		} else {
			parts = strings.Split(link+"/"+rest, "/")
		}
		i = -1
	}
	return filepath.Join(root, cur), nil
}

// Finish applies directory permissions (deepest first).
func (x *Extractor) Finish() {
	var dirs []string
	for d := range x.dirModes {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		os.Chmod(d, x.dirModes[d])
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	if _, err := sys.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	out.Close()
	return os.Chtimes(dst, time.Now(), st.ModTime())
}

// removeAll deletes p even when it or its children are read-only.
func removeAll(p string) error {
	if _, err := os.Lstat(p); err != nil {
		return nil
	}
	if err := os.RemoveAll(p); err == nil {
		return nil
	}
	filepath.Walk(p, func(q string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			os.Chmod(q, 0o700)
		}
		return nil
	})
	return os.RemoveAll(p)
}

// Remove deletes a whole rootfs (or any tree), fixing permissions first.
func Remove(p string) error { return removeAll(p) }
