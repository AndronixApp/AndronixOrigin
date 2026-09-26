package app

import (
	"reflect"
	"testing"

	"github.com/AndronixApp/andronix-distros/internal/conf"
)

func TestResolvePackages(t *testing.T) {
	have := map[string]bool{"nodejs22": true, "nodejs22-npm": true, "python3": true}
	pkgs, skipped, missing := resolvePackages([]string{"nodejs|nodejs24|nodejs22", "nodejs-npm|nodejs24-npm|nodejs22-npm", "corepack?", "python3"}, have)
	if missing != "" || !reflect.DeepEqual(pkgs, []string{"nodejs22", "nodejs22-npm", "python3"}) || !reflect.DeepEqual(skipped, []string{"corepack"}) {
		t.Errorf("got %v %v %q", pkgs, skipped, missing)
	}
	if _, _, missing := resolvePackages([]string{"python3", "pipx"}, have); missing != "pipx" {
		t.Errorf("missing = %q, want pipx", missing)
	}
}

// Every pack loads, has a list for each family, and its files exist.
func TestPacksLoad(t *testing.T) {
	ids := conf.PackIDs()
	if len(ids) < 5 {
		t.Fatalf("packs: %v", ids)
	}
	for _, id := range ids {
		p, err := conf.LoadPack(id)
		if err != nil {
			t.Fatal(err)
		}
		if p.ID != id || p.Name == "" || p.Check == "" || p.DiskMB == 0 {
			t.Errorf("%s: incomplete: %+v", id, p)
		}
		for _, fam := range []string{"apt", "dnf", "pacman", "apk", "xbps"} {
			if len(p.Packages(fam)) == 0 {
				t.Errorf("%s: no PACK_PKGS_%s", id, fam)
			}
		}
		for _, f := range p.Files {
			if b, err := conf.PackFileData(f.Src); err != nil || len(b) == 0 || f.Mode == 0 {
				t.Errorf("%s: file %+v: %v", id, f, err)
			}
		}
	}
}
