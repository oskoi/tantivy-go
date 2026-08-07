package tantivy_go_test

import (
	"os"
	"testing"
	"time"

	"github.com/oskoi/tantivy-go/internal"

	"github.com/stretchr/testify/require"

	tantivy_go "github.com/oskoi/tantivy-go"
)

func TestDateFields(t *testing.T) {
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

	// Add date field for created_at
	err = builder.AddDateField("created_at", true, true, true)
	require.NoError(t, err)

	schema, err := builder.BuildSchema()
	require.NoError(t, err)

	_ = os.RemoveAll("test_date_index")
	tc, err := tantivy_go.NewTantivyContextWithSchema("test_date_index", schema)
	require.NoError(t, err)

	defer func() {
		err := tc.Close()
		require.NoError(t, err)
		_ = os.RemoveAll("test_date_index")
	}()

	err = tc.RegisterTextAnalyzerSimple(tantivy_go.TokenizerSimple, 100, tantivy_go.English)
	require.NoError(t, err)

	// Create documents with different dates
	docs := []struct {
		title     string
		createdAt time.Time
	}{
		{"Old Document", time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"Middle Document", time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)},
		{"Recent Document", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
	}

	for _, d := range docs {
		doc := tantivy_go.NewDocument()
		err = doc.AddField(d.title, tc, "title")
		require.NoError(t, err)
		err = doc.AddDateField(d.createdAt.UnixMilli(), tc, "created_at")
		require.NoError(t, err)
		err = tc.AddAndConsumeDocuments(doc)
		require.NoError(t, err)
	}

	// Verify document count
	count, err := tc.NumDocs()
	require.NoError(t, err)
	require.Equal(t, uint64(3), count)

	// Test 1: Search for documents after a specific date
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	result, err := tc.SearchQueryParser(
		"created_at:["+formatDateRange(startDate)+" TO *]",
		10,
		false,
	)
	require.NoError(t, err)

	size, err := result.GetSize()
	require.NoError(t, err)
	require.Equal(t, 2, int(size), "Should find 2 documents created after 2024-01-01")

	result.Free()

	// Test 2: Search for documents before a specific date
	endDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	result, err = tc.SearchQueryParser(
		"created_at:[* TO "+formatDateRange(endDate)+"]",
		10,
		false,
	)
	require.NoError(t, err)

	size, err = result.GetSize()
	require.NoError(t, err)
	require.Equal(t, 1, int(size), "Should find 1 document created before 2024-01-01")

	result.Free()

	// Test 3: Search for documents within a date range
	startRange := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	endRange := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	result, err = tc.SearchQueryParser(
		"created_at:["+formatDateRange(startRange)+" TO "+formatDateRange(endRange)+"]",
		10,
		false,
	)
	require.NoError(t, err)

	size, err = result.GetSize()
	require.NoError(t, err)
	require.Equal(t, 1, int(size), "Should find 1 document within the date range")

	result.Free()

	// Test 4: Combined query - text search with date filter
	result, err = tc.SearchQueryParser(
		"title:Document AND created_at:["+formatDateRange(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))+" TO *]",
		10,
		false,
	)
	require.NoError(t, err)

	size, err = result.GetSize()
	require.NoError(t, err)
	require.Equal(t, 2, int(size), "Should find 2 documents with 'Document' in title created after 2024-01-01")

	result.Free()

	// Test 5: Verify date values are stored correctly
	result, err = tc.SearchQueryParser("title:Recent", 10, false)
	require.NoError(t, err)

	size, err = result.GetSize()
	require.NoError(t, err)
	require.Equal(t, 1, int(size))

	doc, err := result.Get(0)
	require.NoError(t, err)

	jsonStr, err := doc.ToJSON(tc, "title", "created_at")
	require.NoError(t, err)
	require.Contains(t, jsonStr, "Recent Document")
	// The date should be stored as timestamp in milliseconds
	// 1735689600000 is the Unix timestamp in milliseconds for 2025-01-01 00:00:00 UTC
	require.Contains(t, jsonStr, "1735689600000")

	result.Free()
}

// formatDateRange formats a time.Time for use in tantivy date range queries
// Tantivy expects dates in RFC3339 format for range queries
func formatDateRange(t time.Time) string {
	return t.Format(time.RFC3339)
}
