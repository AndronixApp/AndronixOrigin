package sys

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// os/exec's lookup (exec.LookPath, and exec.Command with a bare name)
// calls faccessat2, which Android's app seccomp filter kills with SIGSYS.
// Outside this package, use LookPath, Command and CommandContext here.
func TestNoBareExec(t *testing.T) {
	root, _ := filepath.Abs("../..")
	re := regexp.MustCompile(`\bexec\.(LookPath|Command|CommandContext)\(`)
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			// This package wraps os/exec; the syscall probe calls it on
			// purpose; the rest aren't Go code of ours.
			switch rel {
			case "internal/sys", "tests/emulator/syscall-probe", ".git", "dist", "cache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for i, l := range strings.Split(string(b), "\n") {
			if re.MatchString(l) && !strings.HasPrefix(strings.TrimSpace(l), "//") {
				t.Errorf("%s:%d: use sys.%s: %s", rel, i+1, re.FindStringSubmatch(l)[1], strings.TrimSpace(l))
			}
		}
		return nil
	})
}
