package board

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

// ChatMessage is one line in a board's live chat.
type ChatMessage = store.ChatMessage

const (
	// MaxChatLen caps one chat message. Chat is single-line and short:
	// anything longer belongs in a thread.
	MaxChatLen = 500
	// ChatPage is how many messages one history fetch returns.
	ChatPage = 100
	// MaxChatFetch bounds one catch-up or reload fetch.
	MaxChatFetch = 500
)

// ErrChatLimit is returned when a key sends chat messages too quickly.
var ErrChatLimit = store.ErrChatLimit

// SendChat validates and appends a message to a board's live chat.
func (s *Service) SendChat(ctx context.Context, author Account, boardID int64, body string) (ChatMessage, error) {
	if author.ID == 0 {
		return ChatMessage{}, ErrNoAccount
	}
	body = CleanLine(body)
	switch {
	case body == "":
		return ChatMessage{}, ErrEmptyBody
	case utf8.RuneCountInString(body) > MaxChatLen:
		return ChatMessage{}, ErrTooLong
	}
	msg, err := s.st.PostChat(ctx, boardID, author.ID, body, s.now().Unix())
	if errors.Is(err, store.ErrNotFound) {
		return ChatMessage{}, ErrNotFound
	}
	if err != nil {
		return ChatMessage{}, err
	}
	s.broker.Publish(Event{Kind: EventChat, AccountID: author.ID, BoardID: boardID, MessageID: msg.ID, Actor: author.Handle})
	return msg, nil
}

// ChatHistory returns up to limit visible messages older than beforeID (0
// for the newest), oldest first. more reports whether older ones exist.
func (s *Service) ChatHistory(ctx context.Context, boardID, beforeID int64, limit int) (msgs []ChatMessage, more bool, err error) {
	return s.ChatHistoryFor(ctx, 0, boardID, beforeID, limit)
}

func (s *Service) ChatHistoryFor(ctx context.Context, viewer, boardID, beforeID int64, limit int) (msgs []ChatMessage, more bool, err error) {
	limit = max(1, min(limit, MaxChatFetch))
	msgs, err = s.st.ChatBefore(ctx, boardID, beforeID, viewer, limit+1)
	if len(msgs) > limit {
		return msgs[1:], true, err
	}
	return msgs, false, err
}

// ChatSince returns the visible messages newer than afterID, oldest first.
// A session that missed broker events catches up with it.
func (s *Service) ChatSince(ctx context.Context, boardID, afterID int64) ([]ChatMessage, error) {
	return s.ChatSinceFor(ctx, 0, boardID, afterID)
}

func (s *Service) ChatSinceFor(ctx context.Context, viewer, boardID, afterID int64) ([]ChatMessage, error) {
	return s.st.ChatAfter(ctx, boardID, afterID, viewer, MaxChatFetch)
}

// DeleteChat withdraws the actor's own chat message.
func (s *Service) DeleteChat(ctx context.Context, actorID, id int64) error {
	if actorID == 0 {
		return ErrNoAccount
	}
	boardID, err := s.st.DeleteChat(ctx, actorID, id, s.now().Unix())
	return s.chatRemoved(boardID, id, err)
}

// RemoveChat hides any chat message; staff only.
func (s *Service) RemoveChat(ctx context.Context, actorID, id int64) error {
	if actorID == 0 {
		return ErrNoAccount
	}
	boardID, err := s.st.RemoveChat(ctx, actorID, id, s.now().Unix())
	return s.chatRemoved(boardID, id, err)
}

func (s *Service) chatRemoved(boardID, id int64, err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err == nil {
		s.broker.Publish(Event{Kind: EventChatRemoved, BoardID: boardID, MessageID: id})
	}
	return err
}
