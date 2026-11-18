package board

import (
	"context"
	"errors"
	"strings"

	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

// PostFlairs lists the optional labels available to all posters. The first
// option leaves new posts unlabelled.
func PostFlairs() []string {
	return []string{"", "Arcade", "Builder", "Tinker", "Retro", "Lurker"}
}

const DirectoryLimit = 30

var (
	ErrFlairInvalid = errors.New("choose a flair from the list")
	ErrSelfFriend   = errors.New("you can't add yourself as a friend")
)

type ProfileEntry = store.ProfileEntry
type ProfilePost = store.ProfilePost

type Profile struct {
	Account
	Recent []ProfilePost
}

// SearchProfiles accepts a username or fragment, with an optional @ prefix.
func (s *Service) SearchProfiles(ctx context.Context, viewer int64, query string, friendsOnly bool) ([]ProfileEntry, bool, error) {
	query = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(query), "@"))
	entries, err := s.st.SearchProfiles(ctx, query, viewer, friendsOnly, DirectoryLimit+1)
	if len(entries) > DirectoryLimit {
		return entries[:DirectoryLimit], true, err
	}
	return entries, false, err
}

func (s *Service) Profile(ctx context.Context, id int64) (Profile, error) {
	a, err := s.st.AccountByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, err
	}
	recent, err := s.st.RecentProfilePosts(ctx, id)
	return Profile{Account: a, Recent: recent}, err
}

func (s *Service) FriendIDs(ctx context.Context, viewer int64) (map[int64]bool, error) {
	return s.st.FriendIDs(ctx, viewer)
}

func (s *Service) SetFriend(ctx context.Context, viewer, target int64, on bool) error {
	if viewer == 0 {
		return ErrNoAccount
	}
	if viewer == target {
		return ErrSelfFriend
	}
	return s.st.SetFriend(ctx, viewer, target, on)
}
