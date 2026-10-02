package ui

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/farrellm/polymarket/internal/api"
)

// The fields of the filter form, in the order they are listed.
const (
	fieldStatus = iota
	fieldVolume
	fieldLiquidity
	fieldEndsAfter
	fieldEndsBefore
	numFields
)

var fieldLabels = [numFields]string{
	"Show", "Min volume", "Min liquidity", "Ends after", "Ends before",
}

// statusChoices are what the Show field cycles through.
var statusChoices = []struct {
	status api.Status
	label  string
}{
	{api.StatusOpen, "open"},
	{api.StatusClosed, "closed"},
	{api.StatusAll, "all"},
}

// formOutcome is how a key left the filter form.
type formOutcome int

const (
	formOpen formOutcome = iota
	formApplied
	formCancelled
)

// filterForm edits a filter: which of open and closed to show, the floors
// and the bounds on the end date. The sort is not its business.
//
// It is drawn by hand rather than with huh, whose text fields have a cursor
// that cannot be stopped from blinking, and whose moves from field to field
// are messages that would have to be routed back to it from the program.
type filterForm struct {
	keys keyMap
	st   styles

	// filter is what the form was opened on and, once it is applied, what
	// the fields say.
	filter api.Filter
	status int
	// inputs are indexed by field; the one for the status is not used.
	inputs  [numFields]textinput.Model
	focus   int
	problem string
}

func newFilterForm(e env, f api.Filter) *filterForm {
	form := &filterForm{keys: e.keys, st: e.st, filter: f}
	for i, c := range statusChoices {
		if c.status == f.Status {
			form.status = i
		}
	}
	for i := range form.inputs {
		form.inputs[i] = newPrompt("")
	}
	form.inputs[fieldVolume].SetValue(amountText(f.VolumeMin))
	form.inputs[fieldLiquidity].SetValue(amountText(f.LiquidityMin))
	form.inputs[fieldEndsAfter].SetValue(dateText(f.EndDateMin))
	form.inputs[fieldEndsBefore].SetValue(dateText(f.EndDateMax))
	return form
}

// amountText is a floor as the form shows it; none is blank.
func amountText(v float64) string {
	if v <= 0 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// dateText is a bound as the form shows it: a day if it is the first moment
// of one, else the whole timestamp. None is blank.
func dateText(t time.Time) string {
	switch {
	case t.IsZero():
		return ""
	case t.Equal(t.Truncate(24 * time.Hour)):
		return t.UTC().Format(time.DateOnly)
	}
	return t.UTC().Format(time.RFC3339)
}

// parseAmount reads a floor: a number of dollars, which may carry a $, commas
// and one of the suffixes the amounts on screen are shown with. Blank is none.
func parseAmount(s string) (float64, error) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(s), "$"), ",", ""))
	if s == "" {
		return 0, nil
	}
	scale := 1.0
	switch s[len(s)-1] {
	case 'k':
		scale = 1e3
	case 'm':
		scale = 1e6
	case 'b':
		scale = 1e9
	}
	if scale != 1 {
		s = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, errors.New("not an amount such as 5000 or 10k")
	}
	return v * scale, nil
}

// read turns the fields into a filter, or says what is wrong with one.
func (f *filterForm) read() (out api.Filter, problem string) {
	out = f.filter
	out.Status = statusChoices[f.status].status

	var err error
	amounts := []struct {
		field int
		into  *float64
	}{{fieldVolume, &out.VolumeMin}, {fieldLiquidity, &out.LiquidityMin}}
	for _, a := range amounts {
		if *a.into, err = parseAmount(f.inputs[a.field].Value()); err != nil {
			return out, fieldLabels[a.field] + " is " + err.Error()
		}
	}
	dates := []struct {
		field int
		into  *time.Time
	}{{fieldEndsAfter, &out.EndDateMin}, {fieldEndsBefore, &out.EndDateMax}}
	for _, d := range dates {
		if *d.into, err = api.ParseDate(strings.TrimSpace(f.inputs[d.field].Value())); err != nil {
			return out, fieldLabels[d.field] + " is " + err.Error()
		}
	}
	if !out.EndDateMin.IsZero() && !out.EndDateMax.IsZero() && out.EndDateMax.Before(out.EndDateMin) {
		return out, "Ends before is earlier than Ends after"
	}
	return out, ""
}

// moveTo puts the focus on a field.
func (f *filterForm) moveTo(field int) {
	if f.focus != fieldStatus {
		f.inputs[f.focus].Blur()
	}
	f.focus = (field + numFields) % numFields
	if f.focus != fieldStatus {
		f.inputs[f.focus].CursorEnd()
		// Focus only has a command to return for a cursor that blinks.
		_ = f.inputs[f.focus].Focus()
	}
}

func (f *filterForm) update(msg tea.KeyPressMsg) (tea.Cmd, formOutcome) {
	k := f.keys
	switch {
	case key.Matches(msg, k.Cancel):
		return nil, formCancelled
	case key.Matches(msg, k.Apply):
		filter, problem := f.read()
		if problem != "" {
			f.problem = problem
			return nil, formOpen
		}
		f.filter = filter
		return nil, formApplied
	case key.Matches(msg, k.Next):
		f.moveTo(f.focus + 1)
	case key.Matches(msg, k.Previous):
		f.moveTo(f.focus - 1)
	case f.focus != fieldStatus:
		f.problem = ""
		return f.edit(msg), formOpen
	case key.Matches(msg, k.Right):
		f.status = (f.status + 1) % len(statusChoices)
	case key.Matches(msg, k.Left):
		f.status = (f.status + len(statusChoices) - 1) % len(statusChoices)
	}
	return nil, formOpen
}

// edit hands the field in focus a message that is its to read.
func (f *filterForm) edit(msg tea.Msg) tea.Cmd {
	if f.focus == fieldStatus {
		return nil
	}
	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	return cmd
}

func (f *filterForm) view(w int) string {
	const labelWidth = 15

	lines := []string{"", " " + f.st.header.Render("Filter"), ""}
	for i := range numFields {
		marker := " "
		if i == f.focus {
			marker = cursorMarker
		}
		var value string
		if i == fieldStatus {
			choices := make([]string, len(statusChoices))
			for j, c := range statusChoices {
				choices[j] = f.st.faint.Render(" " + c.label + " ")
				if j == f.status {
					choices[j] = f.st.tab.Render("[" + c.label + "]")
				}
			}
			value = strings.Join(choices, " ")
		} else {
			value = f.inputs[i].View()
		}
		lines = append(lines, " "+marker+fit(fieldLabels[i], labelWidth, false)+value)
	}
	lines = append(lines, "",
		" "+f.st.faint.Render("← → change what is shown · a blank field is no bound"),
		" "+f.st.faint.Render("an amount may be 5000 or 10k · a date is 2026-11-03"), "")
	if f.problem != "" {
		lines = append(lines, " "+f.st.failure.Render(f.problem))
	}
	for i, l := range lines {
		lines[i] = clip(l, w)
	}
	return strings.Join(lines, "\n")
}
