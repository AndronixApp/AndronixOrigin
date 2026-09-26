package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Reporter is how a running step talks to the screen.
type Reporter interface {
	// Progress sets a bar; unit "bytes" shows sizes, "" shows a count.
	Progress(cur, total int64, unit string)
	// Line is live output (e.g. apt); the last few lines are shown dimmed.
	Line(s string)
	// Detail is shown after the label when the step finishes.
	Detail(s string)
	// Label renames the running step (e.g. once a package count is known).
	Label(s string)
}

// Step is one line in the step list.
type Step struct {
	Label string
	Run   func(ctx context.Context, r Reporter) error
}

type skipErr struct{ reason string }

func (s skipErr) Error() string { return "skipped: " + s.reason }

// Skip ends a step as skipped, e.g. return ui.Skip("already up to date").
func Skip(reason string) error { return skipErr{reason} }

// ErrCancelled is returned when the user presses Ctrl-C.
var ErrCancelled = errors.New("cancelled")

// Logger receives every line and event (the install log). May be nil.
type Logger interface {
	Printf(format string, args ...any)
}

// RunSteps runs steps in order and stops at the first failure.
func RunSteps(ctx context.Context, steps []Step, log Logger) error {
	if log == nil {
		log = nopLog{}
	}
	if Plain {
		return runPlain(ctx, steps, log)
	}
	return runTea(ctx, steps, log)
}

type nopLog struct{}

func (nopLog) Printf(string, ...any) {}

func fmtDur(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	switch {
	case d < time.Second:
		return "<1s"
	case s < 60:
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm%02ds", s/60, s%60)
}

// finishedLine is the permanent line a step leaves behind.
func finishedLine(glyph string, st lipgloss.Style, label, detail, took string) string {
	w := W()
	if detail != "" && lipgloss.Width(label+" "+GSep+" "+detail)+len(took)+6 <= w {
		label += " " + sMuted.Render(GSep+" "+detail)
	}
	label = truncStyled(label, w-6-len(took))
	gap := w - 4 - lipgloss.Width(label) - len(took)
	if gap < 1 {
		gap = 1
	}
	return "  " + st.Render(glyph) + " " + label + strings.Repeat(" ", gap) + sMuted.Render(took)
}

func truncStyled(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	return Trunc(stripANSI(s), n)
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			in = true
		case in && (r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'):
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Plain mode: one stable line per event, for logs, CI and scripts.
// ---------------------------------------------------------------------------

type plainRep struct {
	log          Logger
	label        string
	detail       string
	lastQuartile int64
}

func (p *plainRep) Progress(cur, total int64, unit string) {
	if total <= 0 {
		return
	}
	q := cur * 4 / total
	if q > p.lastQuartile && q < 4 {
		p.lastQuartile = q
		fmt.Printf("  %s %d%%\n", p.label, q*25)
	}
}
func (p *plainRep) Line(s string)   { p.log.Printf("%s", s) }
func (p *plainRep) Detail(s string) { p.detail = s }
func (p *plainRep) Label(s string)  { p.label = s }

func runPlain(ctx context.Context, steps []Step, log Logger) error {
	for i, s := range steps {
		rep := &plainRep{log: log, label: s.Label}
		fmt.Printf("run: [%d/%d] %s\n", i+1, len(steps), s.Label)
		log.Printf("== step: %s", s.Label)
		start := time.Now()
		err := s.Run(ctx, rep)
		took := fmtDur(time.Since(start))
		var sk skipErr
		switch {
		case errors.As(err, &sk):
			fmt.Printf("skip: %s (%s)\n", rep.label, sk.reason)
		case err != nil:
			log.Printf("step failed: %v", err)
			fmt.Printf("fail: %s (%s)\n", rep.label, took)
			return err
		default:
			d := ""
			if rep.detail != "" {
				d = " " + rep.detail
			}
			fmt.Printf("ok: %s (%s)%s\n", rep.label, took, d)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Fancy mode: a bubbletea program. Finished steps are printed above the
// live area (tea.Println), so only the running step redraws, at the
// renderer's frame rate, and only the lines that changed.
// ---------------------------------------------------------------------------

type (
	progMsg struct {
		cur, total int64
		unit       string
	}
	lineMsg   string
	detailMsg string
	labelMsg  string
	doneMsg   struct{ err error }
	quitMsg   struct{}
)

type teaRep struct {
	send func(tea.Msg)
	log  Logger
	mu   sync.Mutex
	last time.Time
}

func (r *teaRep) Progress(cur, total int64, unit string) {
	// Coalesce: at most ~15 updates a second reach the UI.
	r.mu.Lock()
	now := time.Now()
	if cur < total && now.Sub(r.last) < 66*time.Millisecond {
		r.mu.Unlock()
		return
	}
	r.last = now
	r.mu.Unlock()
	r.send(progMsg{cur, total, unit})
}
func (r *teaRep) Line(s string)   { r.log.Printf("%s", s); r.send(lineMsg(s)) }
func (r *teaRep) Detail(s string) { r.send(detailMsg(s)) }
func (r *teaRep) Label(s string)  { r.send(labelMsg(s)) }

type stepModel struct {
	steps  []Step
	idx    int
	label  string
	detail string
	start  time.Time
	spin   spinner.Model
	bar    progress.Model
	cur    int64
	total  int64
	unit   string
	tail   []string
	err    error
	ctx    context.Context
	cancel context.CancelFunc
	rep    *teaRep
	log    Logger
}

func newStepModel(ctx context.Context, steps []Step, log Logger) *stepModel {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	if ASCII {
		sp.Spinner = spinner.Line
	}
	sp.Style = sBrand
	bar := progress.New(progress.WithScaledGradient("#ff8b25", "#ffbc57"), progress.WithoutPercentage())
	if ASCII {
		bar = progress.New(progress.WithSolidFill("#ff8b25"), progress.WithoutPercentage(), progress.WithFillCharacters('#', '.'))
	} else {
		bar.Full = '█'
		bar.Empty = '░'
	}
	bar.EmptyColor = "#535353"
	cctx, cancel := context.WithCancel(ctx)
	return &stepModel{steps: steps, spin: sp, bar: bar, ctx: cctx, cancel: cancel, log: log}
}

func (m *stepModel) Init() tea.Cmd { return tea.Batch(m.spin.Tick, m.startStep()) }

func (m *stepModel) startStep() tea.Cmd {
	s := m.steps[m.idx]
	m.label, m.detail, m.start = s.Label, "", time.Now()
	m.cur, m.total, m.unit, m.tail = 0, 0, "", nil
	m.log.Printf("== step: %s", s.Label)
	rep := m.rep
	ctx := m.ctx
	return func() tea.Msg { return doneMsg{s.Run(ctx, rep)} }
}

func (m *stepModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.cancel()
			m.err = ErrCancelled
			return m, tea.Sequence(tea.Println(finishedLine(GFail, sFail, m.label, "cancelled", fmtDur(time.Since(m.start)))), tea.Quit)
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case progMsg:
		m.cur, m.total, m.unit = msg.cur, msg.total, msg.unit
	case lineMsg:
		l := strings.TrimSpace(stripANSI(strings.ReplaceAll(string(msg), "\r", "")))
		if l != "" {
			m.tail = append(m.tail, l)
			if len(m.tail) > 3 {
				m.tail = m.tail[len(m.tail)-3:]
			}
		}
	case detailMsg:
		m.detail = string(msg)
	case labelMsg:
		m.label = string(msg)
	case doneMsg:
		took := fmtDur(time.Since(m.start))
		var sk skipErr
		var line string
		switch {
		case errors.As(msg.err, &sk):
			line = "  " + sMuted.Render(GSkip+" "+Trunc(m.label+" "+GSep+" "+sk.reason, W()-4))
		case msg.err != nil:
			m.log.Printf("step failed: %v", msg.err)
			m.err = msg.err
			return m, tea.Sequence(tea.Println(finishedLine(GFail, sFail, m.label, "", took)), tea.Quit)
		default:
			line = finishedLine(GOK, sOK, m.label, m.detail, took)
		}
		m.idx++
		if m.idx >= len(m.steps) {
			return m, tea.Sequence(tea.Println(line), func() tea.Msg { return quitMsg{} })
		}
		return m, tea.Sequence(tea.Println(line), m.startStep())
	case quitMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m *stepModel) View() string {
	if m.idx >= len(m.steps) || m.err != nil {
		return ""
	}
	w := W()
	took := fmtDur(time.Since(m.start))
	label := Trunc(m.label, w-6-len(took))
	gap := w - 4 - lipgloss.Width(label) - len(took)
	if gap < 1 {
		gap = 1
	}
	var b strings.Builder
	b.WriteString("  " + m.spin.View() + " " + label + strings.Repeat(" ", gap) + sMuted.Render(took))
	if m.total > 0 {
		pct := float64(m.cur) / float64(m.total)
		if pct > 1 {
			pct = 1
		}
		right := fmt.Sprintf("%3d%%", int(pct*100))
		if m.unit == "bytes" {
			right = fmt.Sprintf("%s / %s  %s", Bytes(m.cur), Bytes(m.total), right)
		} else if m.unit != "" {
			right = fmt.Sprintf("%d/%d %s  %s", m.cur, m.total, m.unit, right)
		}
		bw := w - 4 - 2 - lipgloss.Width(right)
		if bw < 10 { // narrow: drop the numbers, keep the bar
			right = fmt.Sprintf("%3d%%", int(pct*100))
			bw = w - 4 - 2 - len(right)
		}
		m.bar.Width = bw
		b.WriteString("\n    " + m.bar.ViewAs(pct) + "  " + sMuted.Render(right))
	}
	for _, l := range m.tail {
		b.WriteString("\n    " + sGrey.Render("│ "+Trunc(l, w-6)))
	}
	return b.String()
}

func runTea(ctx context.Context, steps []Step, log Logger) error {
	m := newStepModel(ctx, steps, log)
	opts := []tea.ProgramOption{tea.WithOutput(os.Stdout)}
	if !Interactive {
		opts = append(opts, tea.WithInput(nil))
	}
	p := tea.NewProgram(m, opts...)
	m.rep = &teaRep{send: p.Send, log: log}
	if _, err := p.Run(); err != nil {
		return err
	}
	m.cancel()
	return m.err
}
