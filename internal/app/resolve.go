package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/netx"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/telemetry"
)

// Downloads go through products-api's resolver first, so the API can pin,
// roll back or move a download without an installer release:
//
//	GET <API>/v1/installer/resolve?item=bin|rootfs&arch=&flavor=&distro=&ver=&v=[&t=1]
//	-> {url, sha256, size, alt[], version}
//
// Any failure (3 s timeout, 400/404/503, a bad answer) falls back to the
// built-in dl.andronix.app URL, so free installs never depend on the API.
// Whatever URL is used, its sha256 is checked. t=1 only with telemetry on.
// ANDRONIX_MIRROR or ANDRONIX_NO_RESOLVE=1 skip it (tests, mirrors).

// Resolved is the resolver's answer.
type Resolved struct {
	URL     string   `json:"url"`
	SHA256  string   `json:"sha256"`
	Size    int64    `json:"size"`
	Alt     []string `json:"alt"`
	Version string   `json:"version"`
}

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// resolve asks the API for item; nil means "use the built-in URL".
func resolve(ctx context.Context, item string, q url.Values) *Resolved {
	if os.Getenv("ANDRONIX_NO_RESOLVE") != "" || os.Getenv("ANDRONIX_MIRROR") != "" {
		return nil
	}
	r, _ := resolveQuery(ctx, item, q)
	return r
}

// resolveQuery is resolve without the fallback: the answer, or the error
// (a *netx.StatusError for an HTTP status), for callers that have no
// built-in URL (the beta channel).
func resolveQuery(ctx context.Context, item string, q url.Values) (*Resolved, error) {
	q.Set("item", item)
	q.Set("arch", string(sys.DetectArch()))
	q.Set("v", Version)
	if telemetry.Enabled() {
		q.Set("t", "1")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	b, _, err := netx.Get(ctx, API()+"/v1/installer/resolve?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var r Resolved
	if json.Unmarshal(b, &r) != nil || !sha256Re.MatchString(r.SHA256) {
		return nil, errors.New("the resolver's answer isn't valid")
	}
	if u, err := url.Parse(r.URL); err != nil || u.Scheme != "https" {
		return nil, errors.New("the resolver's URL isn't https")
	}
	return &r, nil
}

// urls is the answer's URL followed by its alternatives (https only).
func (r *Resolved) urls() []string {
	out := []string{r.URL}
	for _, a := range r.Alt {
		if u, err := url.Parse(a); err == nil && u.Scheme == "https" {
			out = append(out, a)
		}
	}
	return out
}

// binFlavor is this build's kind for item=bin.
func binFlavor() string {
	if runtime.GOOS == "android" {
		return "android"
	}
	return "linux"
}
