package ui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/farrellm/polymarket/internal/export"
)

const (
	// exportCap is the most rows an export writes unless the dialog is told
	// otherwise: everything that matches may be tens of thousands of rows.
	exportCap = 10000
	// exportProgressInterval is how often a running export reports its row
	// count.
	exportProgressInterval = 100 * time.Millisecond
)

// exportScope is one answer to which rows a dataset is to hold: those in
// hand, or all there are.
type exportScope struct {
	label string
	// open builds the dataset. One that fetches does so under ctx, as its
	// rows are read, and never on the goroutine that draws the screen: what
	// it holds of the screen's is a copy.
	open func(ctx context.Context) export.Dataset
}

// exportChoice is a dataset a screen offers to write out, with the scopes it
// can be written in. A screen lists the one that is most its own first.
type exportChoice struct {
	name   string
	scopes []exportScope
}

// The fields of the export dialog, in the order they are listed.
const (
	exportFieldDataset = iota
	exportFieldRows
	exportFieldLimit
	exportFieldFile
	numExportFields
)

var exportLabels = [numExportFields]string{"Dataset", "Rows", "At most", "File"}

// exportRequest is what the dialog was left saying.
type exportRequest struct {
	dataset string
	scope   exportScope
	// limit is the most rows to write; zero is all of them.
	limit int
	// shown is the file as it was typed, and path with the home directory
	// written out.
	shown, path string
}

// exportDialog asks what to export and where to: a dataset, which of its
// rows, how many at most, and the file.
//
// Like the filter form it is drawn by hand rather than with huh, and for the
// same reasons: a cursor that cannot be stopped from blinking, and messages
// of its own to route.
type exportDialog struct {
	keys keyMap
	st   styles
	// stamp is the moment the dialog was opened, as a default file name
	// carries it.
	stamp string

	choices []exportChoice
	dataset int
	scope   int
	// inputs are indexed by field; only the last two are used.
	inputs [numExportFields]textinput.Model
	focus  int

	problem string
	// req is what the fields said when enter was last pressed on them.
	req exportRequest
	// confirm is the file enter was last pressed on and found to exist.
	// Enter on the same one again overwrites it.
	confirm string
}

func newExportDialog(e env, choices []exportChoice) *exportDialog {
	d := &exportDialog{
		keys:    e.keys,
		st:      e.st,
		stamp:   e.now().Format("20060102-150405"),
		choices: choices,
	}
	for i := range d.inputs {
		d.inputs[i] = newPrompt("")
	}
	// A path is longer than a prompt's text is allowed to be.
	d.inputs[exportFieldFile].CharLimit = 0
	d.inputs[exportFieldLimit].SetValue(strconv.Itoa(exportCap))
	d.inputs[exportFieldFile].SetValue(d.defaultPath())
	return d
}

func (d *exportDialog) choice() exportChoice { return d.choices[d.dataset] }

// defaultPath is where the dataset chosen goes unless a file is named.
func (d *exportDialog) defaultPath() string {
	return "./polymarket-" + d.choice().name + "-" + d.stamp + ".csv"
}

// expandHome writes out the home directory a path starts with as ~.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// read turns the fields into a request, or says what is wrong with one.
func (d *exportDialog) read() (req exportRequest, problem string) {
	c := d.choice()
	req = exportRequest{dataset: c.name, scope: c.scopes[d.scope]}

	if text := strings.ReplaceAll(strings.TrimSpace(d.inputs[exportFieldLimit].Value()), ",", ""); text != "" {
		n, err := strconv.Atoi(text)
		if err != nil || n <= 0 {
			return req, "At most is not a number of rows such as 10000"
		}
		req.limit = n
	}

	req.shown = strings.TrimSpace(d.inputs[exportFieldFile].Value())
	if req.shown == "" {
		return req, "File needs the name of a file to write"
	}
	req.path = expandHome(req.shown)
	if dir := filepath.Dir(req.path); !isDir(dir) {
		return req, "there is no directory " + dir
	}
	switch info, err := os.Stat(req.path); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return req, err.Error()
	case info.IsDir():
		return req, req.shown + " is a directory"
	case d.confirm != req.path:
		d.confirm = req.path
		return req, req.shown + " exists: enter again overwrites it"
	}
	return req, ""
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (d *exportDialog) typed(field int) bool {
	return field == exportFieldLimit || field == exportFieldFile
}

// moveTo puts the focus on a field.
func (d *exportDialog) moveTo(field int) {
	if d.typed(d.focus) {
		d.inputs[d.focus].Blur()
	}
	d.focus = (field + numExportFields) % numExportFields
	if d.typed(d.focus) {
		d.inputs[d.focus].CursorEnd()
		// Focus only has a command to return for a cursor that blinks.
		_ = d.inputs[d.focus].Focus()
	}
}

// turn moves a choice on by step, round and round.
func (d *exportDialog) turn(step int) {
	switch d.focus {
	case exportFieldDataset:
		// A file name left as it was offered follows the dataset.
		follows := d.inputs[exportFieldFile].Value() == d.defaultPath()
		d.dataset = (d.dataset + step + len(d.choices)) % len(d.choices)
		d.scope = 0
		if follows {
			d.inputs[exportFieldFile].SetValue(d.defaultPath())
		}
	case exportFieldRows:
		n := len(d.choice().scopes)
		d.scope = (d.scope + step + n) % n
	}
}

func (d *exportDialog) update(msg tea.KeyPressMsg) (tea.Cmd, formOutcome) {
	k := d.keys
	switch {
	case key.Matches(msg, k.Cancel):
		return nil, formCancelled
	case key.Matches(msg, k.Apply):
		if d.req, d.problem = d.read(); d.problem != "" {
			return nil, formOpen
		}
		return nil, formApplied
	case key.Matches(msg, k.Next):
		d.moveTo(d.focus + 1)
	case key.Matches(msg, k.Previous):
		d.moveTo(d.focus - 1)
	case d.typed(d.focus):
		return d.edit(msg), formOpen
	case key.Matches(msg, k.Right):
		d.turn(+1)
	case key.Matches(msg, k.Left):
		d.turn(-1)
	}
	return nil, formOpen
}

// edit hands the field in focus a message that is its to read.
func (d *exportDialog) edit(msg tea.Msg) tea.Cmd {
	if !d.typed(d.focus) {
		return nil
	}
	before := d.inputs[d.focus].Value()
	var cmd tea.Cmd
	d.inputs[d.focus], cmd = d.inputs[d.focus].Update(msg)
	if d.inputs[d.focus].Value() != before {
		// What was said was said of the fields as they were.
		d.problem, d.confirm = "", ""
	}
	return cmd
}

// choiceLine lists the values of a choice, the one chosen marked.
func (d *exportDialog) choiceLine(labels []string, chosen int) string {
	parts := make([]string, len(labels))
	for i, l := range labels {
		parts[i] = d.st.faint.Render(" " + l + " ")
		if i == chosen {
			parts[i] = d.st.tab.Render("[" + l + "]")
		}
	}
	return strings.Join(parts, " ")
}

func (d *exportDialog) view(w int) string {
	const labelWidth = 15
	// A long path scrolls within the room there is rather than running off
	// the frame.
	d.inputs[exportFieldFile].SetWidth(max(w-labelWidth-4, 1))

	lines := []string{"", " " + d.st.header.Render("Export"), ""}
	for i := range numExportFields {
		marker := " "
		if i == d.focus {
			marker = cursorMarker
		}
		var value string
		switch i {
		case exportFieldDataset:
			names := make([]string, len(d.choices))
			for j, c := range d.choices {
				names[j] = c.name
			}
			value = d.choiceLine(names, d.dataset)
		case exportFieldRows:
			scopes := d.choice().scopes
			labels := make([]string, len(scopes))
			for j, s := range scopes {
				labels[j] = s.label
			}
			value = d.choiceLine(labels, d.scope)
		default:
			value = d.inputs[i].View()
		}
		lines = append(lines, " "+marker+fit(exportLabels[i], labelWidth, false)+value)
	}
	lines = append(lines, "",
		" "+d.st.faint.Render("← → change a choice · At most is a number of rows, blank for no limit"),
		" "+d.st.faint.Render("the file is CSV, with a header row · ~/ is the home directory"), "")
	if d.problem != "" {
		lines = append(lines, " "+d.st.failure.Render(d.problem))
	}
	for i, l := range lines {
		lines[i] = clip(l, w)
	}
	return strings.Join(lines, "\n")
}

// exportProgressMsg is the row count of an export under way.
type exportProgressMsg struct {
	job  *exportJob
	rows int
}

// exportDoneMsg reports that an export has ended, one way or another. How is
// on the job.
type exportDoneMsg struct {
	job *exportJob
}

// exportJob is an export under way. It runs on a goroutine of its own, so
// that the browser stays usable, and reports back through updates.
type exportJob struct {
	req    exportRequest
	cancel context.CancelFunc
	// rows is the count last reported, and stopping is true once the user
	// has asked for the export to end.
	rows     int
	stopping bool

	// run is the export itself. once sees to it that it runs one time,
	// whether the program got round to starting it or is quitting first.
	run  func()
	once sync.Once
	// updates carries the row count as it grows, and is closed when the
	// export has ended. sum and err are only read after that.
	updates chan int
	sum     export.Summary
	err     error
}

func newExportJob(req exportRequest) *exportJob {
	ctx, cancel := context.WithCancel(context.Background())
	j := &exportJob{req: req, cancel: cancel, updates: make(chan int, 1)}
	j.run = func() {
		defer close(j.updates)
		// The throttle is the one thing here timed by the real clock: it
		// spaces out redraws, and shows nothing itself.
		var last time.Time
		opts := export.Options{Limit: req.limit, Progress: func(rows int) {
			now := time.Now()
			if now.Sub(last) < exportProgressInterval {
				return
			}
			last = now
			// A count nobody has read yet is as good as this one.
			select {
			case j.updates <- rows:
			default:
			}
		}}
		j.sum, j.err = export.File(ctx, req.scope.open(ctx), req.path, opts)
	}
	return j
}

// start sets the export going and waits for its first news.
func (j *exportJob) start() tea.Cmd {
	return func() tea.Msg {
		go j.once.Do(j.run)
		return j.next()
	}
}

// listen waits for the export's next news. The export ends, so this does.
func (j *exportJob) listen() tea.Cmd {
	return func() tea.Msg { return j.next() }
}

func (j *exportJob) next() tea.Msg {
	rows, ok := <-j.updates
	if !ok {
		return exportDoneMsg{job: j}
	}
	return exportProgressMsg{job: j, rows: rows}
}

// finish stops the export and waits until it has: a file half written is
// removed on the way out, which a program that has quit cannot do.
func (j *exportJob) finish() {
	j.cancel()
	j.once.Do(j.run)
}

// running is what the status bar says of the export while it lasts.
func (j *exportJob) running() string {
	if j.stopping {
		return "stopping the export…"
	}
	// Not the file, which the dialog named and the outcome will: the bar
	// has to have room left for the key that stops it.
	return "exporting " + j.req.dataset + ": " + countOf(j.rows, "row")
}

// outcome is what the status bar says of the export once it has ended.
func (j *exportJob) outcome() status {
	switch {
	case j.err != nil && j.stopping:
		return status{text: "export cancelled: " + j.req.shown + " was not written"}
	case j.err != nil:
		return status{failure: "could not export the " + j.req.dataset + ": " + j.err.Error()}
	}
	// The status bar may be too short for all of this: what matters most
	// comes first, and the file is named after the dataset as a rule.
	parts := []string{"wrote " + countOf(j.sum.Rows, "row") + " to " + j.req.shown}
	if j.sum.Capped {
		parts = []string{"wrote the first " + countOf(j.sum.Rows, "row") + " to " + j.req.shown, "more match"}
	}
	return status{text: strings.Join(append(parts, j.sum.Notes...), " · ")}
}

// countOf is a number of things, in words: "1 row", "12 rows".
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// openExport opens the export dialog on what the screen on show offers.
func (m *Model) openExport() {
	if m.job != nil {
		m.notice = status{text: "an export is under way · esc stops it"}
		return
	}
	choices := m.top().exports()
	if len(choices) == 0 {
		m.notice = status{text: "nothing here to export yet"}
		return
	}
	m.dialog = newExportDialog(m.env, choices)
}

// updateExport drives the dialog, and starts the export it was left asking
// for.
func (m *Model) updateExport(msg tea.KeyPressMsg) tea.Cmd {
	cmd, outcome := m.dialog.update(msg)
	switch outcome {
	case formOpen:
		return cmd
	case formApplied:
		m.job = newExportJob(m.dialog.req)
		m.dialog = nil
		return m.job.start()
	case formCancelled:
		m.dialog = nil
	}
	return nil
}

// exported takes in the news of an export. It reports whether the message
// was that.
func (m *Model) exported(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case exportProgressMsg:
		if msg.job != m.job {
			return nil, true
		}
		m.job.rows = msg.rows
		return m.job.listen(), true
	case exportDoneMsg:
		if msg.job == m.job {
			m.notice = m.job.outcome()
			m.job = nil
		}
		return nil, true
	}
	return nil, false
}

// stopExport is the binding the status bar shows while an export runs.
func (k keyMap) stopExport() key.Binding {
	return key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "stop export"))
}

// startExport is the key that leaves the dialog, named for what it does there.
func (k keyMap) startExport() key.Binding {
	return key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "export"))
}
