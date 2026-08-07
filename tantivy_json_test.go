package tantivy_go_test

import (
	"encoding/json"
	"os"
	"testing"

	tantivy_go "github.com/oskoi/tantivy-go"
	"github.com/oskoi/tantivy-go/internal"
	"github.com/stretchr/testify/require"
)

func newJSONContext(t *testing.T, indexDir string, expandDots bool) *tantivy_go.TantivyContext {
	err := internal.LibInit(true, false, "debug")
	require.NoError(t, err)

	builder, err := tantivy_go.NewSchemaBuilder()
	require.NoError(t, err)

	err = builder.AddTextField(
		"title",
		true,
		true,
		false,
		true,
		tantivy_go.IndexRecordOptionWithFreqsAndPositions,
		tantivy_go.DefaultTokenizer,
	)
	require.NoError(t, err)

	jsonOpts := tantivy_go.NewJSONFieldOptions()
	jsonOpts.Stored = true
	jsonOpts.IsIndexed = true
	jsonOpts.IsFast = true
	jsonOpts.FastTokenizer = tantivy_go.DefaultTokenizer
	jsonOpts.ExpandDotsEnabled = expandDots
	err = builder.AddJSONField("payload", jsonOpts)
	require.NoError(t, err)

	schema, err := builder.BuildSchema()
	require.NoError(t, err)

	_ = os.RemoveAll(indexDir)
	tc, err := tantivy_go.NewTantivyContextWithSchema(indexDir, schema)
	require.NoError(t, err)

	return tc
}

func TestJSONFieldQueryParser(t *testing.T) {
	// Binding coverage: schema_builder_add_json_field + document_add_json_field.
	tc := newJSONContext(t, "index_dir_json", false)
	defer func() {
		err := tc.Close()
		require.NoError(t, err)
		_ = os.RemoveAll("index_dir_json")
	}()

	doc1 := tantivy_go.NewDocument()
	err := doc1.AddField("alpha", tc, "title")
	require.NoError(t, err)
	err = doc1.AddJSONField(`{"meta":{"author":"alice"},"k8s.node.id":5}`, tc, "payload")
	require.NoError(t, err)

	doc2 := tantivy_go.NewDocument()
	err = doc2.AddField("beta", tc, "title")
	require.NoError(t, err)
	err = doc2.AddJSONField(`{"meta":{"author":"bob"}}`, tc, "payload")
	require.NoError(t, err)

	err = tc.AddAndConsumeDocuments(doc1, doc2)
	require.NoError(t, err)

	result, err := tc.SearchQueryParser("payload.meta.author:alice", 10, false)
	require.NoError(t, err)

	size, err := result.GetSize()
	require.NoError(t, err)
	require.Equal(t, 1, int(size))

	docs, err := tantivy_go.GetSearchResults(result, tc, func(jsonStr string) (map[string]any, error) {
		out := map[string]any{}
		return out, json.Unmarshal([]byte(jsonStr), &out)
	}, "title", "payload")
	require.NoError(t, err)
	require.Equal(t, "alpha", docs[0]["title"])
	require.Equal(t, map[string]any{
		"k8s.node.id": float64(5),
		"meta":        map[string]any{"author": "alice"},
	}, docs[0]["payload"])

	escapedResult, err := tc.SearchQueryParser(`payload.k8s\.node\.id:5`, 10, false)
	require.NoError(t, err)
	escapedSize, err := escapedResult.GetSize()
	require.NoError(t, err)
	require.Equal(t, 1, int(escapedSize))
	escapedResult.Free()
}

func TestJSONFieldExpandDotsEnabled(t *testing.T) {
	tc := newJSONContext(t, "index_dir_json_expand", true)
	defer func() {
		err := tc.Close()
		require.NoError(t, err)
		_ = os.RemoveAll("index_dir_json_expand")
	}()

	doc := tantivy_go.NewDocument()
	err := doc.AddJSONField(`{"k8s.node.id":5}`, tc, "payload")
	require.NoError(t, err)
	err = tc.AddAndConsumeDocuments(doc)
	require.NoError(t, err)

	result, err := tc.SearchQueryParser("payload.k8s.node.id:5", 10, false)
	require.NoError(t, err)
	size, err := result.GetSize()
	require.NoError(t, err)
	require.Equal(t, 1, int(size))
	result.Free()
}

func TestJSONFieldRejectsInvalidJSON(t *testing.T) {
	tc := newJSONContext(t, "index_dir_json_invalid", false)
	defer func() {
		err := tc.Close()
		require.NoError(t, err)
		_ = os.RemoveAll("index_dir_json_invalid")
	}()

	doc := tantivy_go.NewDocument()
	err := doc.AddJSONField(`{"meta":`, tc, "payload")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Invalid JSON value")
}
