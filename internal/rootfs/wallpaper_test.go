package rootfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWallpaper(t *testing.T) {
	out := os.Getenv("WALLPAPER_OUT")
	if out == "" {
		out = filepath.Join(t.TempDir(), "w.png")
	}
	if err := Wallpaper(out, 1280, 720); err != nil {
		t.Fatal(err)
	}
}
