package tantivy_go_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	tantivy_go "github.com/oskoi/tantivy-go"
	"github.com/oskoi/tantivy-go/internal"
	"github.com/stretchr/testify/require"
)

const (
	sortedSearchDocIDField = "doc_id"
	sortedSearchTextField  = "textv"
	sortedSearchU64Field   = "u64v"
	sortedSearchI64Field   = "i64v"
	sortedSearchF64Field   = "f64v"
	sortedSearchDateField  = "datev"
	sortedSearchJSONField  = "payload"
	sortedSearchBoolPath   = "payload.active"
)

type sortedSearchFixtureDoc struct {
	ID   string
	Text *string
	U64  *uint64
	I64  *int64
	F64  *float64
	Bool *bool
	Date *int64
}

func TestSearchSortedMultipleFields(t *testing.T) {
	tc := newSortedSearchContext(t,
		sortedSearchFixtureDoc{
			ID:   "first",
			Text: sortedSearchPtr("alpha"),
			U64:  sortedSearchPtr(uint64(20)),
			I64:  sortedSearchPtr(int64(-7)),
			F64:  sortedSearchPtr(3.5),
		},
		sortedSearchFixtureDoc{
			ID:   "second",
			Text: sortedSearchPtr("alpha"),
			U64:  sortedSearchPtr(uint64(20)),
			I64:  sortedSearchPtr(int64(-7)),
			F64:  sortedSearchPtr(2.5),
		},
		sortedSearchFixtureDoc{
			ID:   "third",
			Text: sortedSearchPtr("alpha"),
			U64:  sortedSearchPtr(uint64(10)),
			I64:  sortedSearchPtr(int64(-10)),
			F64:  sortedSearchPtr(9.0),
		},
		sortedSearchFixtureDoc{
			ID:   "fourth",
			Text: sortedSearchPtr("beta"),
			U64:  sortedSearchPtr(uint64(99)),
			I64:  sortedSearchPtr(int64(8)),
			F64:  sortedSearchPtr(0.5),
		},
	)

	result, err := tc.SearchSorted(tantivy_go.SortedQueryRequest{
		Query: "*",
		Limit: 4,
		Sort: []tantivy_go.SortField{
			{Name: sortedSearchTextField, Direction: tantivy_go.SortAscending},
			{Name: sortedSearchU64Field, Direction: tantivy_go.SortDescending},
			{Name: sortedSearchI64Field, Direction: tantivy_go.SortAscending},
			{Name: sortedSearchF64Field, Direction: tantivy_go.SortDescending},
		},
		Timeout: time.Second,
	})
	require.NoError(t, err)
	defer result.Free()

	require.Equal(t, []string{"first", "second", "third", "fourth"}, sortedSearchResultIDs(t, tc, result))
	hasMore, err := result.HasMore()
	require.NoError(t, err)
	require.False(t, hasMore)

	requireSortedSearchTuple(t, result, 0, []tantivy_go.SortValue{
		{Kind: tantivy_go.SortValueText, Text: "alpha"},
		{Kind: tantivy_go.SortValueU64, U64: 20},
		{Kind: tantivy_go.SortValueI64, I64: -7},
		{Kind: tantivy_go.SortValueF64, F64: 3.5},
	})
	requireSortedSearchTuple(t, result, 1, []tantivy_go.SortValue{
		{Kind: tantivy_go.SortValueText, Text: "alpha"},
		{Kind: tantivy_go.SortValueU64, U64: 20},
		{Kind: tantivy_go.SortValueI64, I64: -7},
		{Kind: tantivy_go.SortValueF64, F64: 2.5},
	})
	requireSortedSearchTuple(t, result, 2, []tantivy_go.SortValue{
		{Kind: tantivy_go.SortValueText, Text: "alpha"},
		{Kind: tantivy_go.SortValueU64, U64: 10},
		{Kind: tantivy_go.SortValueI64, I64: -10},
		{Kind: tantivy_go.SortValueF64, F64: 9},
	})
	requireSortedSearchTuple(t, result, 3, []tantivy_go.SortValue{
		{Kind: tantivy_go.SortValueText, Text: "beta"},
		{Kind: tantivy_go.SortValueU64, U64: 99},
		{Kind: tantivy_go.SortValueI64, I64: 8},
		{Kind: tantivy_go.SortValueF64, F64: 0.5},
	})
}

func TestSearchSortedMissingLastBothDirections(t *testing.T) {
	const earlyDateMillis = int64(1_700_000_000_123)
	const lateDateMillis = int64(1_700_000_001_456)

	tc := newSortedSearchContext(t,
		sortedSearchFixtureDoc{
			ID:   "low",
			Text: sortedSearchPtr("alpha"),
			U64:  sortedSearchPtr(uint64(1)),
			I64:  sortedSearchPtr(int64(-1)),
			F64:  sortedSearchPtr(1.5),
			Bool: sortedSearchPtr(false),
			Date: sortedSearchPtr(earlyDateMillis),
		},
		sortedSearchFixtureDoc{
			ID:   "high",
			Text: sortedSearchPtr("omega"),
			U64:  sortedSearchPtr(uint64(2)),
			I64:  sortedSearchPtr(int64(1)),
			F64:  sortedSearchPtr(2.5),
			Bool: sortedSearchPtr(true),
			Date: sortedSearchPtr(lateDateMillis),
		},
		sortedSearchFixtureDoc{ID: "missing"},
	)

	sortCases := []struct {
		name  string
		field string
		asc   []tantivy_go.SortValue
		desc  []tantivy_go.SortValue
	}{
		{
			name:  "text",
			field: sortedSearchTextField,
			asc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueText, Text: "alpha"},
				{Kind: tantivy_go.SortValueText, Text: "omega"},
				{Kind: tantivy_go.SortValueText, Missing: true},
			},
			desc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueText, Text: "omega"},
				{Kind: tantivy_go.SortValueText, Text: "alpha"},
				{Kind: tantivy_go.SortValueText, Missing: true},
			},
		},
		{
			name:  "u64",
			field: sortedSearchU64Field,
			asc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueU64, U64: 1},
				{Kind: tantivy_go.SortValueU64, U64: 2},
				{Kind: tantivy_go.SortValueU64, Missing: true},
			},
			desc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueU64, U64: 2},
				{Kind: tantivy_go.SortValueU64, U64: 1},
				{Kind: tantivy_go.SortValueU64, Missing: true},
			},
		},
		{
			name:  "i64",
			field: sortedSearchI64Field,
			asc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueI64, I64: -1},
				{Kind: tantivy_go.SortValueI64, I64: 1},
				{Kind: tantivy_go.SortValueI64, Missing: true},
			},
			desc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueI64, I64: 1},
				{Kind: tantivy_go.SortValueI64, I64: -1},
				{Kind: tantivy_go.SortValueI64, Missing: true},
			},
		},
		{
			name:  "f64",
			field: sortedSearchF64Field,
			asc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueF64, F64: 1.5},
				{Kind: tantivy_go.SortValueF64, F64: 2.5},
				{Kind: tantivy_go.SortValueF64, Missing: true},
			},
			desc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueF64, F64: 2.5},
				{Kind: tantivy_go.SortValueF64, F64: 1.5},
				{Kind: tantivy_go.SortValueF64, Missing: true},
			},
		},
		{
			name:  "bool json subpath",
			field: sortedSearchBoolPath,
			asc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueBool, Bool: false},
				{Kind: tantivy_go.SortValueBool, Bool: true},
				{Kind: tantivy_go.SortValueBool, Missing: true},
			},
			desc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueBool, Bool: true},
				{Kind: tantivy_go.SortValueBool, Bool: false},
				{Kind: tantivy_go.SortValueBool, Missing: true},
			},
		},
		{
			name:  "date unix milliseconds",
			field: sortedSearchDateField,
			asc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueDate, I64: earlyDateMillis},
				{Kind: tantivy_go.SortValueDate, I64: lateDateMillis},
				{Kind: tantivy_go.SortValueDate, Missing: true},
			},
			desc: []tantivy_go.SortValue{
				{Kind: tantivy_go.SortValueDate, I64: lateDateMillis},
				{Kind: tantivy_go.SortValueDate, I64: earlyDateMillis},
				{Kind: tantivy_go.SortValueDate, Missing: true},
			},
		},
	}

	for _, sortCase := range sortCases {
		sortCase := sortCase
		t.Run(sortCase.name, func(t *testing.T) {
			directions := []struct {
				name      string
				direction tantivy_go.SortDirection
				wantIDs   []string
				want      []tantivy_go.SortValue
			}{
				{
					name:      "ascending",
					direction: tantivy_go.SortAscending,
					wantIDs:   []string{"low", "high", "missing"},
					want:      sortCase.asc,
				},
				{
					name:      "descending",
					direction: tantivy_go.SortDescending,
					wantIDs:   []string{"high", "low", "missing"},
					want:      sortCase.desc,
				},
			}

			for _, directionCase := range directions {
				directionCase := directionCase
				t.Run(directionCase.name, func(t *testing.T) {
					result, err := tc.SearchSorted(tantivy_go.SortedQueryRequest{
						Query: "*",
						Limit: 3,
						Sort: []tantivy_go.SortField{{
							Name:      sortCase.field,
							Direction: directionCase.direction,
						}},
						Timeout: time.Second,
					})
					require.NoError(t, err)
					defer result.Free()

					require.Equal(t, directionCase.wantIDs, sortedSearchResultIDs(t, tc, result))
					hasMore, err := result.HasMore()
					require.NoError(t, err)
					require.False(t, hasMore)
					for i, want := range directionCase.want {
						requireSortedSearchTuple(t, result, uint64(i), []tantivy_go.SortValue{want})
					}
				})
			}
		})
	}
}

func TestSearchSortedRejectsNativeDescriptorErrors(t *testing.T) {
	tc := newSortedSearchContext(t)
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
			name: "unknown sort field",
			request: tantivy_go.SortedQueryRequest{
				Query: "*",
				Limit: 1,
				Sort: []tantivy_go.SortField{{
					Name:      "missing_field",
					Direction: tantivy_go.SortAscending,
				}},
				Timeout: time.Second,
			},
			want: "missing_field",
		},
		{
			name: "search-after kind mismatch",
			request: tantivy_go.SortedQueryRequest{
				Query: "*",
				Limit: 1,
				Sort:  validSort,
				After: []tantivy_go.SortValue{{
					Kind: tantivy_go.SortValueText,
					Text: "not a u64",
				}},
				Timeout: time.Second,
			},
			want: "kind",
		},
	}

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
		})
	}
}

func newSortedSearchContext(t *testing.T, records ...sortedSearchFixtureDoc) *tantivy_go.TantivyContext {
	t.Helper()

	require.NoError(t, internal.LibInit(true, false, "debug"))

	builder, err := tantivy_go.NewSchemaBuilder()
	require.NoError(t, err)
	require.NoError(t, builder.AddTextField(sortedSearchDocIDField, true, false, true, true, tantivy_go.IndexRecordOptionBasic, tantivy_go.TokenizerRaw))
	require.NoError(t, builder.AddTextField(sortedSearchTextField, true, false, true, true, tantivy_go.IndexRecordOptionBasic, tantivy_go.TokenizerRaw))
	require.NoError(t, builder.AddU64Field(sortedSearchU64Field, true, true, true))
	require.NoError(t, builder.AddI64Field(sortedSearchI64Field, true, true, true))
	require.NoError(t, builder.AddF64Field(sortedSearchF64Field, true, true, true))
	require.NoError(t, builder.AddDateField(sortedSearchDateField, true, true, true))

	jsonOptions := tantivy_go.NewJSONFieldOptions()
	jsonOptions.Stored = true
	jsonOptions.IsFast = true
	jsonOptions.IsIndexed = true
	jsonOptions.IndexTokenizer = tantivy_go.TokenizerRaw
	jsonOptions.FastTokenizer = tantivy_go.TokenizerRaw
	require.NoError(t, builder.AddJSONField(sortedSearchJSONField, jsonOptions))

	schema, err := builder.BuildSchema()
	require.NoError(t, err)
	t.Cleanup(schema.Free)

	tc, err := tantivy_go.NewTantivyContextWithSchema(filepath.Join(t.TempDir(), "idx"), schema)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })
	require.NoError(t, tc.RegisterTextAnalyzerRaw(tantivy_go.TokenizerRaw))

	docs := make([]*tantivy_go.Document, 0, len(records))
	for _, record := range records {
		doc := tantivy_go.NewDocument()
		require.NoError(t, doc.AddField(record.ID, tc, sortedSearchDocIDField))
		if record.Text != nil {
			require.NoError(t, doc.AddField(*record.Text, tc, sortedSearchTextField))
		}
		if record.U64 != nil {
			require.NoError(t, doc.AddU64Field(*record.U64, tc, sortedSearchU64Field))
		}
		if record.I64 != nil {
			require.NoError(t, doc.AddI64Field(*record.I64, tc, sortedSearchI64Field))
		}
		if record.F64 != nil {
			require.NoError(t, doc.AddF64Field(*record.F64, tc, sortedSearchF64Field))
		}
		if record.Bool != nil {
			value, err := json.Marshal(map[string]bool{"active": *record.Bool})
			require.NoError(t, err)
			require.NoError(t, doc.AddJSONField(string(value), tc, sortedSearchJSONField))
		}
		if record.Date != nil {
			require.NoError(t, doc.AddDateField(*record.Date, tc, sortedSearchDateField))
		}
		docs = append(docs, doc)
	}
	if len(docs) > 0 {
		require.NoError(t, tc.AddAndConsumeDocuments(docs...))
	}

	return tc
}

func sortedSearchResultIDs(t *testing.T, tc *tantivy_go.TantivyContext, result *tantivy_go.SearchResult) []string {
	t.Helper()

	size, err := result.GetSize()
	require.NoError(t, err)
	ids := make([]string, 0, size)
	for i := uint64(0); i < size; i++ {
		doc, err := result.Get(i)
		require.NoError(t, err)
		value, err := doc.ToJSON(tc, sortedSearchDocIDField)
		doc.Free()
		require.NoError(t, err)

		var decoded struct {
			ID string `json:"doc_id"`
		}
		require.NoError(t, json.Unmarshal([]byte(value), &decoded))
		require.NotEmpty(t, decoded.ID)
		ids = append(ids, decoded.ID)
	}
	return ids
}

func requireSortedSearchTuple(t *testing.T, result *tantivy_go.SearchResult, index uint64, want []tantivy_go.SortValue) {
	t.Helper()

	got, err := result.SortValues(index)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func sortedSearchPtr[T any](value T) *T {
	return &value
}
