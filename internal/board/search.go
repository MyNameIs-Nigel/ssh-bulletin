package board

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

type SearchResult = store.SearchResult

const (
	MaxSearchLen   = 120
	SearchPageSize = 20
)

func (s *Service) SearchThreads(ctx context.Context, viewer int64, query string, page int) ([]SearchResult, bool, error) {
	query = CleanLine(query)
	if utf8.RuneCountInString(query) > MaxSearchLen {
		return nil, false, ErrTooLong
	}
	words := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r) })
	if len(words) == 0 {
		return nil, false, nil
	}
	for i, word := range words {
		words[i] = `"` + word + `"`
	}
	results, err := s.st.SearchThreads(ctx, viewer, strings.Join(words, " AND "), max(0, page)*SearchPageSize, SearchPageSize+1)
	return results[:min(len(results), SearchPageSize)], len(results) > SearchPageSize, err
}
