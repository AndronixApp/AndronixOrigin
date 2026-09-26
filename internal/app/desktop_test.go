package app

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The XFCE wallpaper defaults must parse (xfconfd drops a broken channel)
// and cover Termux:X11's portrait "builtin" monitor without zooming.
func TestXFCEWallpaperDefaults(t *testing.T) {
	root := t.TempDir()
	if err := setWallpaper(context.Background(), root, "xfce", nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "etc/xdg/xfce4/xfconf/xfce-perchannel-xml/xfce4-desktop.xml"))
	if err != nil {
		t.Fatal(err)
	}
	type prop struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
		Props []prop `xml:"property"`
	}
	var ch struct {
		Props []prop `xml:"property"`
	}
	if err := xml.Unmarshal(b, &ch); err != nil {
		t.Fatalf("not valid XML: %v", err)
	}
	styles := map[string]string{}
	for _, mon := range ch.Props[0].Props[0].Props { // backdrop/screen0/monitor*
		for _, p := range mon.Props[0].Props { // workspace0/*
			if p.Name == "image-style" {
				styles[mon.Name] = p.Value
			}
		}
	}
	if styles["monitorbuiltin"] != "4" || styles["monitorVNC-0"] != "5" {
		t.Fatalf("image-style per monitor: %v", styles)
	}
	if !strings.Contains(string(b), `name="rgba1"`) {
		t.Fatal("no background colour for the letterboxed monitor")
	}
}

// syncSkel fills in what useradd -m missed (nested files), without
// touching files the home already has.
func TestSyncSkel(t *testing.T) {
	skel, home := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(skel, ".config/tigervnc"), 0o700)
	os.WriteFile(filepath.Join(skel, ".config/tigervnc/xstartup"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(skel, ".bashrc"), []byte("skel\n"), 0o644)
	os.WriteFile(filepath.Join(home, ".bashrc"), []byte("mine\n"), 0o644)
	os.MkdirAll(filepath.Join(home, ".config"), 0o700) // what useradd left
	if n := syncSkel(skel, home, os.Getuid(), os.Getgid()); n != 1 {
		t.Fatalf("copied %d files, want 1", n)
	}
	st, err := os.Stat(filepath.Join(home, ".config/tigervnc/xstartup"))
	if err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("xstartup: %v %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".bashrc")); string(b) != "mine\n" {
		t.Fatal("overwrote an existing file")
	}
}
