package tantivy_go_test

import (
	"os"
	"testing"

	"github.com/oskoi/tantivy-go/internal"

	"github.com/stretchr/testify/require"

	tantivy_go "github.com/oskoi/tantivy-go"
)

func TestNumericFields(t *testing.T) {
	err := internal.LibInit(true, false, "debug")
	require.NoError(t, err)

	builder, err := tantivy_go.NewSchemaBuilder()
	require.NoError(t, err)

	// Add text field for ID
	err = builder.AddTextField(
		"id",
		true,
		false,
		false,
		true,
		tantivy_go.IndexRecordOptionBasic,
		tantivy_go.TokenizerRaw,
	)
	require.NoError(t, err)

	// Add numeric fields
	err = builder.AddU64Field("count", true, true, true)
	require.NoError(t, err)

	err = builder.AddI64Field("temperature", true, true, true)
	require.NoError(t, err)

	err = builder.AddF64Field("score", true, true, true)
	require.NoError(t, err)

	schema, err := builder.BuildSchema()
	require.NoError(t, err)

	_ = os.RemoveAll("test_numeric_index")
	tc, err := tantivy_go.NewTantivyContextWithSchema("test_numeric_index", schema)
	require.NoError(t, err)

	defer func() {
		err := tc.Close()
		require.NoError(t, err)
		_ = os.RemoveAll("test_numeric_index")
	}()

	err = tc.RegisterTextAnalyzerRaw(tantivy_go.TokenizerRaw)
	require.NoError(t, err)

	// Create documents with numeric fields
	doc1 := tantivy_go.NewDocument()
	err = doc1.AddField("doc1", tc, "id")
	require.NoError(t, err)
	err = doc1.AddU64Field(100, tc, "count")
	require.NoError(t, err)
	err = doc1.AddI64Field(-10, tc, "temperature")
	require.NoError(t, err)
	err = doc1.AddF64Field(95.5, tc, "score")
	require.NoError(t, err)

	doc2 := tantivy_go.NewDocument()
	err = doc2.AddField("doc2", tc, "id")
	require.NoError(t, err)
	err = doc2.AddU64Field(200, tc, "count")
	require.NoError(t, err)
	err = doc2.AddI64Field(25, tc, "temperature")
	require.NoError(t, err)
	err = doc2.AddF64Field(87.3, tc, "score")
	require.NoError(t, err)

	// Add documents
	err = tc.AddAndConsumeDocuments(doc1, doc2)
	require.NoError(t, err)

	// Verify document count
	count, err := tc.NumDocs()
	require.NoError(t, err)
	require.Equal(t, uint64(2), count)

	// Test search with text query
	sCtx := tantivy_go.NewSearchContextBuilder().
		SetQuery("doc1").
		SetDocsLimit(10).
		SetWithHighlights(false).
		AddFieldDefaultWeight("id").
		Build()

	result, err := tc.Search(sCtx)
	require.NoError(t, err)

	size, err := result.GetSize()
	require.NoError(t, err)
	require.Equal(t, 1, int(size))

	// Get the document and verify it contains all fields
	doc, err := result.Get(0)
	require.NoError(t, err)

	jsonStr, err := doc.ToJSON(tc, "id", "count", "temperature", "score")
	require.NoError(t, err)

	// Verify the document contains our numeric values
	require.Contains(t, jsonStr, "doc1")
	require.Contains(t, jsonStr, "100")
	require.Contains(t, jsonStr, "-10")
	require.Contains(t, jsonStr, "95.5")

	result.Free()
}
