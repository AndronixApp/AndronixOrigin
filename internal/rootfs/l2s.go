package rootfs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// proot --link2symlink turns every hard link a package manager makes into
// a data file named .l2s.<name>NNNN plus symlinks to it, and those symlinks
// hold the HOST path of the rootfs (…/.andronix/distros/<id>/rootfs/…).
// Moved, restored elsewhere or exported, they dangle. These helpers make a
// rootfs portable (for backups) and repair one that was moved.

// reHostRoot matches the host prefix of a rootfs path inside a symlink,
// on any phone or box: <anything>/distros/<id>/rootfs
var reHostRoot = regexp.MustCompile(`^/.*/distros/[^/]+/rootfs(/|$)`)

// GuestTarget maps a symlink target that names a rootfs by its host path
// (this one or any other install's) to the guest path. ok=false if the
// target isn't such a path.
func GuestTarget(root, target string) (string, bool) {
	if root != "" && (target == root || strings.HasPrefix(target, root+"/")) {
		return "/" + strings.TrimPrefix(strings.TrimPrefix(target, root), "/"), true
	}
	if loc := reHostRoot.FindStringIndex(target); loc != nil {
		return "/" + strings.TrimPrefix(target[loc[1]:], "/"), true
	}
	return "", false
}

// IsL2S reports proot's link2symlink bookkeeping files.
func IsL2S(name string) bool { return strings.HasPrefix(filepath.Base(name), ".l2s.") }

// L2SData follows a symlink that points into link2symlink storage to the
// regular file holding the data. Returns "" if it doesn't, or dangles.
func L2SData(root, link string) string {
	cur := link
	for i := 0; i < 16; i++ {
		t, err := os.Readlink(cur)
		if err != nil {
			break
		}
		if g, ok := GuestTarget(root, t); ok {
			t = filepath.Join(root, g)
		} else if strings.HasPrefix(t, "/") {
			t = filepath.Join(root, t)
		} else {
			t = filepath.Join(filepath.Dir(cur), t)
		}
		cur = t
		st, err := os.Lstat(cur)
		if err != nil {
			return ""
		}
		if st.Mode().IsRegular() {
			if i == 0 && !IsL2S(cur) {
				return "" // an ordinary symlink to an ordinary file
			}
			return cur
		}
		if st.Mode()&os.ModeSymlink == 0 {
			return ""
		}
	}
	return ""
}

// RepairHostLinks rewrites symlinks that name a rootfs by a host path
// (another phone's, or this install's old location) so they work here.
// link2symlink links keep pointing into this rootfs's storage by its
// current host path, which is what proot expects; everything else
// becomes a guest path. Returns how many links changed.
func RepairHostLinks(root string) int {
	n := 0
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		t, err := os.Readlink(p)
		if err != nil || !strings.HasPrefix(t, "/") {
			return nil
		}
		g, ok := GuestTarget(root, t)
		if !ok {
			return nil
		}
		want := g
		if IsL2S(g) || strings.Contains(g, "/.l2s.") {
			want = filepath.Join(root, g) // proot resolves these on the host
		}
		if want != t {
			os.Remove(p)
			if os.Symlink(want, p) == nil {
				n++
			}
		}
		return nil
	})
	return n
}
