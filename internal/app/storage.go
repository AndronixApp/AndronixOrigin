package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// StorageDir is where `backup --to storage` writes: the phone's shared
// storage (Termux's ~/storage/shared), so backups survive uninstalling
// Termux and show up in a file manager. ANDRONIX_STORAGE overrides it.
func StorageDir() string {
	if d := os.Getenv("ANDRONIX_STORAGE"); d != "" {
		return d
	}
	return filepath.Join(sys.Home(), "storage/shared/Andronix/backups")
}

// ensureStorage makes StorageDir exist, asking Android for storage access
// (termux-setup-storage) the first time.
func ensureStorage() (string, error) {
	dir := StorageDir()
	shared := filepath.Dir(filepath.Dir(dir)) // ~/storage/shared
	if os.Getenv("ANDRONIX_STORAGE") == "" {
		if _, err := os.Stat(shared); err != nil {
			if _, err := sys.LookPath("termux-setup-storage"); err != nil {
				return "", ui.Errorf("No access to phone storage", "Termux can't see your phone's shared storage.",
					"Run termux-setup-storage, allow access, then try again.")
			}
			ui.Info("Android will ask to let Termux use your files. Tap Allow.")
			sys.Command("termux-setup-storage").Run()
			for i := 0; i < 60; i++ { // the grant arrives asynchronously
				if _, err := os.Stat(shared); err == nil {
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			if _, err := os.Stat(shared); err != nil {
				return "", ui.Errorf("No access to phone storage", "Storage access wasn't granted.",
					"Run termux-setup-storage, tap Allow, then run the same command again.")
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", ui.Errorf("Can't write to phone storage", err.Error(), "Check the phone has free space, then try again.")
	}
	return dir, nil
}

// backupFile is one backup found on the phone.
type backupFile struct {
	path string
	size int64
	mod  time.Time
}

// findBackups lists andronix-*.tar.gz in phone storage and in Termux's home.
func findBackups() []backupFile {
	var out []backupFile
	seen := map[string]bool{}
	for _, dir := range []string{StorageDir(), sys.Home()} {
		m, _ := filepath.Glob(filepath.Join(dir, "andronix-*.tar.gz"))
		for _, p := range m {
			if seen[p] {
				continue
			}
			seen[p] = true
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
				out = append(out, backupFile{p, st.Size(), st.ModTime()})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	return out
}

// pickBackup is `andronix restore` without a file: list what's there and,
// on a terminal, let the user pick one.
func pickBackup() (string, error) {
	list := findBackups()
	if len(list) == 0 {
		return "", ui.Errorf("No backups found", "There are no Andronix backups in "+ui.Tilde(StorageDir())+" or your Termux home.",
			"Make one with: andronix backup <distro> --to storage")
	}
	if !ui.Interactive {
		fmt.Println()
		ui.Section("Backups")
		for _, b := range list {
			ui.Row(ui.Tilde(b.path), fmt.Sprintf("%s, %s", ui.Bytes(b.size), b.mod.Format("2006-01-02 15:04")), 0)
		}
		fmt.Println()
		ui.Note("Restore one with: andronix restore <file>")
		return "", nil
	}
	var opts []ui.Option
	for _, b := range list {
		name := strings.TrimSuffix(filepath.Base(b.path), ".tar.gz")
		opts = append(opts, ui.Option{Label: fmt.Sprintf("%s  (%s, %s)", name, ui.Bytes(b.size), b.mod.Format("Jan 2 15:04")), Value: b.path})
	}
	return ui.Choose("Which backup?", "Newest first.", opts, list[0].path)
}
