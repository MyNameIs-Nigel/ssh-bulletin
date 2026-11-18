package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

type moderationTab int

const (
	modQueue moderationTab = iota
	modHistory
	modRemoved
	modAdmins
)

var moderationTabs = [...]string{"Queue", "History", "Removed", "Admins"}

type moderationState struct {
	tab       moderationTab
	page      int
	cursor    int
	entries   []board.ModerationEntry
	admins    []board.Account
	more      bool
	loading   bool
	err       string
	detail    *board.ModerationEntry
	detailTop int
}

func (s moderationState) count() int {
	return len(s.entries) + len(s.admins)
}

type moderationPageMsg struct {
	request int
	entries []board.ModerationEntry
	admins  []board.Account
	err     error
}

type moderationResultMsg struct {
	message string
	err     error
}

type roleMsg struct {
	request int
	role    board.Role
	err     error
}

func (m Model) canModerate() bool { return m.acct != nil && m.acct.Role.CanModerate() }
func (m Model) isOwner() bool     { return m.acct != nil && m.acct.Role == board.RoleOwner }

func (m *Model) refreshRole() tea.Cmd {
	if m.acct == nil {
		return nil
	}
	m.roleRequest++
	request, d, actor := m.roleRequest, m.d, m.acct.ID
	return func() tea.Msg {
		a, err := d.Service.Account(d.Context, actor)
		return roleMsg{request: request, role: a.Role, err: err}
	}
}

func (m *Model) leaveModeration() {
	m.mod = moderationState{}
	m.modRequest++
	m.modForm = moderationForm{}
	if m.overlay == ovModeration {
		m.overlay, m.overlayT, m.compose.lastShown = ovNone, 0, ovNone
	}
	if m.screen == scrModeration {
		m.goHome()
	}
}

func (m *Model) openModeration() tea.Cmd {
	if !m.canModerate() || m.screen == scrModeration {
		return nil
	}
	m.screen = scrModeration
	m.mod = moderationState{}
	m.setAccent(m.th.Brand.Stops)
	m.touchFocus()
	return m.loadModeration()
}

func (m *Model) loadModeration() tea.Cmd {
	if m.screen != scrModeration || !m.canModerate() {
		return nil
	}
	m.modRequest++
	d, id, request := m.d, m.acct.ID, m.modRequest
	tab, page := m.mod.tab, m.mod.page
	m.mod.loading = true
	return func() tea.Msg {
		msg := moderationPageMsg{request: request}
		if tab == modAdmins {
			msg.admins, msg.err = d.Service.Admins(d.Context, id, page)
		} else {
			msg.entries, msg.err = d.Service.ModerationPage(d.Context, id, board.ModerationView(tab), page)
		}
		return msg
	}
}

func (m *Model) receiveModerationPage(msg moderationPageMsg) tea.Cmd {
	if m.screen != scrModeration || msg.request != m.modRequest {
		return nil
	}
	if errors.Is(msg.err, board.ErrForbidden) {
		m.leaveModeration()
		return m.refreshRole()
	}
	s := &m.mod
	s.loading = false
	if msg.err != nil {
		s.err = friendly(msg.err)
		return nil
	}
	s.err = ""
	s.more = len(msg.entries)+len(msg.admins) > board.ModerationPageSize
	s.entries = msg.entries[:min(len(msg.entries), board.ModerationPageSize)]
	s.admins = msg.admins[:min(len(msg.admins), board.ModerationPageSize)]
	if s.count() == 0 && s.page > 0 {
		s.page--
		return m.loadModeration()
	}
	s.cursor = max(0, min(s.cursor, s.count()-1))
	return nil
}

func (m *Model) updateModeration(k tea.KeyPressMsg) tea.Cmd {
	s := &m.mod
	key := k.String()
	if s.detail != nil {
		switch key {
		case "esc", "backspace", "q", "left":
			s.detail = nil
		case "up", "k":
			s.detailTop = max(0, s.detailTop-1)
		case "down", "j":
			s.detailTop++
		case "pgup":
			s.detailTop = max(0, s.detailTop-5)
		case "pgdown":
			s.detailTop += 5
		default:
			m.moderateEntry(key, *s.detail)
		}
		if s.detail != nil && s.detailTop > 0 {
			w, h := m.moderationPanel().Inner()
			s.detailTop = min(s.detailTop, max(0, len(m.moderationDetailLines(w))-h))
		}
		return nil
	}
	switch key {
	case "1", "2", "3", "4":
		tab := moderationTab(key[0] - '1')
		if tab == modAdmins && !m.isOwner() {
			return nil
		}
		m.mod = moderationState{tab: tab}
		return m.loadModeration()
	case "up", "k":
		s.cursor = max(0, s.cursor-1)
	case "down", "j":
		s.cursor = max(0, min(s.cursor+1, s.count()-1))
	case "pgup":
		if s.page > 0 {
			m.mod = moderationState{tab: s.tab, page: s.page - 1}
			return m.loadModeration()
		}
	case "pgdown":
		if s.more {
			m.mod = moderationState{tab: s.tab, page: s.page + 1}
			return m.loadModeration()
		}
	case "r":
		return m.loadModeration()
	case "a", "v":
		if s.tab == modAdmins && m.isOwner() {
			m.startAdminForm(key == "a")
		}
	default:
		if s.cursor < len(s.entries) {
			entry := s.entries[s.cursor]
			if key == "enter" {
				s.detail, s.detailTop = &entry, 0
			} else {
				m.moderateEntry(key, entry)
			}
		}
	}
	return nil
}

func (m *Model) moderateEntry(key string, entry board.ModerationEntry) {
	if !m.canModerate() {
		return
	}
	switch {
	case key == "d" && m.mod.tab == modQueue:
		m.startModerationForm(board.Dismissed, entry.PostID, entry.Title, entry.Opening)
	case key == "x" && m.mod.tab == modQueue:
		m.startModerationForm(board.Removed, entry.PostID, entry.Title, entry.Opening)
	case key == "u" && m.mod.tab == modRemoved:
		m.startModerationForm(board.Restored, entry.PostID, entry.Title, entry.Opening)
	}
}

func (m Model) moderationPanel() kit.Panel {
	r := m.threadRect()
	return kit.Panel{Title: "Moderation", Width: r.w, Height: r.h, Stops: m.accent(), Focused: true, Phase: m.panelPhase(), Theme: m.th, PadX: 2}
}

func (m Model) moderationDetailLines(w int) []string {
	e := m.mod.detail
	header := fmt.Sprintf("#%d · %s · @%s", e.PostID, postKind(e.Opening), e.Author)
	action := fmt.Sprintf("%s by @%s · %s UTC", e.Action, e.Actor, time.Unix(e.CreatedAt, 0).UTC().Format("2006-01-02 15:04"))
	return strings.Split(kit.Wrap(e.Title+"\n"+header+"\n"+action+"\n"+e.Reason+"\n\n"+e.Body, w), "\n")
}

func (m Model) viewModeration() string {
	s := m.mod
	r := m.threadRect()
	p := m.moderationPanel()
	w, h := p.Inner()
	if s.detail != nil {
		lines := m.moderationDetailLines(w)
		top := min(s.detailTop, max(0, len(lines)-h))
		if len(lines) > h {
			p.Note = fmt.Sprintf("lines %d–%d / %d", top+1, top+h, len(lines))
		}
		return pad(r.x, p.Render(strings.Join(lines[top:min(top+h, len(lines))], "\n")))
	}
	var tabs []string
	for i, name := range moderationTabs {
		if moderationTab(i) == modAdmins && !m.isOwner() {
			break
		}
		label := fmt.Sprintf("%d %s", i+1, name)
		if moderationTab(i) == s.tab {
			label = kit.S(m.th.Text).Bold(true).Render(label)
		} else {
			label = kit.S(m.th.Muted).Render(label)
		}
		tabs = append(tabs, label)
	}
	lines := []string{strings.Join(tabs, "  ·  "), ""}
	if s.tab == modAdmins {
		lines[1] = "Grant or revoke by handle and SSH fingerprint"
	}
	p.Note = fmt.Sprintf("page %d", s.page+1)
	switch {
	case s.err != "":
		lines = append(lines, kit.S(m.th.Bad).Render(s.err))
	case s.loading && s.count() == 0:
		lines = append(lines, kit.Spinner(m.frame, m.accent())+" loading…")
	case s.count() == 0:
		lines = append(lines, "Nothing here yet.")
	default:
		h -= len(lines)
		top := max(0, s.cursor-h+1)
		for i := top; i < min(s.count(), top+h); i++ {
			var label string
			if s.tab == modAdmins {
				label = "@" + s.admins[i].Handle + " [A]"
			} else {
				e := s.entries[i]
				label = fmt.Sprintf("#%d %s · %s · %s", e.PostID, e.Action, e.Title, e.Reason)
			}
			marker := "  "
			if i == s.cursor {
				marker = kit.S(m.th.Text).Bold(true).Render("› ")
			}
			lines = append(lines, marker+kit.Fit(label, w-2))
		}
	}
	return pad(r.x, p.Render(strings.Join(lines, "\n")))
}

func postKind(opening bool) string {
	if opening {
		return "opening post"
	}
	return "reply"
}

type moderationForm struct {
	action  board.ModerationAction // empty for admin management
	postID  int64
	target  string
	opening bool
	grant   bool
	first   textinput.Model // reason or handle
	second  textinput.Model // fingerprint
	focus   int
	busy    bool
	err     string
}

func (m *Model) startModerationForm(action board.ModerationAction, postID int64, title string, opening bool) {
	m.modForm = moderationForm{action: action, postID: postID, target: title, opening: opening,
		first: m.moderationInput("Reason", board.MaxReasonLen)}
	m.openModerationForm()
}

func (m *Model) startAdminForm(grant bool) {
	m.modForm = moderationForm{grant: grant, first: m.moderationInput("Handle", board.MaxHandleLen),
		second: m.moderationInput("SHA256 fingerprint", 80)}
	m.openModerationForm()
}

func (m Model) moderationInput(hint string, limit int) textinput.Model {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = hint
	in.CharLimit = limit
	kit.StyleInput(&in, m.th, m.accent())
	return in
}

func (m *Model) openModerationForm() {
	m.modForm.first.Focus()
	m.compose.lastShown = ovModeration
	m.openOverlay(ovModeration)
}

func (m *Model) updateModerationForm(msg tea.Msg) tea.Cmd {
	f := &m.modForm
	if f.busy {
		return nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			m.closeOverlay()
			return nil
		case "tab", "shift+tab":
			if f.action == "" {
				f.switchField()
			}
			return nil
		case "enter":
			if f.action == "" && f.focus == 0 {
				f.switchField()
				return nil
			}
			return m.submitModerationForm()
		case "ctrl+s":
			return m.submitModerationForm()
		}
	}
	f.err = ""
	var cmd tea.Cmd
	if f.focus == 0 {
		f.first, cmd = f.first.Update(msg)
	} else {
		f.second, cmd = f.second.Update(msg)
	}
	return cmd
}

func (f *moderationForm) switchField() {
	f.focus = 1 - f.focus
	if f.focus == 0 {
		f.second.Blur()
		f.first.Focus()
	} else {
		f.first.Blur()
		f.second.Focus()
	}
}

func (m *Model) submitModerationForm() tea.Cmd {
	if m.acct == nil {
		return nil
	}
	m.modForm.busy = true
	f, d, id := m.modForm, m.d, m.acct.ID
	first, second := f.first.Value(), f.second.Value()
	return func() tea.Msg {
		switch f.action {
		case "":
			target, err := d.Service.SetAdmin(d.Context, id, first, second, f.grant)
			message := "Admin access revoked for @" + target.Handle
			if f.grant {
				message = "Admin access granted to @" + target.Handle
			}
			return moderationResultMsg{message: message, err: err}
		case board.Reported:
			return moderationResultMsg{message: "Report sent to moderators", err: d.Service.Report(d.Context, id, f.postID, first)}
		default:
			return moderationResultMsg{message: "Post " + string(f.action), err: d.Service.Moderate(d.Context, id, f.postID, f.action, first)}
		}
	}
}

func (m Model) viewModerationForm() string {
	f := m.modForm
	w := min(m.w-4, 78)
	f.first.SetWidth(w - 9)
	var title, context string
	switch f.action {
	case board.Reported:
		title = "Report post"
		f.first.Placeholder = "Reason (spam, harassment, off-topic…)"
	case board.Dismissed:
		title = "Dismiss reports for this post"
	case board.Removed:
		title = "Remove " + postKind(f.opening)
		if f.opening {
			title = "Remove opening post"
		}
	case board.Restored:
		title = "Restore " + postKind(f.opening)
		if f.opening {
			title = "Restore thread"
		}
	default:
		title = "Revoke admin access"
		if f.grant {
			title = "Grant admin access"
		}
		context = "Use the full fingerprint from the player's profile."
	}
	if f.action != "" {
		context = kit.Fit(fmt.Sprintf("#%d · %s", f.postID, f.target), w-6)
	}
	lines := []string{"", context, "", f.first.View()}
	hints := []kit.Hint{{"⏎", "confirm"}, {"esc", "cancel"}}
	if f.action == "" {
		f.second.SetWidth(w - 9)
		lines = append(lines, f.second.View())
		hints = append([]kit.Hint{{"tab", "field"}}, hints...)
	}
	lines = append(lines, "")
	switch {
	case f.err != "":
		lines = append(lines, kit.S(m.th.Bad).Render(kit.Wrap(f.err, w-6)))
	case f.busy:
		lines = append(lines, "Saving…")
	default:
		lines = append(lines, kit.Hints(hints, m.th, m.accent(), w-6))
	}
	body := strings.Join(lines, "\n")
	p := kit.Panel{Title: title, Width: w, Height: lipgloss.Height(body) + 2,
		Stops: m.accent(), Focused: true, Theme: m.th, Fill: m.th.Overlay, PadX: 2}
	return p.Render(body)
}

func (m Model) moderationHints() []kit.Hint {
	var hints []kit.Hint
	switch m.mod.tab {
	case modQueue:
		hints = []kit.Hint{{"d", "dismiss"}, {"x", "remove"}}
	case modRemoved:
		hints = []kit.Hint{{"u", "restore"}}
	case modAdmins:
		hints = []kit.Hint{{"a", "grant"}, {"v", "revoke"}}
	}
	if m.mod.detail != nil {
		return append(hints, kit.Hint{"↑↓", "scroll"}, kit.Hint{"esc", "list"})
	}
	if m.mod.tab != modAdmins {
		hints = append(hints, kit.Hint{"⏎", "details"})
	}
	return append(hints, kit.Hint{"pgup/dn", "page"}, kit.Hint{"esc", "back"})
}
