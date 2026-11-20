package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

// chatFixture is a service with a clock the test advances, so chat quotas can
// be stepped past, plus two long-standing members and the owner.
type chatFixture struct {
	svc               *board.Service
	now               *time.Time
	owner, alice, bob board.Account
	board             board.Board
}

func newChatFixture(t *testing.T) chatFixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := fixedNow.Add(-72 * time.Hour)
	f := chatFixture{now: &now}
	f.svc = board.NewService(st, board.NewBroker(), func() time.Time { return *f.now })
	ownerKey := identity.FingerprintFromBytes([32]byte{1})
	if err := f.svc.ConfigureOwner(ctx, ownerKey); err != nil {
		t.Fatal(err)
	}
	reg := func(key byte, handle string) board.Account {
		a, err := f.svc.Register(ctx, identity.FingerprintFromBytes([32]byte{key}), "", handle, "reef")
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	f.owner, f.alice, f.bob = reg(1, "Owner"), reg(2, "Alice"), reg(3, "Bob")
	boards, err := f.svc.Boards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.board = boards[0]
	*f.now = fixedNow.Add(-time.Hour) // members are a couple of days old
	return f
}

// say sends a chat message as a, stepping the clock past the burst quota.
func (f chatFixture) say(t *testing.T, a board.Account, body string) board.ChatMessage {
	t.Helper()
	*f.now = f.now.Add(4 * time.Second)
	msg, err := f.svc.SendChat(context.Background(), a, f.board.ID, body)
	if err != nil {
		t.Fatalf("say %q: %v", body, err)
	}
	return msg
}

func typeText(s string) []tea.Msg {
	var msgs []tea.Msg
	for _, r := range s {
		msgs = append(msgs, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return msgs
}

func screenText(m Model) string { return ansi.Strip(m.View().Content) }

// openChatModel lands a on the first board's chat screen.
func (f chatFixture) openChatModel(t *testing.T, a board.Account, w, h int) Model {
	t.Helper()
	m := drive(t, modelFor(f.svc, a, w, h), tea.WindowSizeMsg{Width: w, Height: h}, key("enter"), key("tab"))
	if m.screen != scrBoard || !m.chatFocus {
		t.Fatalf("tab on the board: screen=%v chatFocus=%v", m.screen, m.chatFocus)
	}
	m = drive(t, m, key("enter"))
	if m.screen != scrChat || !m.chat.loaded {
		t.Fatalf("enter on the chat section: screen=%v loaded=%v", m.screen, m.chat.loaded)
	}
	return m
}

func TestBoardChatSectionShowsTailAndFocus(t *testing.T) {
	f := newChatFixture(t)
	f.say(t, f.alice, "first words")
	f.say(t, f.bob, "the newest line")
	m := drive(t, modelFor(f.svc, f.alice, 120, 32), tea.WindowSizeMsg{Width: 120, Height: 32}, key("enter"))
	if m.screen != scrBoard || m.chatFocus {
		t.Fatalf("screen=%v chatFocus=%v", m.screen, m.chatFocus)
	}
	text := screenText(m)
	for _, want := range []string{"# live chat", "Bob  the newest line", "Alice  first words", "tab chat"} {
		if !strings.Contains(text, want) {
			t.Errorf("board screen lacks %q:\n%s", want, text)
		}
	}
	// ↓ past the last thread reaches the chat; ↑ goes back.
	m = drive(t, m, key("down"))
	if !m.chatFocus {
		t.Fatal("down past the last thread did not focus the chat")
	}
	if !strings.Contains(screenText(m), "join the chat") {
		t.Fatal("focused chat section has no join hint")
	}
	m = drive(t, m, key("up"))
	if m.chatFocus {
		t.Fatal("up from the chat section did not return to the threads")
	}
}

func TestChatHintAndMouseFocusAt80By24(t *testing.T) {
	f := newChatFixture(t)
	m := drive(t, modelFor(f.svc, f.alice, 80, 24), key("enter"))
	if text := screenText(m); !strings.Contains(text, "Tab to focus · Enter to join") {
		t.Fatalf("unfocused chat hint:\n%s", text)
	}
	m = clickOn(t, m, chatSectionKey)
	if m.screen != scrBoard || !m.chatFocus {
		t.Fatalf("first click changed screen=%v focus=%t", m.screen, m.chatFocus)
	}
	m = clickOn(t, m, chatSectionKey)
	if text := screenText(m); m.screen != scrChat || !strings.Contains(text, "public to every arcade player") {
		t.Fatalf("second click or first input cue:\n%s", text)
	}
}

func TestChatSendAppearsAtBottom(t *testing.T) {
	f := newChatFixture(t)
	for i := range 40 {
		f.say(t, f.bob, fmt.Sprint("backlog ", i))
	}
	m := f.openChatModel(t, f.alice, 100, 30)
	if m.chat.offset != 0 {
		t.Fatalf("chat opened scrolled up by %d", m.chat.offset)
	}
	text := screenText(m)
	if !strings.Contains(text, "backlog 39") || strings.Contains(text, "backlog 0\n") {
		t.Fatalf("chat should open at the newest message:\n%s", text)
	}
	*f.now = f.now.Add(time.Minute)
	m = drive(t, m, append(typeText("hello there"), key("enter"))...)
	if m.chat.sending || m.chat.err != "" || m.chat.input.Value() != "" {
		t.Fatalf("after send: sending=%v err=%q input=%q", m.chat.sending, m.chat.err, m.chat.input.Value())
	}
	last := m.chat.msgs[len(m.chat.msgs)-1]
	if last.Body != "hello there" || last.AuthorID != f.alice.ID {
		t.Fatalf("last message = %+v", last)
	}
	rows := strings.Split(screenText(m), "\n")
	// The newest line sits just above the rule and input, at the bottom.
	if !strings.Contains(rows[len(rows)-5], "hello there") {
		t.Fatalf("newest message is not at the bottom:\n%s", strings.Join(rows, "\n"))
	}
	// q and / are text here, not navigation.
	m = drive(t, m, typeText("q/")...)
	if m.screen != scrChat || m.overlay != ovNone || m.chat.input.Value() != "q/" {
		t.Fatalf("typing q/: screen=%v overlay=%v input=%q", m.screen, m.overlay, m.chat.input.Value())
	}
}

func TestChatKeepsReaderPlaceWhenScrolledUp(t *testing.T) {
	f := newChatFixture(t)
	for i := range 60 {
		f.say(t, f.bob, fmt.Sprint("line ", i))
	}
	m := f.openChatModel(t, f.alice, 100, 30)
	m = drive(t, m, key("pgup"))
	if m.chat.offset == 0 {
		t.Fatal("pgup did not scroll")
	}
	before := screenText(m)
	beforeOffset := m.chat.offset

	msg := f.say(t, f.bob, "breaking news")
	m = drive(t, m, eventMsgFor(board.Event{Kind: board.EventChat, BoardID: f.board.ID, MessageID: msg.ID, Actor: "Bob"}))
	if m.chat.unread != 1 || m.chat.offset <= beforeOffset {
		t.Fatalf("unread=%d offset=%d (was %d)", m.chat.unread, m.chat.offset, beforeOffset)
	}
	after := screenText(m)
	if strings.Contains(after, "breaking news") || !strings.Contains(after, "1 new message ↓") {
		t.Fatalf("scrolled-up reader was moved or not told:\n%s", after)
	}
	// The message rows the reader was looking at did not move.
	if b, a := strings.Split(before, "\n")[5], strings.Split(after, "\n")[5]; b != a {
		t.Fatalf("view shifted:\n%q\n%q", b, a)
	}
	// esc jumps to the latest; a second esc leaves to the board.
	m = drive(t, m, key("esc"))
	if m.screen != scrChat || m.chat.offset != 0 || m.chat.unread != 0 || !strings.Contains(screenText(m), "breaking news") {
		t.Fatalf("esc while scrolled: screen=%v offset=%d", m.screen, m.chat.offset)
	}
	m = drive(t, m, key("esc"))
	if m.screen != scrBoard || !m.chatFocus {
		t.Fatalf("second esc: screen=%v chatFocus=%v", m.screen, m.chatFocus)
	}
}

func TestChatLoadsOlderHistoryAtTop(t *testing.T) {
	f := newChatFixture(t)
	for i := range board.ChatPage + 20 {
		f.say(t, f.bob, fmt.Sprint("old ", i))
	}
	m := f.openChatModel(t, f.alice, 100, 30)
	if len(m.chat.msgs) != board.ChatPage || !m.chat.more {
		t.Fatalf("first page: %d messages, more=%v", len(m.chat.msgs), m.chat.more)
	}
	for range 60 {
		m = drive(t, m, key("pgup"))
	}
	if len(m.chat.msgs) != board.ChatPage+20 || m.chat.more || m.chat.msgs[0].Body != "old 0" {
		t.Fatalf("after scrolling to the top: %d messages, more=%v", len(m.chat.msgs), m.chat.more)
	}
	if !strings.Contains(screenText(m), "This is the start of the conversation") {
		t.Fatalf("top of history has no banner:\n%s", screenText(m))
	}
}

func TestChatLiveRemovalAndOwnDelete(t *testing.T) {
	f := newChatFixture(t)
	f.say(t, f.alice, "alice says hi")
	rude := f.say(t, f.bob, "something rude")
	m := f.openChatModel(t, f.alice, 100, 30)

	// Staff remove Bob's message elsewhere; it vanishes live.
	if err := f.svc.RemoveChat(context.Background(), f.owner.ID, rude.ID); err != nil {
		t.Fatal(err)
	}
	m = drive(t, m, eventMsgFor(board.Event{Kind: board.EventChatRemoved, BoardID: f.board.ID, MessageID: rude.ID}))
	if strings.Contains(screenText(m), "something rude") || len(m.chat.msgs) != 1 {
		t.Fatalf("removed message still shown:\n%s", screenText(m))
	}

	// Alice deletes her own message through the options menu.
	m = drive(t, m, key("tab"))
	if m.chat.sel != 0 {
		t.Fatalf("tab selected %d", m.chat.sel)
	}
	m = drive(t, m, key("o"))
	opts := ansi.Strip(m.viewOptions())
	if m.overlay != ovOptions || !strings.Contains(opts, "Delete message") || strings.Contains(opts, "Remove message") {
		t.Fatalf("own-message options:\n%s", opts)
	}
	m = drive(t, m, key("down"), key("enter"), key("y"))
	if len(m.chat.msgs) != 0 || m.overlay != ovNone || m.chat.sel != -1 {
		t.Fatalf("after delete: msgs=%d overlay=%v sel=%d", len(m.chat.msgs), m.overlay, m.chat.sel)
	}
	if !strings.Contains(screenText(m), "No one has said anything yet") {
		t.Fatalf("empty chat has no welcome:\n%s", screenText(m))
	}
}

func TestStaffRemoveChatFromOptions(t *testing.T) {
	f := newChatFixture(t)
	f.say(t, f.bob, "spam spam spam")
	m := f.openChatModel(t, f.owner, 100, 30)
	m = drive(t, m, key("tab"), key("o"))
	opts := ansi.Strip(m.viewOptions())
	if !strings.Contains(opts, "Remove message") || strings.Contains(opts, "Delete message") {
		t.Fatalf("staff options on another's message:\n%s", opts)
	}
	m = drive(t, m, key("down"), key("enter"))
	if !strings.Contains(ansi.Strip(m.viewOptions()), "Remove this message from the chat?") {
		t.Fatalf("no removal confirmation:\n%s", ansi.Strip(m.viewOptions()))
	}
	m = drive(t, m, key("y"))
	if len(m.chat.msgs) != 0 {
		t.Fatalf("message not removed: %+v", m.chat.msgs)
	}
	if msgs, _, _ := f.svc.ChatHistory(context.Background(), f.board.ID, 0, 10); len(msgs) != 0 {
		t.Fatalf("removal not stored: %+v", msgs)
	}

	// Members get no remove option on someone else's message.
	f.say(t, f.bob, "an ordinary message")
	m = drive(t, f.openChatModel(t, f.alice, 100, 30), key("tab"), key("o"))
	if opts := ansi.Strip(m.viewOptions()); strings.Contains(opts, "Remove message") || strings.Contains(opts, "Delete message") {
		t.Fatalf("member options on another's message:\n%s", opts)
	}
}

func TestPaletteOpensBoardChat(t *testing.T) {
	f := newChatFixture(t)
	boards, _ := f.svc.Boards(context.Background())
	m := drive(t, modelFor(f.svc, f.alice, 100, 30), tea.WindowSizeMsg{Width: 100, Height: 30}, key("/"))
	m = drive(t, m, append(typeText("workshop chat"), key("enter"))...)
	if m.screen != scrChat || m.curBoard.Slug != "workshop" || m.chat.boardID != m.curBoard.ID {
		t.Fatalf("screen=%v board=%q chat board=%d", m.screen, m.curBoard.Slug, m.chat.boardID)
	}
	// Leaving lands on that board with the chat section focused.
	m = drive(t, m, key("esc"))
	if m.screen != scrBoard || m.curBoard.ID != boards[2].ID || !m.chatFocus {
		t.Fatalf("after esc: screen=%v board=%q chatFocus=%v", m.screen, m.curBoard.Slug, m.chatFocus)
	}
}

func TestChatQuotaErrorKeepsDraft(t *testing.T) {
	f := newChatFixture(t)
	m := f.openChatModel(t, f.alice, 100, 30)
	for i := range 5 {
		m = drive(t, m, append(typeText(fmt.Sprint("m", i)), key("enter"))...)
	}
	m = drive(t, m, append(typeText("too fast"), key("enter"))...)
	if m.chat.input.Value() != "too fast" || !strings.Contains(m.chat.err, "too fast") {
		t.Fatalf("input=%q err=%q", m.chat.input.Value(), m.chat.err)
	}
	if !strings.Contains(screenText(m), "sending messages too fast") {
		t.Fatalf("quota error not shown:\n%s", screenText(m))
	}
}

// The chat screen fills the terminal exactly, scrolled or not.
func TestChatFrameIsExactlyTheTerminal(t *testing.T) {
	f := newChatFixture(t)
	for i := range 30 {
		f.say(t, f.bob, strings.Repeat(fmt.Sprint("word", i, " "), i%9+1))
	}
	for _, sz := range [][2]int{{64, 20}, {80, 24}, {160, 50}} {
		for _, keys := range [][]string{nil, {"pgup"}, {"tab"}, {"tab", "o"}} {
			m := f.openChatModel(t, f.alice, sz[0], sz[1])
			for _, k := range keys {
				m = drive(t, m, key(k))
			}
			out := m.View().Content
			if lipgloss.Height(out) != sz[1] {
				t.Errorf("%v %v: height %d", sz, keys, lipgloss.Height(out))
			}
			for i, l := range strings.Split(out, "\n") {
				if lipgloss.Width(l) != sz[0] {
					t.Errorf("%v %v: row %d width %d", sz, keys, i, lipgloss.Width(l))
					break
				}
			}
		}
	}
}
