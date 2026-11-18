package board

import (
	"context"
	"errors"

	"github.com/mynameis-nigel/ssh-bulletin/internal/markup"
	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

// Target is what a reference in a post or chat message points at, as far as
// the reader needs to draw and follow it. A reference to something that does
// not exist resolves with OK false and is shown as plain text.
type Target struct {
	OK      bool
	Ref     markup.Ref
	Palette string // the user's, or the board's (for boards, threads and posts)

	AccountID int64 // users
	Handle    string

	BoardID  int64 // boards, threads and posts
	Slug     string
	Title    string // board title, or thread title for threads and posts
	ThreadID int64  // threads and posts
	PostID   int64  // posts
}

// Refs maps references to their targets.
type Refs map[markup.Ref]Target

// MaxRefsPerLoad bounds the lookups one load can ask for, however many
// references the content holds. The rest stay plain text.
const MaxRefsPerLoad = 64

// ResolveRefs looks up every reference in refs. URLs need no lookup: they are
// always external links, and the reader is warned before leaving.
func (s *Service) ResolveRefs(ctx context.Context, refs []markup.Ref) (Refs, error) {
	out := Refs{}
	var handles []string
	var threadIDs []int64
	var boards, posts []markup.Ref
	n := 0
	for _, r := range refs {
		if _, done := out[r]; done {
			continue
		}
		if r.Kind == markup.URL {
			out[r] = Target{OK: true, Ref: r}
			continue
		}
		if n >= MaxRefsPerLoad {
			continue
		}
		n++
		out[r] = Target{Ref: r}
		switch r.Kind {
		case markup.User:
			handles = append(handles, r.Name)
		case markup.Board:
			boards = append(boards, r)
		case markup.Thread:
			threadIDs = append(threadIDs, r.Thread)
		case markup.Post:
			threadIDs = append(threadIDs, r.Thread)
			posts = append(posts, r)
		}
	}
	if len(handles) > 0 {
		profiles, err := s.st.ProfilesByHandle(ctx, handles)
		if err != nil {
			return out, err
		}
		for r := range out {
			if p, ok := profiles[r.Name]; ok && r.Kind == markup.User {
				out[r] = Target{OK: true, Ref: r, AccountID: p.ID, Handle: p.Handle, Palette: p.Palette}
			}
		}
	}
	if len(boards) == 0 && len(threadIDs) == 0 {
		return out, nil
	}
	byID := map[int64]store.BoardRef{}
	slugs, err := s.st.BoardRefs(ctx)
	if err != nil {
		return out, err
	}
	for _, b := range slugs {
		byID[b.ID] = b
	}
	for _, r := range boards {
		if b, ok := slugs[r.Name]; ok {
			out[r] = Target{OK: true, Ref: r, BoardID: b.ID, Slug: b.Slug, Title: b.Title, Palette: b.Palette}
		}
	}
	threads, err := s.st.ThreadsByID(ctx, threadIDs)
	if err != nil {
		return out, err
	}
	for r := range out {
		if r.Kind != markup.Thread && r.Kind != markup.Post {
			continue
		}
		t, ok := threads[r.Thread]
		if !ok {
			continue
		}
		b := byID[t.BoardID]
		tg := Target{OK: true, Ref: r, BoardID: t.BoardID, Slug: b.Slug, Title: t.Title, Palette: b.Palette, ThreadID: t.ID}
		if r.Kind == markup.Post {
			id, err := s.st.PostAt(ctx, t.ID, r.Post)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return out, err
			}
			tg.PostID = id
		}
		out[r] = tg
	}
	return out, nil
}

// ResolveText resolves the references in post bodies (block markup).
func (s *Service) ResolveText(ctx context.Context, bodies ...string) (Refs, error) {
	var refs []markup.Ref
	for _, b := range bodies {
		refs = append(refs, markup.Refs(markup.Parse(b))...)
	}
	return s.ResolveRefs(ctx, refs)
}

// ResolveLines resolves the references in single-line texts (chat, bios),
// which carry inline markup only.
func (s *Service) ResolveLines(ctx context.Context, lines ...string) (Refs, error) {
	var refs []markup.Ref
	for _, l := range lines {
		refs = append(refs, markup.SpanRefs(markup.ParseLine(l))...)
	}
	return s.ResolveRefs(ctx, refs)
}
