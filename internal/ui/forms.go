package ui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Theme is huh in Andronix colours.
func Theme() *huh.Theme {
	t := huh.ThemeBase()
	t.Focused.Base = t.Focused.Base.BorderForeground(Orange)
	t.Focused.Title = t.Focused.Title.Foreground(Orange).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(Muted)
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(Red)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(Red)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(Orange).SetString("> ")
	t.Focused.Option = t.Focused.Option.Foreground(lipgloss.NoColor{})
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(Amber)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(lipgloss.Color("#1a1612")).Background(Orange).Bold(true)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(Muted).Background(Grey)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(Amber)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(Orange)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(Grey)
	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Title = t.Blurred.Title.Foreground(Muted).Bold(false)
	return t
}

func run(fields ...huh.Field) error {
	f := huh.NewForm(huh.NewGroup(fields...)).WithTheme(Theme()).WithWidth(W() - 2).WithShowHelp(false)
	if ASCII {
		f = f.WithAccessible(true)
	}
	err := f.Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCancelled
	}
	return err
}

// Confirm asks a yes/no question. Without a terminal it returns def
// (or yes with --yes).
func Confirm(title, desc string, def bool) (bool, error) {
	if Yes {
		return true, nil
	}
	if !Interactive {
		return def, nil
	}
	v := def
	err := run(huh.NewConfirm().Title(title).Description(desc).Affirmative("Yes").Negative("No").Value(&v))
	return v, err
}

// Option is a menu entry.
type Option struct{ Label, Value string }

// Choose shows a menu; without a terminal it returns def.
func Choose(title, desc string, opts []Option, def string) (string, error) {
	if !Interactive {
		return def, nil
	}
	v := def
	var hopts []huh.Option[string]
	for _, o := range opts {
		hopts = append(hopts, huh.NewOption(o.Label, o.Value))
	}
	err := run(huh.NewSelect[string]().Title(title).Description(desc).Options(hopts...).Value(&v))
	return v, err
}

// Input asks for text, checked by validate (may be nil).
func Input(title, desc, placeholder string, validate func(string) error) (string, error) {
	if !Interactive {
		return "", fmt.Errorf("%s: needs a terminal", title)
	}
	var v string
	in := huh.NewInput().Title(title).Description(desc).Placeholder(placeholder).Value(&v)
	if validate != nil {
		in = in.Validate(validate)
	}
	err := run(in)
	return v, err
}

// Password asks twice and checks both match.
func Password(title, desc string, min, max int) (string, error) {
	if !Interactive {
		return "", fmt.Errorf("%s: needs a terminal", title)
	}
	var a, b string
	check := func(s string) error {
		if len(s) < min {
			return fmt.Errorf("use at least %d characters", min)
		}
		if max > 0 && len(s) > max {
			return fmt.Errorf("use at most %d characters", max)
		}
		return nil
	}
	err := run(
		huh.NewInput().Title(title).Description(desc).EchoMode(huh.EchoModePassword).Validate(check).Value(&a),
		huh.NewInput().Title("Type it again").EchoMode(huh.EchoModePassword).Validate(func(s string) error {
			if s != a {
				return errors.New("the passwords don't match")
			}
			return nil
		}).Value(&b),
	)
	return a, err
}
