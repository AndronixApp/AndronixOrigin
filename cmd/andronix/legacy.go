package main

import (
	"net/url"
	"path"
	"strings"
)

// legacyFiles maps the old Modded products (the -p file the app's old
// install command passed) to their archived editions (owner decision:
// legacy-*, rebuilt on current bases, not the new lineup).
var legacyFiles = map[string]string{
	"andronix_os": "legacy-ubuntu-xfce",
	"kde_ubuntu":  "legacy-ubuntu-kde",
	"manjaro":     "legacy-manjaro",
	"debian_mod":  "legacy-debian",
}

// legacyModded turns the old Modded command's arguments,
//
//	[install] -v 2 -h HASH -k KEY -e EMAIL -p FILE
//
// into "install --edition legacy-<...> --token 'k=&e=&h='" for the four old
// products, and "install <distro> --de <de> --modded --token ..." for
// <distro>_<de> names. It returns ok=false for anything else, so
// "andronix -v" stays the version.
func legacyModded(in []string) (out []string, ok bool) {
	rest := in
	if len(rest) > 0 && (rest[0] == "install" || rest[0] == "i") {
		rest = rest[1:]
	}
	v := map[string]string{}
	var extra []string
	for i := 0; i < len(rest); i++ {
		switch f := rest[i]; f {
		case "-v", "-h", "-k", "-e", "-p":
			if i+1 >= len(rest) {
				return nil, false
			}
			v[f[1:]] = rest[i+1]
			i++
		default:
			extra = append(extra, f)
		}
	}
	if v["v"] != "2" || v["h"] == "" || v["k"] == "" || v["e"] == "" || v["p"] == "" {
		return nil, false
	}
	name := strings.ToLower(path.Base(v["p"]))
	if i := strings.Index(name, ".tar"); i > 0 {
		name = name[:i]
	}
	tok := url.Values{"k": {v["k"]}, "e": {v["e"]}, "h": {v["h"]}}.Encode()
	if ed, found := legacyFiles[name]; found {
		return append([]string{"install", "--edition", ed, "--token", tok}, extra...), true
	}
	// Newer names: <distro>_<desktop>, e.g. ubuntu_xfce.
	d, de, cut := strings.Cut(strings.ReplaceAll(name, "-", "_"), "_")
	if !cut || d == "" || de == "" {
		return nil, false
	}
	return append([]string{"install", d, "--de", strings.TrimSuffix(de, "_modded"), "--modded", "--token", tok}, extra...), true
}
