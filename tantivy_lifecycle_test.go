package tantivy_go

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oskoi/tantivy-go/internal"
	"github.com/stretchr/testify/require"
)

func newLifecycleContext(t *testing.T) *TantivyContext {
	t.Helper()
	return newLifecycleContextAt(t, filepath.Join(t.TempDir(), "idx"))
}

func newLifecycleContextAt(t *testing.T, indexPath string) *TantivyContext {
	t.Helper()

	require.NoError(t, internal.LibInit(true, false, "debug"))

	builder, err := NewSchemaBuilder()
	require.NoError(t, err)
	require.NoError(t, builder.AddTextField("id", true, false, false, true, IndexRecordOptionBasic, TokenizerSimple))
	require.NoError(t, builder.AddTextField("body", true, true, false, true, IndexRecordOptionWithFreqsAndPositions, TokenizerSimple))

	schema, err := builder.BuildSchema()
	require.NoError(t, err)
	t.Cleanup(schema.Free)

	tc, err := NewTantivyContextWithSchema(indexPath, schema)
	require.NoError(t, err)
	require.NoError(t, tc.RegisterTextAnalyzerSimple(TokenizerSimple, 100, English))
	return tc
}

func TestDocumentConsumedAfterAdd(t *testing.T) {
	tc := newLifecycleContext(t)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })

	doc := NewDocument()
	require.NoError(t, doc.AddField("1", tc, "id"))
	require.NoError(t, doc.AddField("hello", tc, "body"))

	require.NoError(t, tc.AddAndConsumeDocuments(doc))

	doc.Free()
	err := doc.AddField("again", tc, "body")
	require.ErrorIs(t, err, ErrConsumedDocument)
}

func TestBatchAddConsumesDocumentsButDoesNotIndexOnDeleteValidationError(t *testing.T) {
	tc := newLifecycleContext(t)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })

	doc := NewDocument()
	require.NoError(t, doc.AddField("1", tc, "id"))
	require.NoError(t, doc.AddField("hello", tc, "body"))

	opstamp, err := tc.BatchAddAndDeleteDocumentsWithOpstamp(
		[]*Document{doc},
		"id",
		[]string{string([]byte{0xff})},
	)
	require.Zero(t, opstamp)
	require.Error(t, err)
	require.ErrorIs(t, doc.AddField("again", tc, "body"), ErrConsumedDocument)

	n, err := tc.NumDocs()
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestCStringInputsRemainUsableThroughFFI(t *testing.T) {
	tc := newLifecycleContext(t)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })

	for i := 0; i < 25; i++ {
		doc := NewDocument()
		require.NoError(t, doc.AddField("same-id", tc, "id"))
		require.NoError(t, doc.AddField("repeat body", tc, "body"))
		require.NoError(t, tc.AddAndConsumeDocuments(doc))
	}

	n, err := tc.NumDocs()
	require.NoError(t, err)
	require.Equal(t, uint64(25), n)
}

func TestClosedContextSentinelErrors(t *testing.T) {
	require.True(t, errors.Is(ErrClosedContext, ErrClosedContext))
}

func TestReloadReaderReturnsErrorWhenCommittedMetaIsMissing(t *testing.T) {
	indexPath := filepath.Join(t.TempDir(), "idx")
	tc := newLifecycleContextAt(t, indexPath)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })

	doc := NewDocument()
	require.NoError(t, doc.AddField("1", tc, "id"))
	require.NoError(t, doc.AddField("reload fault", tc, "body"))
	require.NoError(t, tc.AddAndConsumeDocuments(doc))
	require.NoError(t, os.Remove(filepath.Join(indexPath, "meta.json")))

	require.Error(t, tc.ReloadReader())
}

func TestCloseIsIdempotent(t *testing.T) {
	tc := newLifecycleContext(t)

	require.NoError(t, tc.Close())
	require.NoError(t, tc.Close())
}

func TestUseAfterCloseReturnsClosedContext(t *testing.T) {
	tc := newLifecycleContext(t)
	require.NoError(t, tc.Close())

	_, err := tc.NumDocs()
	require.ErrorIs(t, err, ErrClosedContext)

	sCtx := NewSearchContextBuilder().
		SetQuery("hello").
		SetDocsLimit(10).
		AddFieldDefaultWeight("body").
		Build()
	_, err = tc.Search(sCtx)
	require.ErrorIs(t, err, ErrClosedContext)
}

func TestConcurrentSearchAndCloseDoesNotUseFreedContext(t *testing.T) {
	tc := newLifecycleContext(t)

	docs := make([]*Document, 100)
	for i := range docs {
		doc := NewDocument()
		require.NoError(t, doc.AddField("1", tc, "id"))
		require.NoError(t, doc.AddField("hello", tc, "body"))
		docs[i] = doc
	}
	require.NoError(t, tc.AddAndConsumeDocuments(docs...))

	sCtx := NewSearchContextBuilder().
		SetQuery("hello").
		SetDocsLimit(100).
		AddFieldDefaultWeight("body").
		Build()

	const workers = 8
	var ready sync.WaitGroup
	var done sync.WaitGroup
	var successes atomic.Int64
	stop := make(chan struct{})
	errs := make(chan error, workers)

	ready.Add(workers)
	done.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer done.Done()
			ready.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				result, err := tc.Search(sCtx)
				if err == nil {
					successes.Add(1)
					result.Free()
					continue
				}
				if errors.Is(err, ErrClosedContext) {
					return
				}
				errs <- err
				return
			}
		}()
	}

	ready.Wait()
	require.Eventually(t, func() bool {
		return successes.Load() > 0
	}, time.Second, time.Millisecond)

	require.NoError(t, tc.Close())
	close(stop)
	done.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	require.Positive(t, successes.Load())
	require.NoError(t, tc.Close())
}

func TestSearchResultGetOnEmptyResultReturnsError(t *testing.T) {
	tc := newLifecycleContext(t)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })

	sCtx := NewSearchContextBuilder().
		SetQuery("missing").
		SetDocsLimit(10).
		AddFieldDefaultWeight("body").
		Build()

	result, err := tc.Search(sCtx)
	require.NoError(t, err)
	t.Cleanup(result.Free)

	size, err := result.GetSize()
	require.NoError(t, err)
	require.Equal(t, uint64(0), size)

	doc, err := result.Get(0)
	require.Nil(t, doc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")
}

func TestSchemaBuilderDoesNotRecordFailedField(t *testing.T) {
	require.NoError(t, internal.LibInit(true, false, "debug"))
	builder, err := NewSchemaBuilder()
	require.NoError(t, err)

	err = builder.AddTextField("body", true, true, false, true, 999, TokenizerSimple)
	require.Error(t, err)

	err = builder.AddTextField("body", true, true, false, true, IndexRecordOptionWithFreqsAndPositions, TokenizerSimple)
	require.NoError(t, err)
}

func TestSchemaAndBuilderFreeAreIdempotent(t *testing.T) {
	require.NoError(t, internal.LibInit(true, false, "debug"))
	builder, err := NewSchemaBuilder()
	require.NoError(t, err)
	require.NoError(t, builder.AddTextField("body", true, true, false, true, IndexRecordOptionWithFreqsAndPositions, TokenizerSimple))

	schema, err := builder.BuildSchema()
	require.NoError(t, err)

	_, err = builder.BuildSchema()
	require.ErrorIs(t, err, ErrClosedSchemaBuilder)

	schema.Free()
	schema.Free()

	_, err = NewTantivyContextWithSchema(filepath.Join(t.TempDir(), "idx"), schema)
	require.ErrorIs(t, err, ErrClosedSchema)
}

func TestToJSONWithNoIncludeFieldsReturnsAllFields(t *testing.T) {
	tc := newLifecycleContext(t)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })

	doc := NewDocument()
	require.NoError(t, doc.AddField("1", tc, "id"))
	require.NoError(t, doc.AddField("hello", tc, "body"))

	jsonStr, err := doc.ToJSON(tc)
	require.NoError(t, err)
	require.Contains(t, jsonStr, "id")
	require.Contains(t, jsonStr, "body")

	doc.Free()
}

func TestSearchQueryParserRejectsInvalidLimit(t *testing.T) {
	tc := newLifecycleContext(t)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })

	result, err := tc.SearchQueryParser("body:hello", -1, false)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrInvalidDocsLimit)

	result, err = tc.SearchQueryParser("body:hello", 0, false)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrInvalidDocsLimit)
}

func TestSearchRejectsMismatchedFieldWeights(t *testing.T) {
	tc := newLifecycleContext(t)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })

	result, err := tc.Search(mismatchedSearchContext{})
	require.Nil(t, result)
	require.ErrorContains(t, err, "fieldNames and weights length mismatch")
}

func TestGetSearchResultsReturnsEmptySliceForEmptyResult(t *testing.T) {
	tc := newLifecycleContext(t)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })

	sCtx := NewSearchContextBuilder().
		SetQuery("missing").
		SetDocsLimit(10).
		AddFieldDefaultWeight("body").
		Build()

	result, err := tc.Search(sCtx)
	require.NoError(t, err)

	values, err := GetSearchResults(result, tc, func(json string) (string, error) {
		return json, nil
	}, "id")
	require.NoError(t, err)
	require.Empty(t, values)
}

func TestGetSearchResultsReturnsSearchResultError(t *testing.T) {
	tc := newLifecycleContext(t)
	t.Cleanup(func() { require.NoError(t, tc.Close()) })

	sCtx := NewSearchContextBuilder().
		SetQuery("missing").
		SetDocsLimit(10).
		AddFieldDefaultWeight("body").
		Build()

	result, err := tc.Search(sCtx)
	require.NoError(t, err)
	result.Free()

	values, err := GetSearchResults(result, tc, func(json string) (string, error) {
		return json, nil
	}, "id")
	require.Error(t, err)
	require.Nil(t, values)
}

func TestBooleanQueryRejectsIndependentNestedBuilder(t *testing.T) {
	parent := NewQueryBuilder()
	child := NewQueryBuilder().Query(Must, "body", "hello", TermQuery, 1)

	require.PanicsWithValue(t,
		"nested query builder must be created with parent.NestedBuilder()",
		func() { parent.BooleanQuery(Must, child, 1) },
	)
}

type mismatchedSearchContext struct{}

func (mismatchedSearchContext) GetQuery() string { return "hello" }

func (mismatchedSearchContext) GetDocsLimit() uintptr { return 10 }

func (mismatchedSearchContext) WithHighlights() bool { return false }

func (mismatchedSearchContext) GetFieldAndWeights() ([]string, []float32) {
	return []string{"body"}, nil
}
