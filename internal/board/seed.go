package board

import (
	"context"
	"fmt"

	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

// SeedDemo fills an empty database with a few invented posters and threads
// so the UI has something to render in local development. It does nothing
// if any account already exists. Never enabled in production
// (BULLETIN_DEV_MODE is unset in the fleet compose file).
func SeedDemo(ctx context.Context, svc *Service, st *store.Store) error {
	stats, err := st.Stats(ctx)
	if err != nil || stats.Accounts > 0 {
		return err
	}
	boards, err := st.Boards(ctx)
	if err != nil || len(boards) == 0 {
		return err
	}
	people := []struct{ handle, palette string }{
		{"pixelwitch", "neon"}, {"ferrofluid", "glacier"}, {"oldmoon", "ember"},
		{"tidepool", "reef"}, {"quietfox", "orchid"},
	}
	accts := make([]Account, len(people))
	for i, p := range people {
		a, err := svc.Register(ctx, fmt.Sprintf("SHA256:demo-%d", i), "", p.handle, p.palette)
		if err != nil {
			return err
		}
		accts[i] = a
	}
	threads := []struct {
		board       int
		author      int
		title, body string
		replies     []string
	}{
		{0, 0, "hello from a 24-bit terminal",
			"First post on the new board. Every color you see here is your own palette —\nhit P from anywhere to pick yours.",
			[]string{"Gradient borders over SSH. What a time.", "The handle shimmer is a nice touch."}},
		{1, 2, "Moon Miner: is the pirate belt worth it?",
			"Tried a run through the belt with a half-full hold. Lost everything twice.\nIs there a trick or is it pure push-your-luck?",
			[]string{"Pure push-your-luck. Bank early, bank often.", "Pirates spawn less if you drill the edges first."}},
		{2, 3, "Show your terminal setup",
			"Font, colors, multiplexer. Mine: Iosevka, a dark ink-blue scheme, tmux with zero chrome.",
			[]string{"WezTerm + JetBrains Mono. Ligatures off, forever."}},
		{3, 4, "Idea: a co-op garden between farm saves", "What if two keys could share a plot of land in Idle Farmer?", nil},
		{4, 1, "Favorite sound a computer makes?", "Mine is a mechanical hard drive seeking. Rest in peace.", []string{"Dial-up handshake, obviously."}},
	}
	for _, t := range threads {
		tid, err := svc.CreateThread(ctx, accts[t.author], boards[t.board].ID, t.title, t.body)
		if err != nil {
			return err
		}
		th, err := svc.Thread(ctx, tid)
		if err != nil {
			return err
		}
		for i, r := range t.replies {
			if _, err := svc.Reply(ctx, accts[(t.author+i+1)%len(accts)], th, r); err != nil {
				return err
			}
		}
	}
	return nil
}
