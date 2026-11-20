package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/markup"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

type searchState struct {
	input                         textinput.Model
	menu                          kit.Menu
	results                       []board.SearchResult
	query                         string
	page                          int
	more, loading, focusedResults bool
	err                           string
}

type searchLoadedMsg struct {
	request int
	results []board.SearchResult
	more    bool
	err     error
}

func (m *Model) openSearch() tea.Cmd {
	in := textinput.New()
	in.Prompt, in.Placeholder, in.CharLimit = "› ", "Search thread titles and posts", board.MaxSearchLen
	kit.StyleInput(&in, m.th, m.th.Brand.Stops)
	in.Focus()
	m.search = searchState{input: in, menu: kit.NewMenu(nil)}
	m.searchRequest++
	m.screen = scrSearch
	m.setAccent(m.th.Brand.Stops)
	m.touchFocus()
	return nil
}

func (m *Model) runSearch(page int) tea.Cmd {
	s := &m.search
	s.page, s.loading, s.err, s.results = page, true, "", nil
	s.menu = kit.NewMenu(nil)
	m.searchRequest++
	d, viewer, request, query := m.d, m.viewerID(), m.searchRequest, s.query
	return func() tea.Msg {
		results, more, err := d.Service.SearchThreads(d.Context, viewer, query, page)
		return searchLoadedMsg{request: request, results: results, more: more, err: err}
	}
}

func (m *Model) receiveSearch(msg searchLoadedMsg) {
	if m.screen != scrSearch || msg.request != m.searchRequest {
		return
	}
	s := &m.search
	s.loading = false
	if msg.err != nil {
		s.err = "Couldn't search. Press Enter to try again."
		s.input.Focus()
		s.focusedResults = false
		return
	}
	s.results, s.more = msg.results, msg.more
	items := make([]kit.Item, len(s.results))
	for i, r := range s.results {
		title := m.listingTitle(board.Thread{ID: r.ThreadID, Title: r.Title, AuthorID: r.ThreadAuthorID})
		items[i] = kit.Item{Title: title, Desc: board.CleanLine(r.Excerpt), Meta: r.Board, Stops: m.stopsForBoard(r.BoardID)}
	}
	s.menu.SetItems(items)
	s.focusedResults = len(items) > 0
	if s.focusedResults {
		s.input.Blur()
	} else {
		s.input.Focus()
	}
}

func (m *Model) updateSearch(msg tea.Msg) tea.Cmd {
	s := &m.search
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			m.goHome()
			return nil
		case "tab", "shift+tab":
			if len(s.results) == 0 {
				return nil
			}
			s.focusedResults = !s.focusedResults
			if s.focusedResults {
				s.input.Blur()
			} else {
				s.input.Focus()
			}
			return nil
		case "enter":
			if s.focusedResults {
				return m.openSearchResult(s.menu.Selected())
			}
			s.query = strings.TrimSpace(s.input.Value())
			if s.query != "" {
				return m.runSearch(0)
			}
			s.results, s.more, s.loading, s.err = nil, false, false, ""
			m.searchRequest++
			return nil
		}
		if s.focusedResults {
			switch k.String() {
			case "up", "k":
				s.menu.Move(-1)
			case "down", "j":
				s.menu.Move(1)
			case "pgup":
				if s.page > 0 && !s.loading {
					return m.runSearch(s.page - 1)
				}
			case "pgdown":
				if s.more && !s.loading {
					return m.runSearch(s.page + 1)
				}
			}
			s.menu.Snap()
			return nil
		}
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return cmd
}

func (m *Model) openSearchResult(i int) tea.Cmd {
	if i < 0 || i >= len(m.search.results) {
		return nil
	}
	m.search.menu.Select(i)
	r := m.search.results[i]
	jump := m.follow(board.Target{Ref: markup.Ref{Kind: markup.Post}, BoardID: r.BoardID, ThreadID: r.ThreadID, PostID: r.PostID})
	m.threadFromSearch = true
	return jump
}

func (m Model) viewSearch() string {
	s := m.search
	r := m.threadRect()
	p := kit.Panel{Title: "Search threads", Width: r.w, Height: r.h, Stops: m.accent(), Focused: true, Phase: m.panelPhase(), Theme: m.th, PadX: 2}
	w, h := p.Inner()
	s.input.SetWidth(max(1, w-3))
	input := m.hot("search:input", func(m *Model) tea.Cmd {
		m.search.focusedResults = false
		m.search.input.Focus()
		return nil
	}, s.input.View())
	body := "\n" + input + "\n" + kit.Rule(w, "─", m.accent(), 0) + "\n"
	switch {
	case s.err != "":
		body += s.err
	case s.loading:
		body += "Searching…"
	case s.query == "":
		body += "Enter keywords to search."
	case len(s.results) == 0:
		body += "No matching threads."
	default:
		p.Note = fmt.Sprintf("page %d", s.page+1)
		menu := s.menu
		m.menuHot(&menu, "search", (*Model).openSearchResult)
		body += menu.Render(w, max(1, h-3), m.th, s.focusedResults)
	}
	return pad(r.x, p.Render(body))
}
