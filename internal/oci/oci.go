// Package oci pulls container images straight from a registry over HTTP:
// token, index → platform manifest → layer blobs. No Docker daemon. It's
// the fallback when our own rootfs tarball isn't available.
package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/AndronixApp/andronix-distros/internal/netx"
)

const accept = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

// Ref is a parsed image reference.
type Ref struct{ Host, Repo, Tag string }

// ParseRef applies Docker Hub's defaults (docker.io, library/, :latest).
func ParseRef(s string) Ref {
	r := Ref{Host: "registry-1.docker.io", Tag: "latest"}
	if i := strings.Index(s, "/"); i > 0 {
		first := s[:i]
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			r.Host = first
			s = s[i+1:]
		}
	}
	if r.Host == "docker.io" {
		r.Host = "registry-1.docker.io"
	}
	last := s[strings.LastIndex(s, "/")+1:]
	if i := strings.LastIndex(last, ":"); i >= 0 {
		r.Tag = last[i+1:]
		s = s[:len(s)-len(last)+i]
	}
	if r.Host == "registry-1.docker.io" && !strings.Contains(s, "/") {
		s = "library/" + s
	}
	r.Repo = s
	return r
}

// Layer is one blob of the image.
type Layer struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
}

// Image is a resolved image for one platform.
type Image struct {
	Ref    Ref
	Token  string
	Layers []Layer
	Total  int64
}

// Headers are the auth headers for blob downloads.
func (im *Image) Headers() map[string]string {
	if im.Token == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + im.Token}
}

// BlobURL is where a layer lives.
func (im *Image) BlobURL(l Layer) string {
	return "https://" + im.Ref.Host + "/v2/" + im.Ref.Repo + "/blobs/" + l.Digest
}

var reParam = regexp.MustCompile(`(\w+)="([^"]*)"`)

func token(ctx context.Context, r Ref) (string, error) {
	var url string
	if r.Host == "registry-1.docker.io" {
		url = "https://auth.docker.io/token?service=registry.docker.io&scope=repository:" + r.Repo + ":pull"
	} else {
		req, _ := http.NewRequestWithContext(ctx, "GET", "https://"+r.Host+"/v2/", nil)
		resp, err := netx.Client.Do(req)
		if err != nil {
			return "", err
		}
		resp.Body.Close()
		h := resp.Header.Get("Www-Authenticate")
		if h == "" {
			return "", nil
		}
		p := map[string]string{}
		for _, m := range reParam.FindAllStringSubmatch(h, -1) {
			p[strings.ToLower(m[1])] = m[2]
		}
		if p["realm"] == "" {
			return "", nil
		}
		url = p["realm"] + "?scope=repository:" + r.Repo + ":pull"
		if p["service"] != "" {
			url += "&service=" + p["service"]
		}
	}
	body, _, err := netx.Get(ctx, url, nil)
	if err != nil {
		return "", err
	}
	var t struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return "", err
	}
	if t.Token != "" {
		return t.Token, nil
	}
	return t.AccessToken, nil
}

type manifest struct {
	MediaType string `json:"mediaType"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
			Variant      string `json:"variant"`
		} `json:"platform"`
	} `json:"manifests"`
	Layers []Layer `json:"layers"`
}

func getManifest(ctx context.Context, r Ref, tok, ref string) (*manifest, error) {
	h := map[string]string{"Accept": accept}
	if tok != "" {
		h["Authorization"] = "Bearer " + tok
	}
	body, _, err := netx.Get(ctx, "https://"+r.Host+"/v2/"+r.Repo+"/manifests/"+ref, h)
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("bad manifest: %w", err)
	}
	return &m, nil
}

// ErrNoPlatform means the image has no build for this CPU.
type ErrNoPlatform struct{ Arch, Image string }

func (e *ErrNoPlatform) Error() string { return "no " + e.Arch + " build of " + e.Image }

// Resolve finds the layers of image for arch/variant (OCI names).
func Resolve(ctx context.Context, image, arch, variant string) (*Image, error) {
	r := ParseRef(image)
	tok, err := token(ctx, r)
	if err != nil {
		return nil, err
	}
	m, err := getManifest(ctx, r, tok, r.Tag)
	if err != nil {
		return nil, err
	}
	if len(m.Manifests) > 0 {
		digest := ""
		for _, e := range m.Manifests {
			p := e.Platform
			if p.Architecture == arch && p.OS == "linux" && (variant == "" || p.Variant == "" || p.Variant == variant) {
				digest = e.Digest
				break
			}
		}
		if digest == "" {
			return nil, &ErrNoPlatform{arch, image}
		}
		if m, err = getManifest(ctx, r, tok, digest); err != nil {
			return nil, err
		}
	}
	if len(m.Layers) == 0 {
		return nil, fmt.Errorf("image %s has no layers", image)
	}
	im := &Image{Ref: r, Token: tok, Layers: m.Layers}
	for _, l := range m.Layers {
		im.Total += l.Size
	}
	return im, nil
}
