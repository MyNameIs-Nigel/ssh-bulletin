package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

var friendBlue = kit.Hex("#4B9CFF")

type directoryState struct {
	from        screen
	input       textinput.Model
	menu        kit.Menu
	entries     []board.ProfileEntry
	friendsOnly bool
	loading     bool
	more        bool
	err         string
}

type userProfileState struct {
	from   screen
	id     int64
	data   board.Profile
	recent int
	saving bool
	err    string
}

type directoryMsg struct {
	request int
	entries []board.ProfileEntry
	more    bool
	err     error
}

type userProfileMsg struct {
	request int
	profile board.Profile
	err     error
}

type friendsMsg struct {
	request int
	ids     map[int64]bool
	err     error
}

type friendSavedMsg struct {
	on  bool
	err error
}

func (m Model) loadFriends() tea.Cmd {
	d, viewer, request := m.d, m.viewerID(), m.friendsRequest
	if viewer == 0 {
		return nil
	}
	return func() tea.Msg {
		ids, err := d.Service.FriendIDs(d.Context, viewer)
		return friendsMsg{request: request, ids: ids, err: err}
	}
}

func (m *Model) refreshFriends() tea.Cmd {
	m.friendsRequest++
	return m.loadFriends()
}

func (m Model) friendFlair(id int64) string {
	if m.friends[id] {
		return " " + kit.Chip("F", friendBlue, true)
	}
	return ""
}

func postFlair(flair string, stops []colorT) string {
	if flair == "" {
		return ""
	}
	return " " + kit.Chip(flair, kit.At(stops, 0.35), true)
}

func (m *Model) openDirectory() tea.Cmd {
	dir := directoryState{from: m.screen, input: textinput.New(), menu: kit.NewMenu(nil)}
	dir.menu.Rows = 1
	dir.input.Prompt = "@ "
	dir.input.Placeholder = "Search usernames"
	dir.input.CharLimit = board.MaxHandleLen + 1
	dir.input.Focus()
	kit.StyleInput(&dir.input, m.th, m.th.Brand.Stops)
	m.directory = dir
	m.screen = scrDirectory
	m.setAccent(m.th.Brand.Stops)
	m.touchFocus()
	return m.searchDirectory(true)
}

func (m *Model) searchDirectory(reset bool) tea.Cmd {
	m.socialRequest++
	dir := &m.directory
	dir.loading, dir.err = true, ""
	if reset {
		dir.entries, dir.more = nil, false
		dir.menu.SetItems(nil)
		dir.menu.Snap()
	}
	d, viewer, request, query, only := m.d, m.viewerID(), m.socialRequest, dir.input.Value(), dir.friendsOnly
	return func() tea.Msg {
		entries, more, err := d.Service.SearchProfiles(d.Context, viewer, query, only)
		return directoryMsg{request: request, entries: entries, more: more, err: err}
	}
}

func (m *Model) syncDirectoryItems() {
	items := make([]kit.Item, len(m.directory.entries))
	for i, a := range m.directory.entries {
		it := kit.Item{Title: "@" + a.Handle, Stops: kit.PaletteNamed(a.Palette).Stops}
		if m.friends[a.ID] {
			it.Badge, it.BadgeInk = "F", friendBlue
		}
		if a.Role.CanModerate() {
			it.Meta = string(a.Role)
		}
		items[i] = it
	}
	m.directory.menu.SetItems(items)
}

func (m *Model) updateDirectory(msg tea.Msg) tea.Cmd {
	dir := &m.directory
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			return m.returnFromSocial(dir.from)
		case "tab":
			dir.friendsOnly = !dir.friendsOnly
			return m.searchDirectory(true)
		case "up":
			dir.menu.Move(-1)
			dir.menu.Snap()
			return nil
		case "down":
			dir.menu.Move(1)
			dir.menu.Snap()
			return nil
		case "enter":
			if i := dir.menu.Selected(); i >= 0 {
				return m.openUserProfile(dir.entries[i].ID)
			}
			return nil
		}
	}
	before := dir.input.Value()
	var cmd tea.Cmd
	dir.input, cmd = dir.input.Update(msg)
	if dir.input.Value() != before {
		return tea.Batch(cmd, m.searchDirectory(true))
	}
	return cmd
}

func (m Model) socialRect() rect {
	w := min(m.w-2, 84)
	return rect{(m.w - w) / 2, m.body().y, w, m.body().h}
}

// The directory's first three inner rows hold the tabs, input, and rule.
const directoryHeaderRows = 3

func (m Model) viewDirectory() string {
	r := m.socialRect()
	dir := m.directory
	stops := m.th.Brand.Stops
	p := kit.Panel{Title: "◇ User directory", Width: r.w, Height: r.h, Stops: stops,
		Focused: true, Phase: m.panelPhase(), Theme: m.th}
	iw, ih := p.Inner()
	all, friends := "All users", "My friends"
	if dir.friendsOnly {
		friends = kit.GradientBold(friends, stops)
		all = kit.S(m.th.Muted).Render(all)
	} else {
		all = kit.GradientBold(all, stops)
		friends = kit.S(m.th.Muted).Render(friends)
	}
	dir.input.SetWidth(iw - 3)
	header := all + "  /  " + friends + kit.S(m.th.Faint).Render("  [tab]") + "\n" + dir.input.View() + "\n" +
		kit.FadeRule(iw, "─", kit.At(stops, 0.5), m.th.BgBottom)
	var body string
	switch {
	case dir.err != "":
		body = kit.S(m.th.Bad).Render(dir.err)
	case len(dir.entries) > 0:
		body = dir.menu.Render(iw, ih-directoryHeaderRows, m.th, true)
	case dir.loading:
		body = kit.S(m.th.Muted).Render("Searching…")
	case dir.friendsOnly && dir.input.Value() == "":
		body = kit.S(m.th.Muted).Render("Open a profile to add a friend. Only you see your list.")
	default:
		body = kit.S(m.th.Muted).Render("No matching usernames.")
	}
	if dir.more {
		p.Note = fmt.Sprintf("First %d · refine by username", board.DirectoryLimit)
	}
	return kit.Indent(p.Render(header+"\n"+body), r.x)
}

func (m *Model) openUserProfile(id int64) tea.Cmd {
	m.userProfile = userProfileState{from: m.screen, id: id}
	m.screen = scrUserProfile
	m.touchFocus()
	return m.loadUserProfile()
}

func (m *Model) loadUserProfile() tea.Cmd {
	m.socialRequest++
	d, request, id := m.d, m.socialRequest, m.userProfile.id
	return func() tea.Msg {
		profile, err := d.Service.Profile(d.Context, id)
		return userProfileMsg{request: request, profile: profile, err: err}
	}
}

func (m *Model) updateUserProfile(k tea.KeyPressMsg) tea.Cmd {
	u := &m.userProfile
	switch k.String() {
	case "esc", "backspace", "q":
		if !u.saving {
			return m.returnFromSocial(u.from)
		}
	case "up", "k":
		u.recent = max(0, u.recent-1)
	case "down", "j":
		u.recent = max(0, min(len(u.data.Recent)-1, u.recent+1))
	case "enter":
		if !u.saving && u.recent < len(u.data.Recent) {
			p := u.data.Recent[u.recent]
			for _, b := range m.boards {
				if b.ID == p.BoardID {
					m.curBoard = b
					m.setAccent(kit.PaletteNamed(b.Palette).Stops)
					break
				}
			}
			return m.enterThread(board.Thread{ID: p.ThreadID, BoardID: p.BoardID}, p.PostID)
		}
	case "e":
		if u.data.ID == m.viewerID() {
			m.openProfile()
		}
	case "f":
		viewer := m.viewerID()
		if u.saving || u.data.ID == 0 || viewer == u.id {
			return nil
		}
		u.saving, u.err = true, ""
		d, id, on := m.d, u.id, !m.friends[u.id]
		return func() tea.Msg {
			err := d.Service.SetFriend(d.Context, viewer, id, on)
			return friendSavedMsg{on: on, err: err}
		}
	}
	return nil
}

func (m *Model) returnFromSocial(to screen) tea.Cmd {
	m.screen = to
	m.touchFocus()
	switch to {
	case scrDirectory:
		m.setAccent(m.th.Brand.Stops)
		return m.searchDirectory(false)
	case scrUserProfile:
		m.setAccent(kit.PaletteNamed(m.userProfile.data.Palette).Stops)
		return m.loadUserProfile()
	case scrThread:
		m.setAccent(kit.PaletteNamed(m.curBoard.Palette).Stops)
		return m.loadPosts(m.curThread.ID, 0)
	case scrBoard:
		m.setAccent(kit.PaletteNamed(m.curBoard.Palette).Stops)
		return m.loadThreads(m.curBoard.ID)
	case scrModeration:
		if m.canModerate() {
			m.setAccent(m.th.Brand.Stops)
			return m.loadModeration()
		}
		m.goHome()
		return m.loadHome()
	default:
		m.goHome()
		return m.loadHome()
	}
}

func (m Model) viewUserProfile() string {
	r := m.socialRect()
	u := m.userProfile
	a := u.data
	stops := m.accent()
	p := kit.Panel{Title: "◇ Profile", Width: r.w, Height: r.h, Stops: stops,
		Focused: true, Phase: m.panelPhase(), Theme: m.th}
	if a.ID == 0 {
		status := kit.S(m.th.Muted).Render("Loading profile…")
		if u.err != "" {
			status = kit.S(m.th.Bad).Render(u.err)
		}
		return kit.Indent(p.Render(status), r.x)
	}
	iw, _ := p.Inner()
	stops = kit.PaletteNamed(a.Palette).Stops
	name := kit.GradientBold("@"+a.Handle, stops) + m.friendFlair(a.ID) + staffFlair(a.Role, kit.At(stops, 0.7))
	meta := kit.Plural(a.PostCount, "post", "posts") + " · joined " + time.Unix(a.CreatedAt, 0).UTC().Format("Jan 2006")
	flair := "No flair"
	if a.Flair != "" {
		flair = "New post flair:" + postFlair(a.Flair, stops)
	}
	if a.ID == m.viewerID() {
		p.Note = a.Fingerprint
	}
	if u.err != "" {
		p.Note = u.err
	}
	top := kit.SideBySide(2, kit.Sigil(a.Fingerprint, stops, 1), name+"\n"+kit.S(m.th.Muted).Render(meta)+"\n"+kit.S(m.th.Subtle).Render(flair))
	bio := a.Bio
	if bio == "" {
		bio = "No bio yet."
	}
	var body strings.Builder
	body.WriteString(top + "\n\n" + kit.S(m.th.Subtle).Render(kit.Wrap(bio, iw)) + "\n\n")
	body.WriteString(kit.GradientBold("Recent activity", stops) + "\n")
	if len(a.Recent) == 0 {
		body.WriteString(kit.S(m.th.Muted).Render("No public posts yet."))
	}
	for i, activity := range a.Recent {
		prefix, ink := "  ", m.th.Subtle
		if i == u.recent {
			prefix, ink = "› ", m.th.Text
		}
		verb := "Posted"
		if activity.IsReply {
			verb = "Replied"
		}
		detail := kit.S(m.th.Muted).Render(verb + " · " + activity.Board + " · " + kit.Ago(activity.CreatedAt, m.now()))
		body.WriteString(kit.S(ink).Render(prefix+kit.Fit(activity.Title, iw-lipgloss.Width(detail)-4)) + "  " + detail + "\n")
	}
	return kit.Indent(p.Render(body.String()), r.x)
}
