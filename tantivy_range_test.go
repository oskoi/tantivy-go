package tantivy_go_test

import (
	"os"
	"testing"

	"github.com/oskoi/tantivy-go/internal"

	"github.com/stretchr/testify/require"

	tantivy_go "github.com/oskoi/tantivy-go"
)

func TestRangeQueries(t *testing.T) {
	err := internal.LibInit(true, false, "debug")
	require.NoError(t, err)

	builder, err := tantivy_go.NewSchemaBuilder()
	require.NoError(t, err)

	// Add text field for title
	err = builder.AddTextField(
		"title",
		true,
		true,
		false,
		true,
		tantivy_go.IndexRecordOptionWithFreqsAndPositions,
		tantivy_go.TokenizerSimple,
	)
	require.NoError(t, err)

	// Add numeric field for price
	err = builder.AddU64Field("price", true, true, true)
	require.NoError(t, err)

	schema, err := builder.BuildSchema()
	require.NoError(t, err)

	_ = os.RemoveAll("test_range_index")
	tc, err := tantivy_go.NewTantivyContextWithSchema("test_range_index", schema)
	require.NoError(t, err)

	defer func() {
		err := tc.Close()
		require.NoError(t, err)
		_ = os.RemoveAll("test_range_index")
	}()

	err = tc.RegisterTextAnalyzerSimple(tantivy_go.TokenizerSimple, 100, tantivy_go.English)
	require.NoError(t, err)

	// Create documents with different prices
	docs := []struct {
		title string
		price uint64
	}{
		{"Cheap Product", 10},
		{"Medium Product", 50},
		{"Expensive Product", 100},
		{"Very Expensive Product", 200},
	}

	for _, d := range docs {
		doc := tantivy_go.NewDocument()
		err = doc.AddField(d.title, tc, "title")
		require.NoError(t, err)
		err = doc.AddU64Field(d.price, tc, "price")
		require.NoError(t, err)
		err = tc.AddAndConsumeDocuments(doc)
		require.NoError(t, err)
	}

	// Test range query: price between 20 and 150
	result, err := tc.SearchQueryParser("price:[20 TO 150]", 10, false)
	require.NoError(t, err)

	size, err := result.GetSize()
	require.NoError(t, err)
	require.Equal(t, 2, int(size), "Should find 2 products with price between 20 and 150")

	result.Free()

	// Test range query with exclusive bounds: price > 50 and price < 200
	result, err = tc.SearchQueryParser("price:{50 TO 200}", 10, false)
	require.NoError(t, err)

	size, err = result.GetSize()
	require.NoError(t, err)
	require.Equal(t, 1, int(size), "Should find 1 product with price between 50 and 200 (exclusive)")

	result.Free()

	// Test range query with text field combined: title with "Product" AND price > 75
	result, err = tc.SearchQueryParser("title:Product AND price:[75 TO *]", 10, false)
	require.NoError(t, err)

	size, err = result.GetSize()
	require.NoError(t, err)
	require.Equal(t, 2, int(size), "Should find 2 products with 'Product' in title and price >= 75")

	result.Free()
}
