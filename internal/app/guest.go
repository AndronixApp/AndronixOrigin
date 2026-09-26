package app

// Commands that run inside the distro (the binary copies itself to
// /usr/local/bin/andronix): vnc start/stop, setup-user and welcome.

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/pkgmgr"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

func guestRelease() conf.Values {
	b, _ := os.ReadFile("/etc/andronix-release")
	return conf.Parse(b)
}

func home() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "/root"
}

var reSize = regexp.MustCompile(`^[0-9]{3,5}x[0-9]{3,5}$`)

// VNCStart is `vncserver-start [WxH] [display]`.
// VNC listens on localhost only unless lan is set (vncserver-start --lan):
// reaching the desktop from another device is an explicit opt-in.
func VNCStart(args []string, lan bool) error {
	size, num := "", 1
	if len(args) > 0 {
		size = args[0]
	}
	if len(args) > 1 {
		n, err := strconv.Atoi(strings.TrimPrefix(args[1], ":"))
		if err != nil {
			return ui.Errorf("Bad display", "'"+args[1]+"' is not a number.", "Try: vncserver-start 1920x1080 1")
		}
		num = n
	}
	if _, err := sys.LookPath("Xvnc"); err != nil {
		return ui.Errorf("VNC isn't installed", "This distro has no desktop yet.",
			"In Termux, run: andronix install "+guestRelease().Get("ANDRONIX_DISTRO")+" --de xfce")
	}
	if pidAlive(fmt.Sprintf("/tmp/.X%d-lock", num)) {
		ui.OK(fmt.Sprintf("The desktop is already running on :%d.", num))
		ui.Note(fmt.Sprintf("Connect your VNC viewer to localhost:%d. To stop it: vncserver-stop", num))
		return nil
	}
	os.Remove(fmt.Sprintf("/tmp/.X%d-lock", num))
	os.Remove(fmt.Sprintf("/tmp/.X11-unix/X%d", num))

	light := Profile("/") == "light"
	if size == "" && light && len(args) == 0 && !ui.Interactive {
		size = "1280x720"
	}
	if size == "" {
		var err error
		def := ""
		if light {
			def = "1280x720" // the light profile's default
		}
		size, err = ui.Choose("Pick a screen size", "Enter picks Auto, which fits your VNC viewer.", []ui.Option{
			{Label: "Auto (fits your viewer)", Value: ""}, {Label: "HD 1280x720", Value: "1280x720"},
			{Label: "Full HD 1920x1080", Value: "1920x1080"}, {Label: "QHD 2560x1440", Value: "2560x1440"},
			{Label: "Custom size", Value: "custom"}}, def)
		if err != nil {
			return err
		}
		if size == "custom" {
			if size, err = ui.Input("Screen size", "As WIDTHxHEIGHT, e.g. 1600x900", "1600x900", func(s string) error {
				if !reSize.MatchString(s) {
					return errors.New("use WIDTHxHEIGHT, e.g. 1600x900")
				}
				return nil
			}); err != nil {
				return err
			}
		}
	}
	if size != "" && !reSize.MatchString(size) {
		return ui.Errorf("Bad screen size", "'"+size+"' doesn't look like WIDTHxHEIGHT.", "Try: vncserver-start 1920x1080")
	}

	h := home()
	// Users made where useradd -m skipped nested skel files (Android 9)
	// have no session script: fill in what's missing.
	if _, err := os.Stat(filepath.Join(h, ".config/tigervnc/xstartup")); err != nil {
		syncSkel("/etc/skel", h, os.Getuid(), os.Getgid())
	}
	os.MkdirAll(filepath.Join(h, ".config"), 0o755) // TigerVNC's own migration needs it
	dir := filepath.Join(h, ".config/tigervnc")
	if st, err := os.Lstat(filepath.Join(h, ".vnc")); err == nil && st.IsDir() {
		dir = filepath.Join(h, ".vnc") // a real old ~/.vnc: TigerVNC moves it on this start
	}
	os.MkdirAll(dir, 0o755)
	pw := filepath.Join(dir, "passwd")
	if st, err := os.Stat(pw); err != nil || st.Size() == 0 {
		pass := os.Getenv("ANDRONIX_VNC_PASSWORD")
		if pass == "" {
			fmt.Println()
			p, err := ui.Password("Choose a desktop password", "Your VNC viewer asks for it. 6 to 8 characters.", 6, 8)
			if err != nil {
				return ui.Errorf("No password set", "VNC needs a password before it starts.", "Run vncserver-start again in a terminal.")
			}
			pass = p
		}
		if err := writeVNCPassword(pw, pass); err != nil {
			return ui.Errorf("Couldn't save the password", err.Error(), "Run vncserver-start again.")
		}
	}
	os.Chmod(pw, 0o600) // TigerVNC refuses a password file others can read

	listen := "yes"
	if lan {
		listen = "no"
	}
	// Extra X server options for troubleshooting, e.g.
	// ANDRONIX_VNC_ARGS="-extension MIT-SHM".
	extra := strings.Fields(os.Getenv("ANDRONIX_VNC_ARGS"))
	if light {
		extra = append([]string{"-depth", "16"}, extra...) // half the bandwidth and memory
	}
	logf := fmt.Sprintf("/tmp/andronix-vnc-%d.log", num)
	os.Remove(fmt.Sprintf("/tmp/andronix-session-%d.pid", os.Getuid()))
	// Xvnc and the session, started directly on every distro: the vncserver
	// wrappers differ (and TigerVNC 1.13+ Debian wrappers dropped
	// -xstartup), so they aren't used.
	out, err := startXvnc(num, size, listen, pw, filepath.Join(dir, "xstartup"), logf, extra)
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 4 {
			lines = lines[len(lines)-4:]
		}
		return ui.Errorf("The desktop didn't start", strings.Join(lines, " "),
			"Run vncserver-stop, then vncserver-start again. Log: "+logf)
	}
	// The session can die while Xvnc keeps serving an empty screen; check
	// before saying it's running.
	if dead, tail := sessionDied(filepath.Join(dir, "xstartup")); dead {
		return ui.Errorf("The desktop session stopped", "The VNC server is up, but the desktop inside it quit right away: "+tail,
			"Run vncserver-stop, then vncserver-start. If it happens again, send us "+sessionLog()+" (Discord or email).")
	}
	s := size
	if s == "" {
		s = "auto"
	}
	reach := "this phone only"
	if lan {
		reach = "your whole network (--lan)"
	}
	fmt.Println()
	fmt.Print(ui.Box(ui.BoxOK, "Desktop is running",
		ui.KV("Address", fmt.Sprintf("localhost:%d", num)), ui.KV("Port", strconv.Itoa(5900+num)), ui.KV("Size", s),
		ui.KV("Reachable", reach), "",
		"Open a VNC viewer and connect to the address above.",
		"Keep this Termux session open while you use the desktop: closing it (or typing exit) stops the desktop too.",
		"When you're done: vncserver-stop"))
	fmt.Println()
	return nil
}

func sessionLog() string { return fmt.Sprintf("/tmp/andronix-session-%d.log", os.Getuid()) }

// sessionDied waits up to ~8 s for xstartup to record its pid, then
// checks the session is still alive. Returns the log's last lines if not.
func sessionDied(xstartup string) (bool, string) {
	if b, _ := os.ReadFile(xstartup); !strings.Contains(string(b), "andronix-session") {
		return false, "" // an older xstartup without a pid file: don't guess
	}
	pidf := fmt.Sprintf("/tmp/andronix-session-%d.pid", os.Getuid())
	var pid string
	for i := 0; i < 16; i++ {
		if b, err := os.ReadFile(pidf); err == nil {
			pid = strings.TrimSpace(string(b))
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if pid == "" {
		return false, "" // an older xstartup that doesn't write a pid: don't guess
	}
	time.Sleep(4 * time.Second)
	st, err := os.ReadFile("/proc/" + pid + "/stat")
	if err == nil {
		if f := strings.Fields(string(st)); len(f) > 2 && f[2] != "Z" {
			return false, ""
		}
	}
	b, _ := os.ReadFile(sessionLog())
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return true, strings.Join(lines, " / ")
}

// startXvnc runs Xvnc and the session directly, detached, and waits for
// the display socket.
func startXvnc(num int, size, listen, passwd, xstartup, logf string, extra []string) ([]byte, error) {
	// Xvnc only creates this folder as root; the desktop runs as the user.
	if os.MkdirAll("/tmp/.X11-unix", 0o1777) == nil {
		os.Chmod("/tmp/.X11-unix", os.ModeSticky|0o777)
	}
	lg, err := os.Create(logf)
	if err != nil {
		return nil, err
	}
	defer lg.Close()
	local := "1"
	if listen == "no" {
		local = "0"
	}
	args := []string{fmt.Sprintf(":%d", num), "-desktop", "remote-desktop", "-localhost=" + local,
		"-rfbport", strconv.Itoa(5900 + num), "-rfbauth", passwd, "-SecurityTypes", "VncAuth", "-depth", "24"}
	if size != "" {
		args = append(args, "-geometry", size)
	}
	args = append(args, extra...)
	x := sys.Command("Xvnc", args...)
	x.Stdout, x.Stderr = lg, lg
	x.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := x.Start(); err != nil {
		return nil, err
	}
	sock := fmt.Sprintf("/tmp/.X11-unix/X%d", num)
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if _, err := os.Stat(sock); err != nil {
		x.Process.Kill()
		b, _ := os.ReadFile(logf)
		return b, fmt.Errorf("Xvnc didn't open display :%d", num)
	}
	s := sys.Command(xstartup)
	s.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=:%d", num))
	s.Stdout, s.Stderr = lg, lg
	s.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := s.Start(); err != nil {
		return nil, err
	}
	x.Process.Release()
	s.Process.Release()
	return nil, nil
}

func writeVNCPassword(path, pass string) error {
	cmd := sys.Command("vncpasswd", "-f")
	cmd.Stdin = strings.NewReader(pass + "\n" + pass + "\n")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return fmt.Errorf("vncpasswd failed: %v", err)
	}
	return os.WriteFile(path, out, 0o600)
}

func pidAlive(lock string) bool {
	b, err := os.ReadFile(lock)
	if err != nil {
		return false
	}
	pid := strings.TrimSpace(string(b))
	st, err := os.ReadFile("/proc/" + pid + "/stat")
	if err != nil {
		return false
	}
	f := strings.Fields(string(st))
	if len(f) < 3 || f[2] == "Z" { // gone, or a zombie
		return false
	}
	// After Termux was killed the pid may belong to something else now.
	cmd, _ := os.ReadFile("/proc/" + pid + "/cmdline")
	return strings.Contains(strings.ToLower(string(cmd)), "vnc")
}

// VNCStop is `vncserver-stop [display]`.
func VNCStop(args []string) error {
	num := ""
	if len(args) > 0 {
		num = strings.TrimPrefix(args[0], ":")
	} else if ui.Interactive {
		v, err := ui.Choose("Which display should I stop?", "", []ui.Option{{Label: ":1 (the usual one)", Value: "1"},
			{Label: ":2", Value: "2"}, {Label: ":3", Value: "3"}}, "1")
		if err != nil {
			return err
		}
		num = v
	} else {
		num = "1"
	}
	if _, err := strconv.Atoi(num); err != nil {
		return ui.Errorf("Bad display", "'"+num+"' is not a display number.", "Try: vncserver-stop 1")
	}
	err := sys.Command("vncserver", "-kill", ":"+num).Run()
	// No wrapper (or upstream's, which can't -kill): stop Xvnc by its lock.
	if lock := "/tmp/.X" + num + "-lock"; err != nil && pidAlive(lock) {
		b, _ := os.ReadFile(lock)
		if pid, e := strconv.Atoi(strings.TrimSpace(string(b))); e == nil {
			if p, e := os.FindProcess(pid); e == nil && p.Signal(syscall.SIGTERM) == nil {
				err = nil
				time.Sleep(time.Second)
			}
		}
	}
	for _, p := range []string{"/tmp/.X" + num + "-lock", "/tmp/.X11-unix/X" + num} {
		os.Remove(p)
	}
	if err == nil {
		ui.OK("Stopped the desktop on :" + num + ".")
	} else {
		ui.OK("Nothing was running on :" + num + ". Cleaned up, ready for vncserver-start.")
	}
	return nil
}

var reUser = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// SetupUser is the first-boot setup: create a normal user with sudo and
// set the desktop password. Replaces the old blue `dialog` screens.
func SetupUser(preset string) error {
	if os.Getuid() != 0 {
		return ui.Errorf("Run this as root", "setup-user creates users, so it needs root.", "Start the distro with: andronix start <distro> --root")
	}
	rel := guestRelease()
	d, err := conf.LoadDistro(rel.Get("ANDRONIX_DISTRO"))
	if err != nil {
		return err
	}
	fam, err := pkgmgr.Get(d.Family)
	if err != nil {
		return err
	}
	// Scripted setup: setup-user --user NAME, with the passwords in
	// ANDRONIX_USER_PASSWORD and (optionally) ANDRONIX_VNC_PASSWORD.
	if preset != "" {
		pass := os.Getenv("ANDRONIX_USER_PASSWORD")
		if !reUser.MatchString(preset) || preset == "root" || pass == "" {
			return ui.Errorf("Can't set up that user", "setup-user --user needs a valid name and ANDRONIX_USER_PASSWORD.", "Example: ANDRONIX_USER_PASSWORD=secret andronix setup-user --user alex")
		}
		if err := setLocale(os.Getenv("ANDRONIX_TZ"), os.Getenv("ANDRONIX_KEYBOARD")); err != nil {
			return err
		}
		return createUser(fam, d, preset, pass, os.Getenv("ANDRONIX_VNC_PASSWORD"))
	}
	fmt.Println()
	fmt.Print(ui.Box(ui.BoxBrand, "Set up your user", "Pick a name and password for "+d.Label()+". You'll log in as this user; it can use sudo."))
	fmt.Println()
	name, err := ui.Input("Username", "Lower-case letters and numbers, e.g. alex", "alex", func(s string) error {
		if !reUser.MatchString(s) {
			return errors.New("use lower-case letters, numbers, - or _")
		}
		if s == "root" {
			return errors.New("pick a name other than root")
		}
		if _, _, _, ok := lookupUser("/", s); ok {
			return errors.New(s + " already exists")
		}
		return nil
	})
	if err != nil {
		return err
	}
	pass, err := ui.Password("Password for "+name, "Used for sudo and logging in.", 4, 0)
	if err != nil {
		return err
	}
	vncPass := ""
	if _, err := sys.LookPath("Xvnc"); err == nil {
		same := len(pass) >= 6 && len(pass) <= 8
		if same {
			same, _ = ui.Confirm("Use the same password for the desktop (VNC)?", "", true)
		}
		if same {
			vncPass = pass
		} else if vncPass, err = ui.Password("Desktop (VNC) password", "Your VNC viewer asks for it. 6 to 8 characters.", 6, 8); err != nil {
			return err
		}
	}
	// Time zone (from Android by default) and keyboard layout for the
	// desktop: the old Modded wizard asked for both.
	tz := currentTZ()
	tzIn, err := ui.Input("Time zone", "Detected from your phone: "+tz+". Press Enter to keep it.", tz, func(s string) error {
		if s == "" {
			return nil
		}
		if _, err := os.Stat("/usr/share/zoneinfo/" + s); err != nil {
			return errors.New("unknown time zone; use e.g. Europe/Berlin or Asia/Kolkata")
		}
		return nil
	})
	if err != nil {
		return err
	}
	kb, err := ui.Choose("Keyboard layout (for the desktop)", "", []ui.Option{
		{Label: "English (US)", Value: "us"}, {Label: "English (UK)", Value: "gb"}, {Label: "German", Value: "de"},
		{Label: "French", Value: "fr"}, {Label: "Spanish", Value: "es"}, {Label: "Italian", Value: "it"},
		{Label: "Portuguese (Brazil)", Value: "br"}, {Label: "Russian", Value: "ru"}, {Label: "Turkish", Value: "tr"},
	}, "us")
	if err != nil {
		return err
	}
	if tzIn == "" {
		tzIn = tz
	}
	if err := setLocale(tzIn, kb); err != nil {
		return err
	}
	return createUser(fam, d, name, pass, vncPass)
}

func currentTZ() string {
	if b, err := os.ReadFile("/etc/timezone"); err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b))
	}
	return "Etc/UTC"
}

// setLocale sets the time zone and the desktop keyboard layout (either
// may be ""). The layout is applied by xstartup with setxkbmap.
func setLocale(tz, kb string) error {
	if tz != "" && tz != currentTZ() {
		if _, err := os.Stat("/usr/share/zoneinfo/" + tz); err != nil {
			return ui.Errorf("Unknown time zone", "'"+tz+"' isn't a known time zone.", "Use a name like Europe/Berlin or Asia/Kolkata.")
		}
		os.Remove("/etc/localtime")
		os.Symlink("/usr/share/zoneinfo/"+tz, "/etc/localtime")
		os.WriteFile("/etc/timezone", []byte(tz+"\n"), 0o644)
	}
	if kb != "" {
		if !regexp.MustCompile(`^[a-z]{2,3}$`).MatchString(kb) {
			return ui.Errorf("Unknown keyboard layout", "'"+kb+"' isn't a layout code.", "Use a code like us, gb or de.")
		}
		os.MkdirAll("/etc/andronix", 0o755)
		os.WriteFile("/etc/andronix/keyboard", []byte(kb+"\n"), 0o644)
		if b, err := os.ReadFile("/etc/default/keyboard"); err == nil { // Debian family
			re := regexp.MustCompile(`(?m)^XKBLAYOUT=.*$`)
			os.WriteFile("/etc/default/keyboard", re.ReplaceAll(b, []byte(`XKBLAYOUT="`+kb+`"`)), 0o644)
		}
	}
	return nil
}

func createUser(fam *pkgmgr.Family, d *conf.Distro, name, pass, vncPass string) error {
	run := func(cmd string, stdin string) error {
		c := sys.Command("/bin/sh", "-c", cmd)
		if stdin != "" {
			c.Stdin = strings.NewReader(stdin)
		}
		out, err := c.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %v: %s", cmd, err, bytes.TrimSpace(out))
		}
		return nil
	}
	shell := d.Shell
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	if err := run(fam.AddUser(name, shell), ""); err != nil {
		return ui.Errorf("Couldn't create the user", err.Error(), "Run: andronix setup-user")
	}
	// Void's image has no /etc/shadow until pwconv runs.
	run("[ -f /etc/shadow ] || ! command -v pwconv >/dev/null || pwconv", "")
	if err := run("chpasswd", name+":"+pass+"\n"); err != nil {
		return ui.Errorf("Couldn't set the password", err.Error(), "Run: passwd "+name)
	}
	os.MkdirAll("/etc/sudoers.d", 0o755)
	// By user name: under proot the process's supplementary groups are
	// Android's, so sudo never sees the admin group.
	sudoNote := ""
	switch d.Sudo {
	case "none": // Manjaro ARM's sudo refuses under proot (docs/ports/manjaro.md)
		sudoNote = "sudo doesn't work on " + d.Name + " yet; for admin tasks run: andronix start " + d.ID + " --root"
	case "nopasswd": // Void: password rules fail under proot's PAM path
		os.WriteFile("/etc/sudoers.d/andronix", []byte(name+" ALL=(ALL:ALL) NOPASSWD: ALL\n"), 0o440)
		sudoNote = "sudo works without asking for a password here (a proot limit on " + d.Name + ")."
	default:
		os.WriteFile("/etc/sudoers.d/andronix", []byte(name+" ALL=(ALL:ALL) ALL\n"), 0o440)
	}
	uid, gid, h, _ := lookupUser("/", name)
	if h != "" {
		// useradd -m copies only the top of /etc/skel on some kernels
		// (Android 9, kernel 4.4, under proot): the user got no
		// ~/.config/tigervnc/xstartup and VNC couldn't start the desktop.
		if n := syncSkel("/etc/skel", h, atoi(uid), atoi(gid)); n > 0 {
			fmt.Fprintf(os.Stderr, "andronix: copied %d files from /etc/skel that useradd missed\n", n)
		}
	}
	if vncPass != "" && h != "" {
		dir := filepath.Join(h, ".config/tigervnc")
		os.MkdirAll(dir, 0o755)
		writeVNCPassword(filepath.Join(dir, "passwd"), vncPass)
	}
	os.MkdirAll("/etc/andronix", 0o755)
	os.WriteFile("/etc/andronix/user", []byte(name+"\n"), 0o644)
	os.Remove("/etc/andronix/firstboot")
	fmt.Println()
	ui.OK("Created " + name + ". Andronix logs you in as " + name + " from now on.")
	if sudoNote != "" {
		ui.Note(sudoNote)
	}
	return nil
}

// Welcome is the greeting on interactive logins. On a first boot it runs
// the user setup, then switches to the new user.
func Welcome() error {
	rel := guestRelease()
	if _, err := os.Stat("/etc/andronix/firstboot"); err == nil && os.Getuid() == 0 && ui.Interactive {
		if err := SetupUser(""); err == nil {
			if b, err := os.ReadFile("/etc/andronix/user"); err == nil {
				name := strings.TrimSpace(string(b))
				// Not su: after su from proot's fake root, sudo refuses. The
				// next start logs in as the user with proot -i.
				ui.Note("Type exit, then start " + rel.Get("ANDRONIX_NAME") + " again to log in as " + name + ".")
				return nil
			}
		}
	}
	if ui.Plain {
		return nil
	}
	fmt.Println()
	fmt.Println("  " + ui.Bold(ui.BrandS("andronix")+"."+" Welcome to Andronix | "+rel.Get("ANDRONIX_NAME")))
	if _, err := sys.LookPath("Xvnc"); err == nil {
		ui.Note("Desktop: type exit, then in Termux: andronix desktop " + rel.Get("ANDRONIX_DISTRO") + " (Termux:X11). Or VNC here: vncserver-start.")
	} else {
		ui.Note("Leave with: exit")
	}
	fmt.Println()
	return nil
}

// syncSkel copies what's in skel but missing from home into home (owned
// by uid:gid), and returns how many files it copied. Existing files are
// left alone.
func syncSkel(skel, home string, uid, gid int) int {
	n := 0
	filepath.Walk(skel, func(p string, fi os.FileInfo, err error) error {
		if err != nil || p == skel {
			return nil
		}
		rel, _ := filepath.Rel(skel, p)
		dst := filepath.Join(home, rel)
		if _, err := os.Lstat(dst); err == nil {
			return nil
		}
		switch {
		case fi.IsDir():
			os.MkdirAll(dst, fi.Mode().Perm())
		case fi.Mode()&os.ModeSymlink != 0:
			if t, err := os.Readlink(p); err == nil && os.Symlink(t, dst) == nil {
				n++
			}
			os.Lchown(dst, uid, gid)
			return nil
		case fi.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil || os.WriteFile(dst, b, fi.Mode().Perm()) != nil {
				return nil
			}
			os.Chmod(dst, fi.Mode().Perm()) // WriteFile honours the umask
			n++
		default:
			return nil
		}
		os.Chown(dst, uid, gid)
		return nil
	})
	return n
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }
