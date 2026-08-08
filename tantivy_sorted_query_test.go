package tantivy_go_test

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	tantivy_go "github.com/oskoi/tantivy-go"
	"github.com/stretchr/testify/require"
)

func TestSearchSortedRejectsInvalidRequests(t *testing.T) {
	validSort := []tantivy_go.SortField{{
		Name:      sortedSearchU64Field,
		Direction: tantivy_go.SortAscending,
	}}

	testCases := []struct {
		name    string
		request tantivy_go.SortedQueryRequest
		want    string
	}{
		{
			name: "empty query",
			request: tantivy_go.SortedQueryRequest{
				Limit:   1,
				Sort:    validSort,
				Timeout: time.Second,
			},
			want: "must not be empty",
		},
		{
			name: "query contains NUL",
			request: tantivy_go.SortedQueryRequest{
				Query:   "textv:alpha\x00",
				Limit:   1,
				Sort:    validSort,
				Timeout: time.Second,
			},
			want: "contains a NUL byte",
		},
		{
			name: "zero limit",
			request: tantivy_go.SortedQueryRequest{
				Query:   "*",
				Limit:   0,
				Sort:    validSort,
				Timeout: time.Second,
			},
			want: "docsLimit must be greater than 0",
		},
		{
			name: "negative limit",
			request: tantivy_go.SortedQueryRequest{
				Query:   "*",
				Limit:   -1,
				Sort:    validSort,
				Timeout: time.Second,
			},
			want: "docsLimit must be greater than 0",
		},
		{
			name: "limit above maximum",
			request: tantivy_go.SortedQueryRequest{
				Query:   "*",
				Limit:   10_001,
				Sort:    validSort,
				Timeout: time.Second,
			},
			want: "must not exceed 10000",
		},
		{
			name: "zero timeout",
			request: tantivy_go.SortedQueryRequest{
				Query: "*",
				Limit: 1,
				Sort:  validSort,
			},
			want: "timeout must be greater than zero",
		},
		{
			name: "negative timeout",
			request: tantivy_go.SortedQueryRequest{
				Query:   "*",
				Limit:   1,
				Sort:    validSort,
				Timeout: -time.Second,
			},
			want: "timeout must be greater than zero",
		},
		{
			name: "no sort fields",
			request: tantivy_go.SortedQueryRequest{
				Query:   "*",
				Limit:   1,
				Timeout: time.Second,
			},
			want: "requires between one and four sort fields",
		},
		{
			name: "too many sort fields",
			request: tantivy_go.SortedQueryRequest{
				Query: "*",
				Limit: 1,
				Sort: []tantivy_go.SortField{
					{Name: sortedSearchU64Field, Direction: tantivy_go.SortAscending},
					{Name: sortedSearchI64Field, Direction: tantivy_go.SortAscending},
					{Name: sortedSearchF64Field, Direction: tantivy_go.SortAscending},
					{Name: sortedSearchDateField, Direction: tantivy_go.SortAscending},
					{Name: sortedSearchDocIDField, Direction: tantivy_go.SortAscending},
				},
				Timeout: time.Second,
			},
			want: "requires between one and four sort fields",
		},
		{
			name: "empty sort field name",
			request: tantivy_go.SortedQueryRequest{
				Query: "*",
				Limit: 1,
				Sort: []tantivy_go.SortField{{
					Direction: tantivy_go.SortAscending,
				}},
				Timeout: time.Second,
			},
			want: "has an empty name",
		},
		{
			name: "sort field name contains NUL",
			request: tantivy_go.SortedQueryRequest{
				Query: "*",
				Limit: 1,
				Sort: []tantivy_go.SortField{{
					Name:      "u64v\x00",
					Direction: tantivy_go.SortAscending,
				}},
				Timeout: time.Second,
			},
			want: "contains a NUL byte",
		},
		{
			name: "invalid sort direction",
			request: tantivy_go.SortedQueryRequest{
				Query: "*",
				Limit: 1,
				Sort: []tantivy_go.SortField{{
					Name:      sortedSearchU64Field,
					Direction: tantivy_go.SortDirection(99),
				}},
				Timeout: time.Second,
			},
			want: "has an invalid direction",
		},
		{
			name: "search-after arity mismatch",
			request: tantivy_go.SortedQueryRequest{
				Query: "*",
				Limit: 1,
				Sort: []tantivy_go.SortField{
					{Name: sortedSearchU64Field, Direction: tantivy_go.SortAscending},
					{Name: sortedSearchDocIDField, Direction: tantivy_go.SortAscending},
				},
				After:   []tantivy_go.SortValue{{Kind: tantivy_go.SortValueU64, U64: 1}},
				Timeout: time.Second,
			},
			want: "after tuple length must match sort fields",
		},
		{
			name: "zero search-after kind",
			request: tantivy_go.SortedQueryRequest{
				Query:   "*",
				Limit:   1,
				Sort:    validSort,
				After:   []tantivy_go.SortValue{{}},
				Timeout: time.Second,
			},
			want: "invalid kind",
		},
		{
			name: "search-after has multiple union values",
			request: tantivy_go.SortedQueryRequest{
				Query: "*",
				Limit: 1,
				Sort:  validSort,
				After: []tantivy_go.SortValue{{
					Kind: tantivy_go.SortValueU64,
					U64:  1,
					I64:  1,
				}},
				Timeout: time.Second,
			},
			want: "multiple union values",
		},
		{
			name: "missing search-after has a union value",
			request: tantivy_go.SortedQueryRequest{
				Query: "*",
				Limit: 1,
				Sort:  validSort,
				After: []tantivy_go.SortValue{{
					Kind:    tantivy_go.SortValueU64,
					Missing: true,
					U64:     1,
				}},
				Timeout: time.Second,
			},
			want: "is missing and also has a union value",
		},
		{
			name: "NaN search-after value",
			request: tantivy_go.SortedQueryRequest{
				Query: "*",
				Limit: 1,
				Sort: []tantivy_go.SortField{{
					Name:      sortedSearchF64Field,
					Direction: tantivy_go.SortAscending,
				}},
				After:   []tantivy_go.SortValue{{Kind: tantivy_go.SortValueF64, F64: math.NaN()}},
				Timeout: time.Second,
			},
			want: "cannot be NaN",
		},
	}

	var tc *tantivy_go.TantivyContext
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			result, err := tc.SearchSorted(testCase.request)
			if result != nil {
				result.Free()
			}
			require.Nil(t, result)
			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.want)
			require.False(t, errors.Is(err, tantivy_go.ErrClosedContext))
		})
	}
}

func TestSearchSortedLeavesWhitespaceForNativeParser(t *testing.T) {
	var tc *tantivy_go.TantivyContext

	result, err := tc.SearchSorted(sortedQueryRequest(" \t "))
	if result != nil {
		result.Free()
	}
	require.Nil(t, result)
	require.ErrorIs(t, err, tantivy_go.ErrClosedContext)
}

func TestSearchSortedTerms(t *testing.T) {
	tc := newSortedQueryContext(t)

	testCases := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "match all",
			query: "*",
			want:  []string{"amber", "birch", "cedar", "delta", "ember"},
		},
		{
			name:  "exact keyword",
			query: "textv:alpha",
			want:  []string{"amber"},
		},
		{
			name:  "u64",
			query: "u64v:40",
			want:  []string{"ember"},
		},
		{
			name:  "i64",
			query: "i64v:-5",
			want:  []string{"birch"},
		},
		{
			name:  "f64",
			query: "f64v:3.5",
			want:  []string{"cedar"},
		},
		{
			name:  "Boolean",
			query: "payload.active:true",
			want:  []string{"birch", "cedar"},
		},
		{
			name:  "date",
			query: "datev:\"2024-01-03T00:00:00Z\"",
			want:  []string{"cedar"},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			requireSortedQueryIDs(t, tc, testCase.query, testCase.want)
		})
	}
}

func TestSearchSortedRanges(t *testing.T) {
	tc := newSortedQueryContext(t)

	testCases := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "greater than or equal",
			query: "u64v:[20 TO *]",
			want:  []string{"birch", "cedar", "delta", "ember"},
		},
		{
			name:  "greater than",
			query: "u64v:{20 TO *]",
			want:  []string{"delta", "ember"},
		},
		{
			name:  "less than or equal",
			query: "u64v:[* TO 20]",
			want:  []string{"amber", "birch", "cedar"},
		},
		{
			name:  "less than",
			query: "u64v:[* TO 20}",
			want:  []string{"amber"},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			requireSortedQueryIDs(t, tc, testCase.query, testCase.want)
		})
	}
}

func TestSearchSortedComposition(t *testing.T) {
	tc := newSortedQueryContext(t)

	testCases := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "set membership",
			query: "textv:IN [alpha gamma]",
			want:  []string{"amber", "ember"},
		},
		{
			name:  "direct keyword regex",
			query: "textv:/alpha.*/",
			want:  []string{"amber", "cedar"},
		},
		{
			name:  "nested Boolean composition",
			query: "(textv:alpha OR textv:beta) AND payload.active:true",
			want:  []string{"birch"},
		},
		{
			name:  "explicit match-all all-negative form",
			query: "(+* -textv:blocked)",
			want:  []string{"amber", "birch", "cedar", "ember"},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			requireSortedQueryIDs(t, tc, testCase.query, testCase.want)
		})
	}
}

func TestSearchSortedRejectsParserErrors(t *testing.T) {
	tc := newSortedQueryContext(t)

	testCases := []struct {
		name  string
		query string
	}{
		{name: "invalid syntax", query: "textv:("},
		{name: "unqualified term has no default field", query: "alpha"},
		{name: "unknown field", query: "missing:alpha"},
		{name: "incompatible u64 value", query: "u64v:not-a-number"},
		{name: "incompatible i64 value", query: "i64v:not-an-integer"},
		{name: "incompatible f64 value", query: "f64v:not-a-float"},
		{name: "incompatible date value", query: "datev:not-a-date"},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			result, err := tc.SearchSorted(sortedQueryRequest(testCase.query))
			if result != nil {
				result.Free()
			}
			require.Nil(t, result)
			require.Error(t, err)
		})
	}
}

func TestSearchSortedAfterTraversesTiesOnce(t *testing.T) {
	tc := newSortedQueryContext(t)

	wantIDs := []string{"amber", "birch", "cedar", "delta", "ember"}
	wantCursors := [][]tantivy_go.SortValue{
		{
			{Kind: tantivy_go.SortValueU64, U64: 20},
			{Kind: tantivy_go.SortValueText, Text: "birch"},
		},
		{
			{Kind: tantivy_go.SortValueU64, U64: 30},
			{Kind: tantivy_go.SortValueText, Text: "delta"},
		},
		{
			{Kind: tantivy_go.SortValueU64, U64: 40},
			{Kind: tantivy_go.SortValueText, Text: "ember"},
		},
	}

	var after []tantivy_go.SortValue
	gotIDs := make([]string, 0, len(wantIDs))
	seenIDs := make(map[string]struct{}, len(wantIDs))

	for page := 0; ; page++ {
		require.Less(t, page, len(wantCursors), "search-after did not exhaust")

		request := sortedQueryRequest("*")
		request.Limit = 2
		request.Sort = []tantivy_go.SortField{
			{Name: sortedSearchU64Field, Direction: tantivy_go.SortAscending},
			{Name: sortedSearchDocIDField, Direction: tantivy_go.SortAscending},
		}
		request.After = after
		result, err := tc.SearchSorted(request)
		require.NoError(t, err)

		pageIDs := sortedSearchResultIDs(t, tc, result)
		cursor, err := result.SortValues(uint64(len(pageIDs) - 1))
		require.NoError(t, err)
		require.Equal(t, wantCursors[page], cursor)
		hasMore, err := result.HasMore()
		require.NoError(t, err)
		result.Free()

		for _, id := range pageIDs {
			_, duplicate := seenIDs[id]
			require.Falsef(t, duplicate, "duplicate result %q", id)
			seenIDs[id] = struct{}{}
			gotIDs = append(gotIDs, id)
		}

		if page == len(wantCursors)-1 {
			require.False(t, hasMore)
			break
		}
		require.True(t, hasMore)
		after = append([]tantivy_go.SortValue(nil), cursor...)
	}

	require.Equal(t, wantIDs, gotIDs)
	require.Len(t, seenIDs, len(wantIDs))
}

func TestSearchSortedDeadline(t *testing.T) {
	tc := newSortedQueryContext(t)
	request := sortedQueryRequest("*")
	request.Timeout = time.Nanosecond

	result, err := tc.SearchSorted(request)
	if result != nil {
		result.Free()
	}
	require.Nil(t, result)
	require.ErrorIs(t, err, tantivy_go.ErrSearchTimeout)
}

func TestSearchSortedSnapshotSupportsConcurrentReads(t *testing.T) {
	tc := newSortedQueryContext(t)
	require.NoError(t, tc.ReloadReader())

	const readers = 32
	errors := make(chan error, readers)
	var workers sync.WaitGroup
	workers.Add(readers)
	for range readers {
		go func() {
			defer workers.Done()
			result, err := tc.SearchSortedSnapshot(sortedQueryRequest("textv:alpha"))
			if err != nil {
				errors <- err
				return
			}
			defer result.Free()
			size, err := result.GetSize()
			if err != nil {
				errors <- err
				return
			}
			if size != 1 {
				errors <- fmt.Errorf("reader snapshot returned %d documents, want 1", size)
			}
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
}

func newSortedQueryContext(t *testing.T) *tantivy_go.TantivyContext {
	t.Helper()

	return newSortedSearchContext(t,
		sortedSearchFixtureDoc{
			ID:   "amber",
			Text: sortedSearchPtr("alpha"),
			U64:  sortedSearchPtr(uint64(10)),
			I64:  sortedSearchPtr(int64(-10)),
			F64:  sortedSearchPtr(1.5),
			Bool: sortedSearchPtr(false),
			Date: sortedSearchPtr(int64(1_704_067_200_000)),
		},
		sortedSearchFixtureDoc{
			ID:   "cedar",
			Text: sortedSearchPtr("alphabet"),
			U64:  sortedSearchPtr(uint64(20)),
			I64:  sortedSearchPtr(int64(0)),
			F64:  sortedSearchPtr(3.5),
			Bool: sortedSearchPtr(true),
			Date: sortedSearchPtr(int64(1_704_240_000_000)),
		},
		sortedSearchFixtureDoc{
			ID:   "birch",
			Text: sortedSearchPtr("beta"),
			U64:  sortedSearchPtr(uint64(20)),
			I64:  sortedSearchPtr(int64(-5)),
			F64:  sortedSearchPtr(2.5),
			Bool: sortedSearchPtr(true),
			Date: sortedSearchPtr(int64(1_704_153_600_000)),
		},
		sortedSearchFixtureDoc{
			ID:   "delta",
			Text: sortedSearchPtr("blocked"),
			U64:  sortedSearchPtr(uint64(30)),
			I64:  sortedSearchPtr(int64(5)),
			F64:  sortedSearchPtr(4.5),
			Bool: sortedSearchPtr(false),
			Date: sortedSearchPtr(int64(1_704_326_400_000)),
		},
		sortedSearchFixtureDoc{
			ID:   "ember",
			Text: sortedSearchPtr("gamma"),
			U64:  sortedSearchPtr(uint64(40)),
			I64:  sortedSearchPtr(int64(10)),
			F64:  sortedSearchPtr(5.5),
			Bool: sortedSearchPtr(false),
			Date: sortedSearchPtr(int64(1_704_412_800_000)),
		},
	)
}

func sortedQueryRequest(query string) tantivy_go.SortedQueryRequest {
	return tantivy_go.SortedQueryRequest{
		Query: query,
		Limit: 10,
		Sort: []tantivy_go.SortField{{
			Name:      sortedSearchDocIDField,
			Direction: tantivy_go.SortAscending,
		}},
		Timeout: time.Second,
	}
}

func requireSortedQueryIDs(t *testing.T, tc *tantivy_go.TantivyContext, query string, want []string) {
	t.Helper()

	result, err := tc.SearchSorted(sortedQueryRequest(query))
	require.NoError(t, err)
	defer result.Free()

	require.Equal(t, want, sortedSearchResultIDs(t, tc, result))
	hasMore, err := result.HasMore()
	require.NoError(t, err)
	require.False(t, hasMore)
}
