package compat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The table in the binary parses and every name it uses exists.
func TestDefaultTable(t *testing.T) {
	tab, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(tab.Rules) == 0 {
		t.Fatal("no rules")
	}
	for _, r := range tab.Rules {
		if r.Why == "" {
			t.Errorf("%s: say why", r.ID)
		}
	}
}

// Each recorded (or synthetic) phone gets exactly its expected rules.
func TestFixtures(t *testing.T) {
	tab, _ := Default()
	files, _ := filepath.Glob("testdata/*.json")
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		var fx struct {
			Distro, Family, DE string
			Probe              Probe
			Expect             []string
		}
		if err := json.Unmarshal(b, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		got := PlanFor(tab.Match(Facts{Probe: &fx.Probe, Distro: fx.Distro, Family: fx.Family, DE: fx.DE})).IDs
		if !reflect.DeepEqual(got, fx.Expect) && !(len(got) == 0 && len(fx.Expect) == 0) {
			t.Errorf("%s: rules %v, want %v", filepath.Base(f), got, fx.Expect)
		}
	}
}

// Rules with probe conditions wait for the probe; kernel rules don't.
func TestBeforeProbe(t *testing.T) {
	tab, _ := Default()
	p := PlanFor(tab.Match(Facts{Probe: &Probe{Kernel: "4.4.302", SDK: 28, Phantom: "off"}, Distro: "void", Family: "xbps", DE: "none"}))
	if !reflect.DeepEqual(p.IDs, []string{"old-syscall-order"}) || p.Env["PROOT_NO_SECCOMP"] != "1" {
		t.Errorf("before the probe on 4.4 with no desktop: %v %v", p.IDs, p.Env)
	}
	if p := PlanFor(tab.Match(Facts{Probe: &Probe{}})); len(p.IDs) != 0 {
		t.Errorf("nothing known: %v", p.IDs)
	}
}

func TestParseRejects(t *testing.T) {
	for name, table := range map[string]string{
		"unknown message": `{"version":1,"rules":[{"id":"a","when":{},"do":{"warn":"nope"}}]}`,
		"unknown script":  `{"version":1,"rules":[{"id":"a","when":{},"do":{"pre_script":"rm-rf"}}]}`,
		"path in script":  `{"version":1,"rules":[{"id":"a","when":{},"do":{"pre_script":"../x"}}]}`,
		"unknown preload": `{"version":1,"rules":[{"id":"a","when":{},"do":{"preload":"evil"}}]}`,
		"unknown syscall": `{"version":1,"rules":[{"id":"a","when":{"probe":{"execve":["ok"]}},"do":{}}]}`,
		"repeated id":     `{"version":1,"rules":[{"id":"a","when":{},"do":{}},{"id":"a","when":{},"do":{}}]}`,
		"unknown field":   `{"version":1,"rules":[{"id":"a","when":{"kernel_less":"4.8"},"do":{}}]}`,
		"bad apt pin":     `{"version":1,"rules":[{"id":"a","when":{},"do":{"apt_pin":["systemd"]}}]}`,
	} {
		if _, err := Parse([]byte(table)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPlanMerge(t *testing.T) {
	yes := true
	rules := []Rule{
		{ID: "a", When: When{Desktop: &yes}, Do: Do{Env: map[string]string{"X": "1"}, SkipOptional: []string{"big"}, AptPin: []string{"systemd -1"}}},
		{ID: "b", Do: Do{Env: map[string]string{"X": "2", "Y": "1"}, Warn: "phantom_process", Refuse: "old_kernel_desktop"}},
	}
	p := PlanFor(rules)
	if strings.Join(p.EnvList(), " ") != "X=2 Y=1" || !p.SkipOptional["big"] || p.Refuse != "old_kernel_desktop" || len(p.AptPins) != 1 {
		t.Errorf("plan %+v", p)
	}
	tab := &Table{Rules: rules}
	if got := PlanFor(tab.Match(Facts{DE: "none"})).IDs; !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("desktop condition: %v", got)
	}
}

func TestSummaryAndBuckets(t *testing.T) {
	p := &Probe{Kernel: "4.14.356-perf+", RAMMB: 3500, Syscalls: map[string]string{"statx": "ok", "clone3": "SIGSYS", "open_tree": "EUNATCH"}}
	if p.Summary() != "open_tree:EUNATCH,clone3:SIGSYS" || p.KernelMajorMinor() != "4.14" || p.RAMBucket() != "3-4G" {
		t.Errorf("%s %s %s", p.Summary(), p.KernelMajorMinor(), p.RAMBucket())
	}
	if (&Probe{}).Summary() != "not-probed" {
		t.Error("empty probe")
	}
}

func TestParseLines(t *testing.T) {
	got := ParseLines("statx ok\nclone3 SIGSYS\nnoise here\nbogus ok\n")
	if !reflect.DeepEqual(got, map[string]string{"statx": "ok", "clone3": "SIGSYS"}) {
		t.Errorf("%v", got)
	}
}

func TestIsLineageBuild(t *testing.T) {
	if !isLineageBuild("lineage_violet-userdebug", "") || !isLineageBuild("", "lineage_violet-bp1a.250505.005") {
		t.Error("LineageOS flavor or display id not recognised")
	}
	if isLineageBuild("violet-user", "PKQ1.180904.001 V12.5.1.0") || isLineageBuild("sdk_gphone64_arm64-userdebug") {
		t.Error("stock build taken for LineageOS")
	}
}
