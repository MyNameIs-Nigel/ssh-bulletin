// Package tui is the bulletin's interface, built entirely from internal/tui/kit.
//
// Shape: one root Model owns the screen stack (splash → onboarding or home →
// board → thread), the overlays (compose, command palette, help), toasts,
// and a single animation loop that runs only while something is moving.
// Data loads and writes are tea.Cmds, so the UI never blocks on SQLite, and
// live posts from other sessions arrive as messages from the broker.
package tui

import (
	"context"
	"image/color"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// Deps is everything a session's UI needs from the server.
type Deps struct {
	Service     *board.Service
	Fingerprint string
	PublicKey   string
	Suggested   string // SSH username, offered as a first-run handle
	Account     *board.Account
	Events      <-chan board.Event
	Width       int
	Height      int
	IdleTimeout time.Duration
	Context     context.Context
	Now         func() time.Time // tests pin the clock
	NoSplash    bool             // tests skip straight past the splash
	Version     string           // build version for the footer; "" or "dev" shows "dev build"
}

type screen int

const (
	scrSplash screen = iota
	scrOnboard
	scrHome
	scrBoard
	scrThread
	scrProfile
	scrModeration
	scrDirectory
	scrUserProfile
	scrChat
)

type overlayKind int

const (
	ovNone overlayKind = iota
	ovCompose
	ovPalette
	ovHelp
	ovModeration
	ovOptions
	ovLink
)

// Model is the root Bubble Tea model.
type Model struct {
	rtt terminalRTT
	d   Deps
	th  kit.Theme
	w   int
	h   int
	now func() time.Time

	acct   *board.Account
	screen screen

	// Accent cross-fade between places (brand → board palette → ...).
	accentFrom []color.Color
	accentTo   []color.Color
	accentT    float64

	// Animation loop.
	gen       int
	ticking   bool
	frame     int
	born      time.Time
	lastFrame time.Time
	focusAt   time.Time // when focus last changed: borders flow briefly
	lastInput time.Time
	emptyAt   time.Time // when the board in view became empty; zero if it is not
	blinkGen  int       // identifies the live blink timer; stale ones are dropped

	// Data.
	stats          board.Stats
	boards         []board.Board
	boardMenu      kit.Menu
	curBoard       board.Board
	threads        []board.Thread
	threadMenu     kit.Menu
	previews       map[int64][]board.Post   // board screen: opening posts by thread
	peek           map[int64][]board.Thread // home screen: recent threads by board
	curThread      board.Thread
	posts          []board.Post
	postCursor     int
	postTop        int // first visible line of the thread view
	loading        bool
	contentVersion int // invalidates reads started before a visibility change

	// Live chat of the board in view, and whether the board screen's chat
	// section (rather than its thread list) has focus.
	chat      chatState
	chatFocus bool

	// Identity form (onboarding and profile editing).
	form     identityForm
	editFrom screen

	directory      directoryState
	userProfile    userProfileState
	friends        map[int64]bool
	friendsRequest int
	socialRequest  int

	mod         moderationState
	modForm     moderationForm
	opts        optionsState
	modRequest  int
	roleRequest int

	// Overlays.
	overlay  overlayKind
	overlayT float64 // 0..1 open animation
	compose  composeState
	pal      paletteState

	toasts  kit.Toasts
	flash   string // one-line status message in the footer
	flashAt time.Time

	// Hover and links (hover.go, rich.go).
	ui      *frameState // what the last frame drew, shared by every copy
	stamp   int         // bumped by every update that may change the frame
	reuse   bool        // this update changed nothing visible
	hover   string      // hot spot under the pointer
	litWas  string      // what was lit, to restart the flow when it changes
	litAt   time.Time   // when it lit up
	refs    board.Refs
	linkSel int // keyboard-focused link in the selected post, or -1
	link    linkModalState
}

// New builds the model for one session.
func New(d Deps) Model {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Context == nil {
		d.Context = context.Background()
	}
	now := d.Now()
	m := Model{
		d:          d,
		th:         kit.Midnight,
		w:          max(d.Width, 1),
		h:          max(d.Height, 1),
		now:        d.Now,
		acct:       d.Account,
		screen:     scrSplash,
		accentFrom: kit.Midnight.Brand.Stops,
		accentTo:   kit.Midnight.Brand.Stops,
		accentT:    1,
		born:       now,
		lastFrame:  now,
		focusAt:    now,
		lastInput:  now,
		previews:   map[int64][]board.Post{},
		peek:       map[int64][]board.Thread{},
		boardMenu:  kit.NewMenu(nil),
		threadMenu: kit.NewMenu(nil),
		ui:         newFrameState(),
		refs:       board.Refs{},
		linkSel:    -1,
	}
	m.chat.link = -1
	m.form = newIdentityForm(m.th, d.Suggested, m.acct)
	m.compose = newCompose(m.th)
	m.pal = newPalette(m.th)
	if d.NoSplash {
		m.leaveSplash()
	}
	return m
}

// accent is the current (possibly mid-fade) accent gradient.
func (m Model) accent() []color.Color {
	if m.accentT >= 1 {
		return m.accentTo
	}
	return kit.MixStops(m.accentFrom, m.accentTo, smooth(m.accentT))
}

// setAccent starts a cross-fade toward stops.
func (m *Model) setAccent(stops []color.Color) {
	m.accentFrom = m.accent()
	m.accentTo = stops
	m.accentT = 0
}

func smooth(t float64) float64 { return t * t * (3 - 2*t) }

// myStops is the signed-in user's palette (brand if none yet).
func (m Model) myStops() []color.Color {
	if m.acct == nil {
		return kit.PaletteNamed(m.form.palette().Name).Stops
	}
	return kit.PaletteNamed(m.acct.Palette).Stops
}

// Init starts the event listener, the clock, and the splash animation.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{waitEvent(m.d.Events), clockTick(), m.loadHome(), m.loadFriends()}
	if m.screen == scrSplash || m.screen == scrOnboard {
		cmds = append(cmds, kit.Frame(m.gen))
	}
	return tea.Batch(cmds...)
}

// animating reports whether anything on screen is still moving.
func (m Model) animating() bool {
	now := m.now()
	switch {
	case m.screen == scrSplash:
		// The splash is a waiting room: it moves until a key is pressed, so
		// the birds and water never freeze and then jump. The idle timeout
		// (clockTick) bounds how long an abandoned session can run it.
		return true
	case m.screen == scrOnboard && now.Sub(m.lastInput) < ambientFlow:
		// Onboarding shimmers only for a while after the last keypress:
		// someone who walks away from it must not stream 20 fps until the
		// idle timeout. The next key restarts it.
		return true
	case m.onEmptyBoard() && m.blinking():
		return true
	case m.accentT < 1, m.overlay != ovNone && m.overlayT < 1, m.overlay == ovNone && m.overlayT > 0:
		return true
	case m.boardMenu.Animating(), m.threadMenu.Animating(), m.toasts.Animating():
		return true
	case now.Sub(m.focusAt) < focusFlow:
		return true
	case m.loading, m.screen == scrModeration && m.mod.loading:
		return true
	case (m.screen == scrBoard || m.screen == scrChat) && (!m.chat.loaded || m.chat.sending || m.chat.loadingOlder):
		return true
	case m.flowing():
		return true
	}
	return false
}

// focusFlow is how long a newly focused border keeps flowing.
const focusFlow = 1600 * time.Millisecond

// ambientFlow is how long onboarding keeps animating after the
// last input before settling on a still frame.
const ambientFlow = 8 * time.Second

// ensureTicking starts the frame loop if it is not running.
func (m *Model) ensureTicking() tea.Cmd {
	if m.ticking || !m.animating() {
		return nil
	}
	m.ticking = true
	m.gen++
	m.lastFrame = m.now()
	return kit.Frame(m.gen)
}

// touchFocus marks a focus change so borders flow for a moment.
func (m *Model) touchFocus() { m.focusAt = m.now() }

// ---- messages ----------------------------------------------------------

type homeMsg struct {
	version int
	boards  []board.Board
	stats   board.Stats
	err     error
}
type threadsMsg struct {
	version int
	boardID int64
	threads []board.Thread
	err     error
}
type previewMsg struct {
	version  int
	threadID int64
	posts    []board.Post
	refs     board.Refs
}
type postsMsg struct {
	version  int
	threadID int64
	thread   board.Thread
	posts    []board.Post
	err      error
	focus    int64 // post id to put the cursor on, if any
	refs     board.Refs
}
type eventMsg board.Event
type eventsClosedMsg struct{}
type clockMsg struct{}
type blinkMsg struct{ gen int }
type registeredMsg struct {
	acct board.Account
	err  error
}
type postedMsg struct {
	threadID int64
	postID   int64
	err      error
}
type kudosMsg struct{ err error }

// ---- commands ----------------------------------------------------------

func waitEvent(ch <-chan board.Event) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return eventsClosedMsg{}
		}
		return eventMsg(ev)
	}
}

func clockTick() tea.Cmd {
	return tea.Tick(15*time.Second, func(time.Time) tea.Msg { return clockMsg{} })
}

func (m Model) loadHome() tea.Cmd {
	d, version := m.d, m.contentVersion
	return func() tea.Msg {
		bs, err := d.Service.Boards(d.Context)
		if err != nil {
			return homeMsg{version: version, err: err}
		}
		st, err := d.Service.Stats(d.Context)
		return homeMsg{version: version, boards: bs, stats: st, err: err}
	}
}

func (m Model) loadThreads(boardID int64) tea.Cmd {
	d, version := m.d, m.contentVersion
	return func() tea.Msg {
		ts, err := d.Service.Threads(d.Context, boardID)
		return threadsMsg{version: version, boardID: boardID, threads: ts, err: err}
	}
}

func (m Model) loadPreview(threadID int64) tea.Cmd {
	d, version := m.d, m.contentVersion
	return func() tea.Msg {
		ps, _ := d.Service.Posts(d.Context, threadID, 0)
		if len(ps) > 1 {
			ps = ps[:1]
		}
		var refs board.Refs
		if len(ps) > 0 {
			refs, _ = d.Service.ResolveText(d.Context, ps[0].Body)
		}
		return previewMsg{version: version, threadID: threadID, posts: ps, refs: refs}
	}
}

func (m Model) loadPosts(threadID, focus int64) tea.Cmd {
	d, version, viewer := m.d, m.contentVersion, m.viewerID()
	return func() tea.Msg {
		t, err := d.Service.Thread(d.Context, threadID)
		if err != nil {
			return postsMsg{version: version, threadID: threadID, err: err}
		}
		ps, err := d.Service.Posts(d.Context, threadID, viewer)
		if err == nil && len(ps) == 0 {
			err = board.ErrNotFound
		}
		bodies := make([]string, len(ps))
		for i, p := range ps {
			bodies[i] = p.Body
		}
		refs, _ := d.Service.ResolveText(d.Context, bodies...)
		return postsMsg{version: version, threadID: threadID, thread: t, posts: ps, err: err, focus: focus, refs: refs}
	}
}

// mergeRefs keeps the latest resolution of every reference seen.
func (m *Model) mergeRefs(refs board.Refs) {
	if len(refs) == 0 {
		return
	}
	if len(m.refs)+len(refs) > 4096 {
		m.refs = board.Refs{}
	}
	for k, v := range refs {
		m.refs[k] = v
	}
}

func (m Model) viewerID() int64 {
	if m.acct == nil {
		return 0
	}
	return m.acct.ID
}

// The empty-board cat first blinks blinkFirst after the empty board appears,
// then every blinkEvery, shutting its eyes for blinkFor. Blinking is a pure
// function of the clock; a timer aimed at each blink wakes the frame loop
// just for it, so the resting cat costs no frames at all.
const (
	blinkFirst = 3 * time.Second
	blinkEvery = 5 * time.Second
	blinkFor   = 350 * time.Millisecond
)

// onEmptyBoard reports whether the board screen is showing its empty state.
func (m Model) onEmptyBoard() bool {
	return m.screen == scrBoard && !m.loading && len(m.threads) == 0
}

// untilBlink is how long until the next blink starts, counted from when the
// empty board appeared (zero while one is under way).
func (m Model) untilBlink() time.Duration {
	e := m.now().Sub(m.emptyAt)
	if e < blinkFirst {
		return blinkFirst - e
	}
	return (blinkEvery - (e-blinkFirst)%blinkEvery) % blinkEvery
}

// blinking reports whether the cat's eyes are shut right now.
func (m Model) blinking() bool {
	if m.emptyAt.IsZero() {
		return false
	}
	e := m.now().Sub(m.emptyAt)
	return e >= blinkFirst && (e-blinkFirst)%blinkEvery < blinkFor
}

// blinkTick fires just after the next blink begins.
func (m Model) blinkTick() tea.Cmd {
	gen := m.blinkGen
	return tea.Tick(m.untilBlink()+20*time.Millisecond, func(time.Time) tea.Msg { return blinkMsg{gen: gen} })
}
