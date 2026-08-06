package tantivy_go_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tantivy_go "github.com/oskoi/tantivy-go"
	"github.com/oskoi/tantivy-go/internal"
	"github.com/stretchr/testify/require"
)

func TestSearchFastFieldBytes(t *testing.T) {
	// Binding coverage: schema_builder_add_bytes_field + document_add_bytes_field.
	err := internal.LibInit(true, false, "debug")
	require.NoError(t, err)

	builder, err := tantivy_go.NewSchemaBuilder()
	require.NoError(t, err)

	err = builder.AddTextField(
		"title",
		true,
		true,
		false,
		tantivy_go.IndexRecordOptionWithFreqsAndPositions,
		tantivy_go.DefaultTokenizer,
	)
	require.NoError(t, err)

	err = builder.AddBytesField("blob", false, true, false)
	require.NoError(t, err)

	schema, err := builder.BuildSchema()
	require.NoError(t, err)

	_ = os.RemoveAll("index_dir_bytes")
	tc, err := tantivy_go.NewTantivyContextWithSchema("index_dir_bytes", schema)
	require.NoError(t, err)
	defer func() {
		err := tc.Close()
		require.NoError(t, err)
		_ = os.RemoveAll("index_dir_bytes")
	}()

	doc1 := tantivy_go.NewDocument()
	err = doc1.AddField("alpha", tc, "title")
	require.NoError(t, err)
	err = doc1.AddBytesField([]byte{0, 1, 2}, tc, "blob")
	require.NoError(t, err)

	doc2 := tantivy_go.NewDocument()
	err = doc2.AddField("alpha", tc, "title")
	require.NoError(t, err)
	err = doc2.AddBytesField([]byte{255, 254}, tc, "blob")
	require.NoError(t, err)

	err = tc.AddAndConsumeDocuments(doc1, doc2)
	require.NoError(t, err)

	sCtx := tantivy_go.NewSearchContextBuilder().
		SetQuery("alpha").
		SetDocsLimit(10).
		SetWithHighlights(false).
		AddFieldDefaultWeight("title").
		Build()

	result, err := tc.SearchFastField(sCtx, "blob")
	require.NoError(t, err)
	require.Equal(t, 2, len(result.Values))
	require.Equal(t, 2, len(result.Scores))

	values := map[string]bool{}
	for _, v := range result.Values {
		values[v] = true
	}

	require.True(t, values["AAEC"])
	require.True(t, values["//4="])
}

func TestSearchFastFieldTypedValues(t *testing.T) {
	tc, ts := newTypedFastFieldContext(t)

	sCtx := tantivy_go.NewSearchContextBuilder().
		SetQuery("alpha").
		SetDocsLimit(10).
		AddFieldDefaultWeight("title").
		Build()

	u64Result, err := tc.SearchFastFieldU64(sCtx, "u64v")
	requireTypedFastFieldResult(t, u64Result, err, uint64(42))

	i64Result, err := tc.SearchFastFieldI64(sCtx, "i64v")
	requireTypedFastFieldResult(t, i64Result, err, int64(-7))

	f64Result, err := tc.SearchFastFieldF64(sCtx, "f64v")
	requireTypedFastFieldResult(t, f64Result, err, 3.5)

	dateResult, err := tc.SearchFastFieldDate(sCtx, "datev")
	requireTypedFastFieldResult(t, dateResult, err, ts)
}

func TestSearchFastFieldTypedRejectsMismatchedFieldWeights(t *testing.T) {
	tc, _ := newTypedFastFieldContext(t)

	result, err := tc.SearchFastFieldU64(mismatchedFastFieldSearchContext{}, "u64v")
	require.Nil(t, result)
	require.ErrorContains(t, err, "fieldNames and weights length mismatch")
}

func TestSearchFastFieldTypedJSONValues(t *testing.T) {
	tc, ts := newTypedFastFieldContext(t)

	query := tantivy_go.NewQueryBuilder().
		Query(tantivy_go.Must, "title", "alpha", tantivy_go.TermQuery, 1).
		Build()
	sCtx := tantivy_go.NewSearchContextBuilder().
		SetQueryFromJSON(&query).
		SetDocsLimit(10).
		Build()

	u64Result, err := tc.SearchFastFieldU64JSON(sCtx, "u64v")
	requireTypedFastFieldResult(t, u64Result, err, uint64(42))

	i64Result, err := tc.SearchFastFieldI64JSON(sCtx, "i64v")
	requireTypedFastFieldResult(t, i64Result, err, int64(-7))

	f64Result, err := tc.SearchFastFieldF64JSON(sCtx, "f64v")
	requireTypedFastFieldResult(t, f64Result, err, 3.5)

	dateResult, err := tc.SearchFastFieldDateJSON(sCtx, "datev")
	requireTypedFastFieldResult(t, dateResult, err, ts)
}

func newTypedFastFieldContext(t *testing.T) (*tantivy_go.TantivyContext, time.Time) {
	t.Helper()

	require.NoError(t, internal.LibInit(true, false, "debug"))

	builder, err := tantivy_go.NewSchemaBuilder()
	require.NoError(t, err)
	require.NoError(t, builder.AddTextField("title", true, true, false, tantivy_go.IndexRecordOptionWithFreqsAndPositions, tantivy_go.TokenizerSimple))
	require.NoError(t, builder.AddU64Field("u64v", true, true, true))
	require.NoError(t, builder.AddI64Field("i64v", true, true, true))
	require.NoError(t, builder.AddF64Field("f64v", true, true, true))
	require.NoError(t, builder.AddDateField("datev", true, true, true))

	schema, err := builder.BuildSchema()
	require.NoError(t, err)
	t.Cleanup(schema.Free)

	tc, err := tantivy_go.NewTantivyContextWithSchema(filepath.Join(t.TempDir(), "idx"), schema)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })
	require.NoError(t, tc.RegisterTextAnalyzerSimple(tantivy_go.TokenizerSimple, 100, tantivy_go.English))

	ts := time.UnixMilli(1_700_000_000_123).UTC()
	withValues := tantivy_go.NewDocument()
	require.NoError(t, withValues.AddField("alpha", tc, "title"))
	require.NoError(t, withValues.AddU64Field(42, tc, "u64v"))
	require.NoError(t, withValues.AddI64Field(-7, tc, "i64v"))
	require.NoError(t, withValues.AddF64Field(3.5, tc, "f64v"))
	require.NoError(t, withValues.AddDateField(ts.UnixMilli(), tc, "datev"))

	withoutValues := tantivy_go.NewDocument()
	require.NoError(t, withoutValues.AddField("alpha", tc, "title"))

	require.NoError(t, tc.AddAndConsumeDocuments(withValues, withoutValues))
	return tc, ts
}

func requireTypedFastFieldResult[T comparable](t *testing.T, result *tantivy_go.TypedFastFieldResult[T], err error, want T) {
	t.Helper()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Values, 2)
	require.Len(t, result.Valid, 2)
	require.Len(t, result.Scores, 2)

	var zero T
	validCount := 0
	for i, valid := range result.Valid {
		if valid {
			validCount++
			require.Equal(t, want, result.Values[i])
			continue
		}
		require.Equal(t, zero, result.Values[i])
	}
	require.Equal(t, 1, validCount)
}

type mismatchedFastFieldSearchContext struct{}

func (mismatchedFastFieldSearchContext) GetQuery() string { return "alpha" }

func (mismatchedFastFieldSearchContext) GetDocsLimit() uintptr { return 10 }

func (mismatchedFastFieldSearchContext) WithHighlights() bool { return false }

func (mismatchedFastFieldSearchContext) GetFieldAndWeights() ([]string, []float32) {
	return []string{"title"}, nil
}
