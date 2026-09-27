package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// The download cache (~/.andronix/cache) only holds what an install still
// needs: a finished install deletes its image, remove deletes whatever the
// distro left there (an interrupted download, Modded and Classic images
// too), and `andronix clean` empties it.

// cacheEntries are the cache files and folders of distro id: its images
// (<id>-..., legacy-<id>-...), their .part downloads and its layer cache.
func cacheEntries(cache, id string) []string {
	es, _ := os.ReadDir(cache)
	var out []string
	for _, e := range es {
		n := e.Name()
		if n == "layers-"+id || strings.HasPrefix(n, id+"-") || strings.HasPrefix(n, "legacy-"+id+"-") {
			out = append(out, filepath.Join(cache, n))
		}
	}
	return out
}

// removeCache deletes paths and returns the bytes freed.
func removeCache(paths []string) int64 {
	var freed int64
	for _, p := range paths {
		n := diskSize(p)
		if os.RemoveAll(p) == nil {
			freed += n
		}
	}
	return freed
}

func cacheSize(paths []string) int64 {
	var n int64
	for _, p := range paths {
		n += diskSize(p)
	}
	return n
}

func diskSize(p string) int64 {
	var n int64
	filepath.WalkDir(p, func(_ string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			if i, err := e.Info(); err == nil {
				n += i.Size()
			}
		}
		return nil
	})
	return n
}

// Clean is `andronix clean`: empties the download cache, except a download
// that's still running (written to in the last two minutes).
func Clean() error {
	cache := GetPaths().Cache
	es, _ := os.ReadDir(cache)
	var paths []string
	busy := 0
	for _, e := range es {
		p := filepath.Join(cache, e.Name())
		if recentlyWritten(p, 2*time.Minute) {
			busy++
			continue
		}
		paths = append(paths, p)
	}
	freed := removeCache(paths)
	if freed == 0 && busy == 0 {
		ui.OK("The download cache is already empty.")
		return nil
	}
	ui.OK("Freed " + ui.Bytes(freed) + " from " + ui.Tilde(cache) + ".")
	if busy > 0 {
		ui.Note("Kept a download that's still running. Run andronix clean again after it finishes.")
	}
	return nil
}

func recentlyWritten(p string, d time.Duration) bool {
	recent := false
	filepath.WalkDir(p, func(_ string, e fs.DirEntry, err error) error {
		if err == nil {
			if i, err := e.Info(); err == nil && time.Since(i.ModTime()) < d {
				recent = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	return recent
}
