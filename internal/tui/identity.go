package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// identityForm is shared by first-run onboarding and profile editing: a
// handle, a palette, and (when editing) a one-line bio, with a live preview
// of how you will look on the board.
type identityForm struct {
	handle textinput.Model
	bio    textinput.Model
	palIdx int
	flair  int
	focus  int // 0 handle, 1 palette, 2 bio, 3 post flair
	err    string
	busy   bool
}

func newIdentityForm(th kit.Theme, suggested string, acct *board.Account) identityForm {
	f := identityForm{handle: textinput.New(), bio: textinput.New()}
	f.handle.Prompt = "@ "
	f.handle.Placeholder = "your-handle"
	f.handle.CharLimit = board.MaxHandleLen
	f.bio.Prompt = "› "
	f.bio.Placeholder = "a line about you (optional)"
	f.bio.CharLimit = board.MaxBioLen
	if acct != nil {
		f.handle.SetValue(acct.Handle)
		f.bio.SetValue(acct.Bio)
		f.palIdx = kit.PaletteIndex(acct.Palette)
		f.flair = max(0, slices.Index(board.PostFlairs(), acct.Flair))
	} else if board.ValidHandle(suggested) && suggested != "default" {
		f.handle.SetValue(suggested)
	}
	f.handle.Focus()
	f.restyle(th)
	return f
}

func (f *identityForm) palette() kit.Palette { return kit.Palettes[f.palIdx] }

func (f *identityForm) restyle(th kit.Theme) {
	kit.StyleInput(&f.handle, th, f.palette().Stops)
	kit.StyleInput(&f.bio, th, f.palette().Stops)
}

func (f *identityForm) setFocus(i, fields int) {
	f.focus = (i + fields) % fields
	f.handle.Blur()
	f.bio.Blur()
	switch f.focus {
	case 0:
		f.handle.Focus()
	case 2:
		f.bio.Focus()
	}
}

// update handles one input for the form; it returns submit=true on enter
// from the last field or ctrl+s anywhere.
func (f *identityForm) update(msg tea.Msg, th kit.Theme, fields int) (cmd tea.Cmd, submit bool) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "tab", "down":
			f.setFocus(f.focus+1, fields)
			return nil, false
		case "shift+tab", "up":
			f.setFocus(f.focus-1, fields)
			return nil, false
		case "ctrl+s":
			return nil, true
		case "enter":
			if f.focus == fields-1 {
				return nil, true
			}
			f.setFocus(f.focus+1, fields)
			return nil, false
		}
		if f.focus == 1 {
			switch k.String() {
			case "left", "h":
				f.palIdx = (f.palIdx + len(kit.Palettes) - 1) % len(kit.Palettes)
			case "right", "l", "space":
				f.palIdx = (f.palIdx + 1) % len(kit.Palettes)
			}
			f.restyle(th)
			return nil, false
		}
		if f.focus == 3 {
			count := len(board.PostFlairs())
			switch k.String() {
			case "left", "h":
				f.flair = (f.flair + count - 1) % count
			case "right", "l", "space":
				f.flair = (f.flair + 1) % count
			}
			return nil, false
		}
	}
	f.err = ""
	switch f.focus {
	case 0:
		f.handle, cmd = f.handle.Update(msg)
	case 2:
		f.bio, cmd = f.bio.Update(msg)
	}
	return cmd, false
}

func (m *Model) updateOnboard(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "esc" {
		return tea.Quit
	}
	cmd, submit := m.form.update(msg, m.th, 2)
	if !submit || m.form.busy {
		return cmd
	}
	handle := strings.TrimSpace(m.form.handle.Value())
	if !board.ValidHandle(handle) {
		m.form.err = board.ErrHandleInvalid.Error()
		return cmd
	}
	m.form.busy = true
	d, pal := m.d, m.form.palette().Name
	return func() tea.Msg {
		a, err := d.Service.Register(d.Context, d.Fingerprint, d.PublicKey, handle, pal)
		return registeredMsg{acct: a, err: err}
	}
}

func (m *Model) updateProfile(msg tea.Msg) tea.Cmd {
	if m.form.busy {
		return nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			return m.back()
		case "ctrl+k":
			m.openPalette()
			return nil
		}
	}
	before := m.form.palIdx
	cmd, submit := m.form.update(msg, m.th, 4)
	if m.form.palIdx != before {
		m.setAccent(m.form.palette().Stops)
	}
	if !submit || m.acct == nil {
		return cmd
	}
	acct, d := *m.acct, m.d
	handle, pal, bio := m.form.handle.Value(), m.form.palette().Name, m.form.bio.Value()
	flair := board.PostFlairs()[m.form.flair]
	m.form.busy = true
	return func() tea.Msg {
		a, err := d.Service.UpdateProfile(d.Context, acct, handle, pal, bio, flair)
		return registeredMsg{acct: a, err: err}
	}
}

// ---- views -------------------------------------------------------------

// viewIdentity renders onboarding (full-screen) or the profile editor
// (inside the chrome).
func (m Model) viewIdentity(edit bool) string {
	f := m.form
	stops := f.palette().Stops
	w := min(m.w-4, 92)
	var b strings.Builder

	if !edit {
		shimmer := float64(m.frame) / 60
		// The block wordmark is five rows; on short terminals the form
		// needs them more, so the title falls back to a single line.
		if m.h >= 26 {
			b.WriteString(kit.Center(kit.Wordmark("BULLETIN", m.th.Brand.Stops, shimmer), m.w) + "\n\n")
		} else {
			b.WriteString(kit.Center(kit.Gradient("B U L L E T I N", lipgloss.NewStyle().Bold(true), m.th.Brand.Stops, shimmer), m.w) + "\n\n")
		}
		b.WriteString(kit.Center(kit.Gradient("make your mark", lipgloss.NewStyle().Bold(true), m.th.Brand.Stops, shimmer), m.w) + "\n")
		b.WriteString(kit.Center(kit.S(m.th.Muted).Render("Your SSH key is your account — no password, no email, nothing to forget."), m.w) + "\n\n")
	} else {
		b.WriteString(kit.Center(m.d.Fingerprint, m.w) + "\n")
	}

	// Left column: fields. Right column: live preview card.
	fieldsW := min(44, w/2)
	fields := m.formFields(fieldsW, edit)
	preview := m.previewCard(min(w-fieldsW-3, 46), edit)
	row := kit.SideBySide(3, fields, preview)
	b.WriteString(kit.Center(row, m.w) + "\n")

	if f.err != "" {
		b.WriteString(kit.Center(kit.S(m.th.Bad).Render("✗ "+f.err), m.w) + "\n")
	}
	if !edit {
		b.WriteString("\n" + kit.Center(kit.Hints([]kit.Hint{{"tab", "next field"}, {"←→", "palette"}, {"⏎", "create account"}, {"esc", "leave"}}, m.th, stops, m.w), m.w))
		out := b.String()
		top := max(0, (m.h-lipgloss.Height(out))/2)
		return strings.Repeat("\n", top) + out
	}
	return b.String()
}

func (m Model) formFields(w int, edit bool) string {
	f := m.form
	stops := f.palette().Stops
	label := func(i int, s string) string {
		if f.focus == i {
			return kit.Gradient(s, lipgloss.NewStyle().Bold(true), stops, 0)
		}
		return kit.S(m.th.Muted).Render(s)
	}
	f.handle.SetWidth(w - 7)
	f.bio.SetWidth(w - 7)
	field := func(i int, body string) string {
		p := kit.Panel{Width: w, Height: 3, Stops: stops, Focused: f.focus == i, Phase: m.panelPhase(), Theme: m.th}
		return p.Render(body)
	}
	var parts []string
	parts = append(parts, label(0, "HANDLE"), field(0, f.handle.View()))
	parts = append(parts, kit.S(m.th.Faint).Render(fmt.Sprintf("%d–%d letters, digits, _ or -", board.MinHandleLen, board.MaxHandleLen)))
	if !edit {
		parts = append(parts, "")
	}
	parts = append(parts, label(1, "PALETTE  ")+kit.S(m.th.Subtle).Render(f.palette().Label), field(1, m.swatches(w-4)))
	if edit {
		flair := board.PostFlairs()[f.flair]
		choice := "None"
		if flair != "" {
			choice = kit.Chip(flair, kit.At(stops, 0.35), true)
		}
		direction := kit.S(m.th.Muted)
		if f.focus == 3 {
			direction = kit.S(kit.At(stops, 0.35)).Bold(true)
		}
		parts = append(parts, label(2, "BIO"), field(2, f.bio.View()),
			label(3, "FLAIR ON NEW POSTS"), " "+direction.Render("‹")+" "+choice+" "+direction.Render("›"))
	}
	return strings.Join(parts, "\n")
}

// swatches is the palette picker: a strip of little gradients. The chosen
// one is tall and bright; the rest sit low and dimmed.
func (m Model) swatches(w int) string {
	n := len(kit.Palettes)
	cell := max(2, min(3, (w-n)/n))
	var b strings.Builder
	for i, p := range kit.Palettes {
		stops := p.Stops
		glyph := "▄"
		if i == m.form.palIdx {
			glyph = "█"
		} else {
			dim := make([]colorT, len(stops))
			for j, c := range stops {
				dim[j] = kit.Dim(c, 0.45)
			}
			stops = dim
		}
		b.WriteString(kit.Gradient(strings.Repeat(glyph, cell), lipgloss.NewStyle(), stops, 0) + " ")
	}
	return kit.Fit(b.String(), w)
}

// previewCard shows the account as others will see it.
func (m Model) previewCard(w int, edit bool) string {
	f := m.form
	stops := f.palette().Stops
	handle := strings.TrimSpace(f.handle.Value())
	if handle == "" {
		handle = "your-handle"
	}
	shimmer := float64(m.frame) / 45
	sigil := kit.Sigil(m.d.Fingerprint, stops, 2)
	name := kit.Gradient(handle, lipgloss.NewStyle().Bold(true), stops, shimmer)
	meta := kit.S(m.th.Muted).Render("new poster")
	if m.acct != nil {
		name += staffFlair(m.acct.Role, kit.At(stops, 0.7))
		meta = kit.S(m.th.Muted).Render(fmt.Sprintf("%s · joined %s", kit.Plural(m.acct.PostCount, "post", "posts"),
			time.Unix(m.acct.CreatedAt, 0).UTC().Format("Jan 2006")))
	}
	idW := max(8, w-4-lipgloss.Width(sigil)-2)
	idBlock := kit.Fit(name, idW) + "\n" + kit.Fit(meta, idW)
	if edit {
		idBlock += "\n" + kit.Fit(postFlair(board.PostFlairs()[f.flair], stops), idW)
	}
	top := kit.SideBySide(2, sigil, idBlock)

	sampleStyle, sample := kit.S(m.th.Subtle), "This is how your posts will look."
	if edit && strings.TrimSpace(f.bio.Value()) != "" {
		sampleStyle, sample = sampleStyle.Italic(true), f.bio.Value()
	}
	// Wrap plain text, then style: wrapping styled text leaves the
	// continuation lines unstyled.
	body := "\n" + top + "\n\n" + kit.Rule(w-4, "─", stops, shimmer) + "\n" + sampleStyle.Render(kit.Wrap(sample, w-4))
	p := kit.Panel{Title: "preview", Width: w, Height: lipgloss.Height(body) + 3, Stops: stops, Focused: true, Phase: shimmer, Theme: m.th}
	return p.Render(body)
}
