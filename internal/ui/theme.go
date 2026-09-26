// Package ui is the Andronix terminal look: colours, the wordmark, boxes,
// the step list (bubbletea) and prompts (huh). It adapts to 40-column
// portrait phones and has a plain mode for logs, CI and scripts.
package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Brand colours: logo orange, docs flame, app amber and deep orange, logo
// neutrals. Truecolor with 256- and 16-colour fallbacks.
var (
	Orange = lipgloss.CompleteColor{TrueColor: "#ff8b25", ANSI256: "208", ANSI: "3"}
	Flame  = lipgloss.CompleteColor{TrueColor: "#f5641d", ANSI256: "202", ANSI: "3"}
	Amber  = lipgloss.CompleteColor{TrueColor: "#ffbc57", ANSI256: "215", ANSI: "3"}
	Deep   = lipgloss.CompleteColor{TrueColor: "#c65c00", ANSI256: "166", ANSI: "3"}
	Grey   = lipgloss.CompleteColor{TrueColor: "#535353", ANSI256: "240", ANSI: "8"}
	Muted  = lipgloss.CompleteColor{TrueColor: "#96928c", ANSI256: "246", ANSI: "7"}
	Green  = lipgloss.CompleteColor{TrueColor: "#7ec86e", ANSI256: "114", ANSI: "2"}
	Red    = lipgloss.CompleteColor{TrueColor: "#ff5f56", ANSI256: "203", ANSI: "1"}
)

var (
	// Plain is one stable line per event: no colour, no animation. On
	// when stdout isn't a terminal, TERM=dumb, --plain or ANDRONIX_PLAIN.
	Plain bool
	// ASCII swaps Unicode glyphs and borders for ASCII (ANDRONIX_ASCII=1).
	ASCII bool
	// Interactive is true when we can ask questions (stdin is a terminal
	// and ANDRONIX_YES isn't set).
	Interactive bool
	// Yes answers every question with its default "yes".
	Yes bool
)

// Glyphs.
var (
	GOK, GFail, GSkip, GDot, GWarn, GArrow, GAsk, GSep = "✓", "✗", "–", "●", "!", "→", "?", "·"
)

// Init reads the environment. Call once, after flags are parsed.
func Init(forcePlain bool) {
	outTTY := term.IsTerminal(int(os.Stdout.Fd()))
	inTTY := term.IsTerminal(int(os.Stdin.Fd()))
	Plain = forcePlain || !outTTY || os.Getenv("TERM") == "dumb" || os.Getenv("ANDRONIX_PLAIN") != ""
	Yes = Yes || os.Getenv("ANDRONIX_YES") != ""
	Interactive = inTTY && outTTY && !Plain && !Yes

	loc := os.Getenv("LC_ALL")
	if loc == "" {
		loc = os.Getenv("LC_CTYPE")
	}
	if loc == "" {
		loc = os.Getenv("LANG")
	}
	lc := strings.ToLower(loc)
	ASCII = os.Getenv("ANDRONIX_ASCII") != "" || os.Getenv("TERM") == "linux" ||
		(loc != "" && !strings.Contains(lc, "utf-8") && !strings.Contains(lc, "utf8"))
	if ASCII {
		GOK, GFail, GSkip, GDot, GWarn, GArrow, GAsk, GSep = "+", "x", "-", "*", "!", "->", "?", "-"
	}

	switch {
	case os.Getenv("NO_COLOR") != "" || os.Getenv("ANDRONIX_COLOR") == "never":
		lipgloss.SetColorProfile(termenv.Ascii)
	case os.Getenv("ANDRONIX_COLOR") == "always":
		lipgloss.SetColorProfile(colorFromEnv())
	case Plain:
		lipgloss.SetColorProfile(termenv.Ascii)
	default:
		lipgloss.SetColorProfile(colorFromEnv())
	}
}

// Truecolor only when the terminal says so; otherwise 256 colours.
func colorFromEnv() termenv.Profile {
	switch os.Getenv("COLORTERM") {
	case "truecolor", "24bit":
		return termenv.TrueColor
	}
	t := os.Getenv("TERM")
	if strings.Contains(t, "256") || strings.HasPrefix(t, "xterm") || strings.HasPrefix(t, "screen") || strings.HasPrefix(t, "tmux") {
		return termenv.ANSI256
	}
	return termenv.ANSI
}

// Cols is the terminal width.
func Cols() int {
	if c, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && c > 0 {
		return c
	}
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 80
}

// W is the width we draw to: never the last column (some terminals wrap
// there), at most 72 so boxes stay readable on tablets.
func W() int {
	w := Cols() - 1
	if w > 72 {
		w = 72
	}
	if w < 24 {
		w = 24
	}
	return w
}

// Styles.
var (
	sBold  = lipgloss.NewStyle().Bold(true)
	sMuted = lipgloss.NewStyle().Foreground(Muted)
	sGrey  = lipgloss.NewStyle().Foreground(Grey)
	sOK    = lipgloss.NewStyle().Foreground(Green)
	sFail  = lipgloss.NewStyle().Foreground(Red)
	sWarn  = lipgloss.NewStyle().Foreground(Amber)
	sBrand = lipgloss.NewStyle().Foreground(Orange)
	sCmd   = lipgloss.NewStyle().Foreground(Amber).Bold(true)
)

func Bold(s string) string   { return sBold.Render(s) }
func MutedS(s string) string { return sMuted.Render(s) }
func BrandS(s string) string { return sBrand.Render(s) }

// Banner is the andronix wordmark: a 2-row block font with an amber to
// deep-orange gradient and an orange dot, like the logo. Narrow or ASCII
// terminals get a one-line wordmark.
func Banner(tagline string) string {
	var b strings.Builder
	b.WriteString("\n")
	if Plain {
		b.WriteString("andronix " + tagline + "\n")
		return b.String()
	}
	if !ASCII && Cols() >= 36 {
		top := []string{"▄▀█", "█▄ █", "█▀▄", "█▀█", "█▀█", "█▄ █", "█", "▀▄▀"}
		bot := []string{"█▀█", "█ ▀█", "█▄▀", "█▀▄", "█▄█", "█ ▀█", "█", "█ █"}
		grad := []lipgloss.CompleteColor{
			{TrueColor: "#ffbc57", ANSI256: "215", ANSI: "3"}, {TrueColor: "#ffac47", ANSI256: "215", ANSI: "3"},
			{TrueColor: "#ff9b36", ANSI256: "214", ANSI: "3"}, {TrueColor: "#ff8b25", ANSI256: "208", ANSI: "3"},
			{TrueColor: "#fa7a21", ANSI256: "208", ANSI: "3"}, {TrueColor: "#f5641d", ANSI256: "202", ANSI: "3"},
			{TrueColor: "#e05f0e", ANSI256: "202", ANSI: "3"}, {TrueColor: "#c65c00", ANSI256: "166", ANSI: "3"},
		}
		r1, r2 := "  ", "  "
		for i := range top {
			st := lipgloss.NewStyle().Foreground(grad[i])
			r1 += st.Render(top[i]) + " "
			r2 += st.Render(bot[i]) + " "
		}
		b.WriteString(strings.TrimRight(r1, " ") + "\n")
		b.WriteString(r2 + lipgloss.NewStyle().Foreground(Flame).Render("▄") + "\n")
	} else {
		b.WriteString("  " + sBold.Foreground(Orange).Render("andronix") + lipgloss.NewStyle().Foreground(Flame).Render(".") + "\n")
	}
	if tagline != "" {
		b.WriteString("  " + sMuted.Render(Trunc(tagline, W()-2)) + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// Trunc shortens s to n display columns.
func Trunc(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	ell := "…"
	if ASCII {
		ell = "."
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[:len(r)-1]
	}
	return string(r) + ell
}

// Box kinds.
const (
	BoxBrand = iota
	BoxMuted
	BoxOK
	BoxWarn
	BoxError
)

// Box draws a rounded box with the title set into the top border. Lines
// wrap to the width; "" is a blank spacer.
func Box(kind int, title string, lines ...string) string {
	if Plain {
		var b strings.Builder
		b.WriteString(title + "\n")
		for _, l := range lines {
			if l != "" {
				b.WriteString("  " + l + "\n")
			}
		}
		return b.String()
	}
	col := lipgloss.TerminalColor(Grey)
	tcol := lipgloss.TerminalColor(lipgloss.NoColor{})
	switch kind {
	case BoxBrand:
		col, tcol = Orange, Orange
	case BoxOK:
		col, tcol = Green, Green
	case BoxWarn:
		col, tcol = Amber, Amber
	case BoxError:
		col, tcol = Red, Red
	}
	border := lipgloss.RoundedBorder()
	if ASCII {
		border = lipgloss.ASCIIBorder()
	}
	width := W() - 2 // two-space indent
	inner := width - 4
	body := lipgloss.NewStyle().Border(border, false, true, true, true).BorderForeground(col).
		Padding(0, 1).Width(width - 2).Render(lipgloss.NewStyle().Width(inner).Render(strings.Join(lines, "\n")))
	t := Trunc(title, width-6)
	fill := width - lipgloss.Width(t) - 5
	if fill < 1 {
		fill = 1
	}
	bs := lipgloss.NewStyle().Foreground(col)
	top := bs.Render(border.TopLeft+border.Top) + " " + sBold.Foreground(tcol).Render(t) + " " +
		bs.Render(strings.Repeat(border.Top, fill)+border.TopRight)
	return indent(top+"\n"+body, "  ") + "\n"
}

// KV is a key/value row for boxes and lists.
func KV(k, v string) string {
	return sMuted.Render(fmt.Sprintf("%-9s", k)) + " " + v
}

func indent(s, pre string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pre + l
	}
	return strings.Join(lines, "\n")
}

func msg(glyph string, st lipgloss.Style, text string) {
	if Plain {
		fmt.Println(glyph + " " + text)
		return
	}
	wrapped := lipgloss.NewStyle().Width(W() - 4).Render(text)
	for i, l := range strings.Split(wrapped, "\n") {
		if i == 0 {
			fmt.Println("  " + st.Render(glyph) + " " + l)
		} else {
			fmt.Println("    " + l)
		}
	}
}

func Info(t string) { msg(GDot, sBrand, t) }
func OK(t string)   { msg(GOK, sOK, t) }
func Warn(t string) { msg(GWarn, sWarn, t) }
func Fail(t string) { msg(GFail, sFail, t) }
func Hint(t string) { msg(GArrow, sWarn, t) }

// Note is muted, indented text.
func Note(t string) {
	if Plain {
		fmt.Println("  " + t)
		return
	}
	fmt.Println(indent(sMuted.Width(W()-4).Render(t), "    "))
}

// Cmd shows a command to type.
func Cmd(c string) {
	if Plain {
		fmt.Println("  $ " + c)
		return
	}
	fmt.Println("    " + sGrey.Render("$") + " " + sCmd.Render(c))
}

// Section is a bold heading.
func Section(t string) { fmt.Println("\n  " + sBold.Render(t)) }

// Row prints an aligned key/value row.
func Row(k, v string, kw int) {
	fmt.Println("  " + sMuted.Render(fmt.Sprintf("%-*s", kw, k)) + " " + v)
}

// Footer is the help block every screen ends with.
func Footer() {
	fmt.Println()
	if Plain {
		fmt.Println("help: https://chat.andronix.app  https://docs.andronix.app  support@andronix.app")
		return
	}
	fmt.Println("  " + sBold.Render("Need a hand?"))
	Row("Discord", "https://chat.andronix.app", 8)
	Row("Docs", "https://docs.andronix.app", 8)
	Row("Email", "support@andronix.app", 8)
	fmt.Println()
}

// UserError is a failure we can explain: what happened and how to fix it.
type UserError struct {
	Title, What, Fix string
	Log              string
	Err              error
	// Class names the kind of failure for telemetry (download,
	// package_manager, ...); empty means a slug of Title.
	Class string
}

func (e *UserError) Error() string {
	if e.Err != nil {
		return e.Title + ": " + e.What + ": " + e.Err.Error()
	}
	return e.Title + ": " + e.What
}

func (e *UserError) Unwrap() error { return e.Err }

// Errorf builds a UserError.
func Errorf(title, what, fix string) *UserError {
	return &UserError{Title: title, What: what, Fix: fix}
}

// ShowError prints a friendly error box and the help footer.
func ShowError(err error) {
	ue, ok := err.(*UserError)
	if !ok {
		ue = &UserError{Title: "Something went wrong", What: err.Error(),
			Fix: "Run the same command again. If it keeps happening, send us the log."}
	}
	if Plain {
		fmt.Printf("error: %s: %s\n", ue.Title, ue.What)
		if ue.Fix != "" {
			fmt.Printf("fix: %s\n", ue.Fix)
		}
		if ue.Log != "" {
			fmt.Printf("log: %s\n", ue.Log)
		}
		Footer()
		return
	}
	lines := []string{ue.What}
	if ue.Fix != "" {
		lines = append(lines, "", sWarn.Render(GArrow)+" "+sBold.Render("Fix: ")+ue.Fix)
	}
	if ue.Log != "" {
		lines = append(lines, "", sMuted.Render("Log: ")+Tilde(ue.Log))
	}
	fmt.Println()
	fmt.Print(Box(BoxError, GFail+" "+ue.Title, lines...))
	Footer()
}

// Tilde shows paths under $HOME as ~/...
func Tilde(p string) string {
	if h := os.Getenv("HOME"); h != "" && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}

// Bytes is a human-readable size.
func Bytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
