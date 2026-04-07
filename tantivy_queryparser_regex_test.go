package tantivy_go_test

import (
	"encoding/json"
	"testing"

	tantivy_go "github.com/oskoi/tantivy-go"
	"github.com/stretchr/testify/require"
)

func TestSearchQueryParserRegexOption(t *testing.T) {
	// Binding coverage: context_search_query_parser -> SearchQueryParser with QueryParserOption.
	_, tc := fx(t, limit, minGram, false, false)

	defer func() {
		err := tc.Close()
		require.NoError(t, err)
	}()

	doc1, err := addDoc(t, "three foxes", "", "1", tc)
	require.NoError(t, err)
	doc2, err := addDoc(t, "throne room", "", "2", tc)
	require.NoError(t, err)
	doc3, err := addDoc(t, "alpha", "", "3", tc)
	require.NoError(t, err)

	err = tc.AddAndConsumeDocuments(doc1, doc2, doc3)
	require.NoError(t, err)

	t.Run("default query parser keeps regex disabled", func(t *testing.T) {
		result, err := tc.SearchQueryParser("title:/thr.*/", 10, false)
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "Regex queries are not allowed")
	})

	t.Run("regex disabled", func(t *testing.T) {
		result, err := tc.SearchQueryParser("title:/thr.*/", 10, false)
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "Regex queries are not allowed")
	})

	t.Run("regex enabled", func(t *testing.T) {
		result, err := tc.SearchQueryParser("title:/thr.*/", 10, false, tantivy_go.WithRegexesEnabled())
		require.NoError(t, err)
		defer result.Free()

		size, err := result.GetSize()
		require.NoError(t, err)
		require.Equal(t, 2, int(size))

		results, err := tantivy_go.GetSearchResults(result, tc, func(jsonStr string) (string, error) {
			var data struct {
				Title string `json:"title"`
			}
			if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
				return "", err
			}
			return data.Title, nil
		}, NameTitle)
		require.NoError(t, err)

		titles := map[string]bool{}
		for _, title := range results {
			titles[title] = true
		}

		require.True(t, titles["three foxes"])
		require.True(t, titles["throne room"])
	})
}
