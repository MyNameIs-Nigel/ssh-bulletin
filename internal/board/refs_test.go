package board

import (
	"context"
	"testing"

	"github.com/mynameis-nigel/ssh-bulletin/internal/markup"
)

func TestResolveRefs(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	alice, err := svc.Register(ctx, "SHA256:alice", "", "Alice", "reef")
	if err != nil {
		t.Fatal(err)
	}
	boards, _ := svc.Boards(ctx)
	tid, err := svc.CreateThread(ctx, alice, boards[1].ID, "Belt tactics", "opening")
	if err != nil {
		t.Fatal(err)
	}
	th, _ := svc.Thread(ctx, tid)
	pid, err := svc.Reply(ctx, alice, th, "second")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := svc.ResolveText(ctx, "hi @ALICE and @ghost, in #"+boards[1].Slug+" and #nowhere: "+
		markup.Ref{Kind: markup.Thread, Thread: tid}.Key()+" "+markup.Ref{Kind: markup.Post, Thread: tid, Post: 2}.Key()+
		" "+markup.Ref{Kind: markup.Post, Thread: tid, Post: 9}.Key()+" #99999 https://example.com/x")
	if err != nil {
		t.Fatal(err)
	}
	get := func(r markup.Ref) Target {
		t.Helper()
		tg, ok := refs[r]
		if !ok {
			t.Fatalf("%s was not looked up", r.Key())
		}
		return tg
	}
	if u := get(markup.Ref{Kind: markup.User, Name: "alice"}); !u.OK || u.AccountID != alice.ID || u.Handle != "Alice" {
		t.Errorf("user: %+v", u)
	}
	if b := get(markup.Ref{Kind: markup.Board, Name: boards[1].Slug}); !b.OK || b.BoardID != boards[1].ID || b.Palette != boards[1].Palette {
		t.Errorf("board: %+v", b)
	}
	if th := get(markup.Ref{Kind: markup.Thread, Thread: tid}); !th.OK || th.Title != "Belt tactics" || th.BoardID != boards[1].ID || th.Palette != boards[1].Palette {
		t.Errorf("thread: %+v", th)
	}
	if p := get(markup.Ref{Kind: markup.Post, Thread: tid, Post: 2}); !p.OK || p.PostID != pid {
		t.Errorf("post: %+v (want post %d)", p, pid)
	}
	for _, missing := range []markup.Ref{{Kind: markup.User, Name: "ghost"}, {Kind: markup.Board, Name: "nowhere"},
		{Kind: markup.Post, Thread: tid, Post: 9}, {Kind: markup.Thread, Thread: 99999}} {
		if get(missing).OK {
			t.Errorf("%s resolved but does not exist", missing.Key())
		}
	}
	if u := get(markup.Ref{Kind: markup.URL, URL: "https://example.com/x"}); !u.OK {
		t.Error("URLs are always links")
	}
}

// One load cannot make an unbounded number of lookups.
func TestResolveRefsIsBounded(t *testing.T) {
	svc := newSvc(t)
	var refs []markup.Ref
	for i := range MaxRefsPerLoad + 10 {
		refs = append(refs, markup.Ref{Kind: markup.Thread, Thread: int64(i + 1)})
	}
	out, err := svc.ResolveRefs(context.Background(), refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != MaxRefsPerLoad {
		t.Fatalf("looked up %d references, cap is %d", len(out), MaxRefsPerLoad)
	}
}
