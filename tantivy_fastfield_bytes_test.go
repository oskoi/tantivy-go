package tantivy_go_test

import (
	"os"
	"testing"

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
