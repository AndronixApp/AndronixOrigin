package app

import (
	"context"
	clog "github.com/charmbracelet/log"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/netx"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

const sha = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestResolve(t *testing.T) {
	var lastQuery url.Values
	answer := `{"url":"https://dl.andronix.app/bin/2.0.1/andronix-android-aarch64","sha256":"` + sha + `","size":5,"alt":["https://m.example/x","http://insecure/x"],"version":"2.0.1"}`
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastQuery = r.URL.Query()
		if r.URL.Path != "/v1/installer/resolve" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.WriteHeader(status)
		w.Write([]byte(answer))
	}))
	defer srv.Close()
	t.Setenv("ANDRONIX_API", srv.URL)
	t.Setenv("ANDRONIX_MIRROR", "")
	t.Setenv("ANDRONIX_NO_RESOLVE", "")

	r := resolve(context.Background(), "bin", url.Values{"flavor": {"android"}})
	if r == nil || r.SHA256 != sha || strings.Join(r.urls(), " ") != "https://dl.andronix.app/bin/2.0.1/andronix-android-aarch64 https://m.example/x" {
		t.Fatalf("resolve: %+v", r)
	}
	if lastQuery.Get("item") != "bin" || lastQuery.Get("flavor") != "android" || lastQuery.Get("v") != Version ||
		lastQuery.Get("arch") != string(sys.DetectArch()) || lastQuery.Get("t") != "" {
		t.Errorf("query %v (telemetry is off in tests: no t=1)", lastQuery)
	}
	for name, a := range map[string]string{
		"bad sha":    `{"url":"https://x/y","sha256":"abc"}`,
		"plain http": `{"url":"http://x/y","sha256":"` + sha + `"}`,
		"not json":   `<html>`,
	} {
		answer = a
		if r := resolve(context.Background(), "bin", url.Values{}); r != nil {
			t.Errorf("%s: want fallback, got %+v", name, r)
		}
	}
	for _, code := range []int{400, 404, 503} {
		status = code
		if r := resolve(context.Background(), "rootfs", url.Values{}); r != nil {
			t.Errorf("%d: want fallback", code)
		}
	}
	// An explicit mirror or ANDRONIX_NO_RESOLVE skips the API.
	status, answer = 200, `{"url":"https://x/y","sha256":"`+sha+`"}`
	t.Setenv("ANDRONIX_MIRROR", "https://mirror.example")
	if resolve(context.Background(), "bin", url.Values{}) != nil {
		t.Error("ANDRONIX_MIRROR should skip the resolver")
	}
}

func TestResolveTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()
	t.Setenv("ANDRONIX_API", srv.URL)
	t.Setenv("ANDRONIX_MIRROR", "")
	start := time.Now()
	if resolve(context.Background(), "bin", url.Values{}) != nil || time.Since(start) > 4*time.Second {
		t.Errorf("want a fallback within ~3 s, took %v", time.Since(start))
	}
}

// The built-in mirror's tarball is used only with its checksum.
func TestMirrorTarballNeedsChecksum(t *testing.T) {
	withSum := false
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			if !withSum {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(sha + "  debian-13-aarch64.tar.xz\n"))
			return
		}
		w.Header().Set("Content-Length", "7")
	}))
	defer mirror.Close()
	d, _ := conf.LoadDistro("debian")
	lg := &Logger{Logger: clog.New(io.Discard)}
	t.Setenv("ANDRONIX_MIRROR", mirror.URL) // also skips the resolver
	t.Setenv("ANDRONIX_ROOTFS", "")
	t.Setenv("ANDRONIX_SOURCE", "tarball")
	if s, err := pickSource(context.Background(), d, "aarch64", Paths{Cache: t.TempDir()}, lg); err == nil {
		t.Errorf("no checksum: want no tarball, got %+v", s)
	}
	withSum = true
	if s, err := pickSource(context.Background(), d, "aarch64", Paths{Cache: t.TempDir()}, lg); err != nil || s.sha != sha || s.kind != "tarball" {
		t.Errorf("with checksum: %+v %v", s, err)
	}
}

func TestBetaChannel(t *testing.T) {
	var q url.Values
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		w.WriteHeader(status)
		w.Write([]byte(`{"url":"https://dl.andronix.app/bin/beta/andronix-android-aarch64","sha256":"` + sha + `","version":"2.1.0-beta.1"}`))
	}))
	defer srv.Close()
	t.Setenv("ANDRONIX_API", srv.URL)
	t.Setenv("ANDRONIX_MIRROR", "https://mirror.example") // beta ignores the mirror override
	res, err := betaResolve(context.Background(), "k=K&e=E&h=H&f=x")
	if err != nil || res.Version != "2.1.0-beta.1" {
		t.Fatalf("beta: %+v %v", res, err)
	}
	if q.Get("channel") != "beta" || q.Get("k") != "K" || q.Get("h") != "H" || q.Get("f") != "" || q.Get("item") != "bin" || q.Get("flavor") == "" {
		t.Errorf("query %v", q)
	}
	for code, class := range map[int]string{403: "beta_refused", 401: "beta_refused", 404: "beta_none", 503: "download"} {
		status = code
		_, err := betaResolve(context.Background(), "k=K&e=E&h=H")
		if ue, ok := err.(*ui.UserError); !ok || ue.Class != class {
			t.Errorf("%d: %v", code, err)
		}
	}
	if _, err := betaResolve(context.Background(), ""); err == nil {
		t.Error("no token must be an error")
	}
}

func TestEarlyErr(t *testing.T) {
	early := &conf.Edition{ID: "fedora-xfce", Distro: "fedora", Desktop: "xfce", Early: true}
	if e, ok := earlyErr(&netx.StatusError{Code: 403}, early).(*ui.UserError); !ok || e.Title != "Early access: Premium" {
		t.Errorf("403 on an early edition: %v", e)
	}
	if earlyErr(&netx.StatusError{Code: 403}, &conf.Edition{ID: "debian-xfce"}) != nil || earlyErr(&netx.StatusError{Code: 500}, early) != nil || earlyErr(nil, nil) != nil {
		t.Error("only a refused early edition")
	}
}

// Modded downloads are checked against <file>.sha256 from products-api,
// behind the same token.
func TestModdedChecksum(t *testing.T) {
	var path, query string
	body, status := sha+"  ubuntu-26.04-xfce-modded-aarch64.tar.xz\n", 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// products-api answers 302 to the signed file, like the tarball.
		if r.URL.Path == "/signed/sum" {
			w.WriteHeader(status)
			w.Write([]byte(body))
			return
		}
		path, query = r.URL.Path, r.URL.RawQuery
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		http.Redirect(w, r, "/signed/sum", http.StatusFound)
	}))
	defer srv.Close()
	t.Setenv("ANDRONIX_API", srv.URL)
	d, _ := conf.LoadDistro("ubuntu")
	de, _ := conf.ResolveDesktop("xfce")
	lg := &Logger{Logger: clog.New(io.Discard)}
	s, _ := moddedSource(d, de, "aarch64", Paths{Cache: t.TempDir()}, "k=K&e=E&h=H", "ubuntu-xfce")
	if err := moddedChecksum(context.Background(), &s, lg, nil); err != nil || s.sha != sha {
		t.Fatalf("sha %q, %v", s.sha, err)
	}
	if path != "/v1/modded/download/ubuntu-26.04-xfce-modded-aarch64.tar.xz.sha256" || query != "e=E&h=H&k=K" {
		t.Errorf("asked %s?%s", path, query)
	}
	s.sha, status = "", 404
	if err := moddedChecksum(context.Background(), &s, lg, nil); err != nil || s.sha != "" {
		t.Errorf("404 (not served yet) must pass unchecked: %v", err)
	}
	status, body = 200, "<html>"
	if err := moddedChecksum(context.Background(), &s, lg, nil); err == nil {
		t.Error("a malformed checksum must be an error")
	}
	status = 401
	if err := moddedChecksum(context.Background(), &s, lg, nil); err == nil || !strings.Contains(err.Error(), "Download link expired") {
		t.Errorf("401: %v", err)
	}
	status = 403
	if err := moddedChecksum(context.Background(), &s, lg, nil); err == nil || !strings.Contains(err.Error(), "another edition") {
		t.Errorf("403: %v", err)
	}
}
