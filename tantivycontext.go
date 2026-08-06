package tantivy_go

// #include "bindings.h"
import "C"
import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

type TantivyContext struct {
	ptr    *C.TantivyContext
	schema *Schema
	mu     sync.Mutex
}

type queryParserConfig struct {
	allowRegexes bool
}

// QueryParserOption configures SearchQueryParser behavior.
type QueryParserOption func(*queryParserConfig)

// WithRegexesEnabled allows regex syntax in query parser expressions.
func WithRegexesEnabled() QueryParserOption {
	return func(cfg *queryParserConfig) {
		cfg.allowRegexes = true
	}
}

func (tc *TantivyContext) lockNative() (*C.TantivyContext, func(), error) {
	if tc == nil {
		return nil, nil, ErrClosedContext
	}
	tc.mu.Lock()
	if tc.ptr == nil {
		tc.mu.Unlock()
		return nil, nil, ErrClosedContext
	}
	return tc.ptr, tc.mu.Unlock, nil
}

// NewTantivyContextWithSchema creates a new instance of TantivyContext with the provided schema.
//
// Parameters:
//   - path: The path to the index as a string.
//   - schema: A pointer to the Schema to be used.
//
// Returns:
//   - *TantivyContext: A pointer to a newly created TantivyContext instance.
//   - error: An error if the index creation fails.
func NewTantivyContextWithSchema(path string, schema *Schema) (*TantivyContext, error) {
	if err := schema.ensureOpen(); err != nil {
		return nil, err
	}

	cPath, freePath := newCString(path)
	defer freePath()

	var errBuffer *C.char
	ptr := C.context_create_with_schema(cPath, schema.ptr, &errBuffer)
	if ptr == nil {
		if err := tryExtractError(errBuffer); err != nil {
			return nil, err
		}
		return nil, errors.New("failed to create tantivy context")
	}
	return &TantivyContext{ptr: ptr, schema: schema}, nil
}

// AddAndConsumeDocuments adds and consumes the provided documents to the index.
//
// Parameters:
//   - docs: A variadic parameter of pointers to Document to be added and consumed.
//
// Returns:
//   - error: An error if adding and consuming the documents fails.
func (tc *TantivyContext) AddAndConsumeDocuments(docs ...*Document) error {
	_, err := tc.AddAndConsumeDocumentsWithOpstamp(docs...)
	return err
}

// AddAndConsumeDocumentsWithOpstamp adds and consumes the provided documents to the index and returns the commit opstamp.
//
// Parameters:
//   - docs: A variadic parameter of pointers to Document to be added and consumed.
//
// Returns:
//   - uint64: The opstamp from the commit operation. Returns 0 if no documents are provided.
//   - error: An error if adding and consuming the documents fails.
func (tc *TantivyContext) AddAndConsumeDocumentsWithOpstamp(docs ...*Document) (uint64, error) {
	if len(docs) == 0 {
		return 0, nil
	}

	docsPtr := make([]*C.Document, len(docs))
	for j, doc := range docs {
		if err := doc.ensureOpen(); err != nil {
			return 0, err
		}
		docsPtr[j] = doc.ptr
	}

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return 0, err
	}
	defer unlock()

	var errBuffer *C.char
	opstamp := C.context_add_and_consume_documents(ptr, &docsPtr[0], C.uintptr_t(len(docs)), &errBuffer)
	for _, doc := range docs {
		doc.markConsumed()
	}
	if err := tryExtractError(errBuffer); err != nil {
		return 0, err
	}

	return uint64(opstamp), nil
}

// DeleteDocuments deletes documents from the index based on the specified field and IDs.
//
// Parameters:
//   - fieldName: The field name to match against the document IDs.
//   - deleteIds: A variadic parameter of document IDs to be deleted.
//
// Returns:
//   - error: An error if deleting the documents fails.
func (tc *TantivyContext) DeleteDocuments(fieldName string, deleteIds ...string) error {
	_, err := tc.DeleteDocumentsWithOpstamp(fieldName, deleteIds...)
	return err
}

// DeleteDocumentsWithOpstamp deletes documents from the index based on the specified field and IDs and returns the commit opstamp.
//
// Parameters:
//   - fieldName: The field name to match against the document IDs.
//   - deleteIds: A variadic parameter of document IDs to be deleted.
//
// Returns:
//   - uint64: The opstamp from the delete operation. Returns 0 if no IDs are provided.
//   - error: An error if deleting the documents fails.
func (tc *TantivyContext) DeleteDocumentsWithOpstamp(fieldName string, deleteIds ...string) (uint64, error) {
	if len(deleteIds) == 0 {
		return 0, nil
	}
	if tc == nil || tc.schema == nil {
		return 0, ErrClosedContext
	}
	fieldID, contains := tc.schema.fieldNames[fieldName]
	if !contains {
		return 0, errors.New("field not found in schema")
	}

	deleteIDsPtr := make([]*C.char, len(deleteIds))
	for j, id := range deleteIds {
		cID, freeID := newCString(id)
		defer freeID()
		deleteIDsPtr[j] = cID
	}

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return 0, err
	}
	defer unlock()

	var errBuffer *C.char
	opstamp := C.context_delete_documents(ptr, C.uint(fieldID), (**C.char)(unsafe.Pointer(&deleteIDsPtr[0])), C.uintptr_t(len(deleteIds)), &errBuffer)
	if err := tryExtractError(errBuffer); err != nil {
		return 0, err
	}

	return uint64(opstamp), nil
}

// BatchAddAndDeleteDocumentsWithOpstamp performs batch add and delete operations within a single commit.
// This is more efficient than calling AddAndConsumeDocumentsWithOpstamp and DeleteDocumentsWithOpstamp
// separately as it only commits once, reducing I/O overhead.
//
// Important: To update an existing document, you must include its field value in deleteFieldValues.
// Otherwise, the new document will be added without removing the old one, creating duplicates.
// The delete operation happens first, then the add operation.
//
// Parameters:
//   - addDocs: Documents to add to the index.
//   - deleteFieldName: The field name to match against for deletion.
//   - deleteFieldValues: Field values to delete from the index (documents where deleteFieldName matches these values).
//
// Returns:
//   - uint64: The opstamp from the commit operation. Returns 0 if both addDocs and deleteFieldValues are empty.
//   - error: An error if the batch operation fails.
func (tc *TantivyContext) BatchAddAndDeleteDocumentsWithOpstamp(addDocs []*Document, deleteFieldName string, deleteFieldValues []string) (uint64, error) {
	if len(addDocs) == 0 && len(deleteFieldValues) == 0 {
		return 0, nil
	}
	if tc == nil || tc.schema == nil {
		return 0, ErrClosedContext
	}

	var addDocsPtr **C.Document
	var addDocsLen C.uintptr_t
	if len(addDocs) > 0 {
		docsPtr := make([]*C.Document, len(addDocs))
		for j, doc := range addDocs {
			if err := doc.ensureOpen(); err != nil {
				return 0, err
			}
			docsPtr[j] = doc.ptr
		}
		addDocsPtr = &docsPtr[0]
		addDocsLen = C.uintptr_t(len(addDocs))
	}

	var deleteFieldID C.uint
	var deleteValuesPtr **C.char
	var deleteValuesLen C.uintptr_t
	if len(deleteFieldValues) > 0 {
		fieldID, contains := tc.schema.fieldNames[deleteFieldName]
		if !contains {
			return 0, errors.New("field not found in schema")
		}
		deleteFieldID = C.uint(fieldID)

		deleteValuesCPtr := make([]*C.char, len(deleteFieldValues))
		for j, value := range deleteFieldValues {
			cValue, freeValue := newCString(value)
			defer freeValue()
			deleteValuesCPtr[j] = cValue
		}
		deleteValuesPtr = (**C.char)(unsafe.Pointer(&deleteValuesCPtr[0]))
		deleteValuesLen = C.uintptr_t(len(deleteFieldValues))
	}

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return 0, err
	}
	defer unlock()

	var errBuffer *C.char
	opstamp := C.context_batch_add_and_delete_documents(
		ptr,
		addDocsPtr,
		addDocsLen,
		deleteFieldID,
		deleteValuesPtr,
		deleteValuesLen,
		&errBuffer,
	)
	for _, doc := range addDocs {
		doc.markConsumed()
	}
	if err := tryExtractError(errBuffer); err != nil {
		return 0, err
	}

	return uint64(opstamp), nil
}

// NumDocs returns the number of documents in the index.
//
// Returns:
//   - uint64: The number of documents.
//   - error: An error if retrieving the document count fails.
func (tc *TantivyContext) NumDocs() (uint64, error) {
	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return 0, err
	}
	defer unlock()

	var errBuffer *C.char
	numDocs := C.context_num_docs(ptr, &errBuffer)
	if err := tryExtractError(errBuffer); err != nil {
		return 0, err
	}
	return uint64(numDocs), nil
}

func validateSearchContext(sCtx SearchContext) error {
	if sCtx == nil {
		return errors.New("search context is nil")
	}
	if sCtx.GetDocsLimit() == 0 {
		return ErrInvalidDocsLimit
	}
	return nil
}

func cFieldWeights(fieldNames []string, weights []float32) ([]C.float, error) {
	if len(weights) != len(fieldNames) {
		return nil, fmt.Errorf("fieldNames and weights length mismatch")
	}
	fieldWeights := make([]C.float, len(fieldNames))
	for i, weight := range weights {
		fieldWeights[i] = C.float(weight)
	}
	return fieldWeights, nil
}

// Search performs a search query on the index and returns the search results.
//
// Parameters:
//   - sCtx (SearchContext): The context for the search, containing query string,
//     document limit, highlight option, and field weights.
//
// Returns:
//   - *SearchResult: A pointer to the SearchResult containing the search results.
//   - error: An error if the search fails.
func (tc *TantivyContext) Search(sCtx SearchContext) (*SearchResult, error) {
	if err := validateSearchContext(sCtx); err != nil {
		return nil, err
	}
	fieldNames, weights := sCtx.GetFieldAndWeights()
	if len(fieldNames) == 0 {
		return nil, fmt.Errorf("fieldNames must not be empty")
	}
	fieldNamesPtr, err := tc.extractFields(fieldNames)
	if err != nil {
		return nil, err
	}
	fieldWeightsPtr, err := cFieldWeights(fieldNames, weights)
	if err != nil {
		return nil, err
	}
	cQuery, freeQuery := newCString(sCtx.GetQuery())
	defer freeQuery()

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	resultPtr := C.context_search(
		ptr,
		(*C.uint)(unsafe.Pointer(&fieldNamesPtr[0])),
		(*C.float)(unsafe.Pointer(&fieldWeightsPtr[0])),
		C.uintptr_t(len(fieldNames)),
		cQuery,
		&errBuffer,
		pointerCType(sCtx.GetDocsLimit()),
		C.bool(sCtx.WithHighlights()),
	)
	if resultPtr == nil {
		if err := tryExtractError(errBuffer); err != nil {
			return nil, err
		}
		return nil, errors.New("search result is nil")
	}

	return &SearchResult{ptr: resultPtr}, nil
}

// SearchJSON performs a simplified search query on the index and returns the search results.
//
// Parameters:
//   - sCtx (SearchContext): The context for the search, containing query string,
//     document limit, and highlight option.
//
// Returns:
//   - *SearchResult: A pointer to the SearchResult containing the search results.
//   - error: An error if the search fails.
func (tc *TantivyContext) SearchJSON(sCtx SearchContext) (*SearchResult, error) {
	if err := validateSearchContext(sCtx); err != nil {
		return nil, err
	}
	cQuery, freeQuery := newCString(sCtx.GetQuery())
	defer freeQuery()

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	resultPtr := C.context_search_json(
		ptr,
		cQuery,
		&errBuffer,
		pointerCType(sCtx.GetDocsLimit()),
		C.bool(sCtx.WithHighlights()),
	)
	if resultPtr == nil {
		if err := tryExtractError(errBuffer); err != nil {
			return nil, err
		}
		return nil, errors.New("search result is nil")
	}

	return &SearchResult{ptr: resultPtr}, nil
}

// SearchQueryParser performs a search using tantivy's query parser syntax.
// Supports range queries (e.g., "price:[10 TO 100]"), fuzzy queries, and wildcards.
//
// Parameters:
//   - query: The query string in tantivy query parser syntax
//   - docsLimit: Maximum number of documents to return
//   - withHighlights: Whether to include highlighted snippets
//   - opts: Optional parser configuration options (e.g., WithRegexesEnabled)
//
// Returns:
//   - *SearchResult: A pointer to the SearchResult containing the search results.
//   - error: An error if the search fails.
func (tc *TantivyContext) SearchQueryParser(query string, docsLimit int, withHighlights bool, opts ...QueryParserOption) (*SearchResult, error) {
	if docsLimit <= 0 {
		return nil, ErrInvalidDocsLimit
	}

	cfg := queryParserConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	cQuery, freeQuery := newCString(query)
	defer freeQuery()

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	resultPtr := C.context_search_query_parser(
		ptr,
		cQuery,
		pointerCType(docsLimit),
		C.bool(withHighlights),
		C.bool(cfg.allowRegexes),
		&errBuffer,
	)
	if resultPtr == nil {
		if err := tryExtractError(errBuffer); err != nil {
			return nil, err
		}
		return nil, errors.New("search result is nil")
	}

	return &SearchResult{ptr: resultPtr}, nil
}

// Close waits till the merging operations are finished and releases all the resources held by the indexWriter.
func (tc *TantivyContext) Close() error {
	if tc == nil {
		return nil
	}

	tc.mu.Lock()
	if tc.ptr == nil {
		tc.mu.Unlock()
		return nil
	}
	ptr := tc.ptr
	tc.ptr = nil
	tc.mu.Unlock()

	var errBuffer *C.char
	C.context_wait_and_free(ptr, &errBuffer)
	return tryExtractError(errBuffer)
}

// Deprecated: Use Close() instead.
func (tc *TantivyContext) Free() {
	if err := tc.Close(); err != nil {
		fmt.Println("Failed to wait for merging threads: ", err)
	}
}

// RegisterTextAnalyzerNgram registers a text analyzer using N-grams with the index.
//
// Parameters:
//   - tokenizerName (string): The name of the tokenizer to be used.
//   - minGram (uintptr): The minimum length of the n-grams.
//   - maxGram (uintptr): The maximum length of the n-grams.
//   - prefixOnly (bool): Whether to generate only prefix n-grams.
//
// Returns:
//   - error: An error if the registration fails.
func (tc *TantivyContext) RegisterTextAnalyzerNgram(tokenizerName string, minGram, maxGram uintptr, prefixOnly bool) error {
	cTokenizerName, freeTokenizerName := newCString(tokenizerName)
	defer freeTokenizerName()

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return err
	}
	defer unlock()

	var errBuffer *C.char
	C.context_register_text_analyzer_ngram(ptr, cTokenizerName, C.uintptr_t(minGram), C.uintptr_t(maxGram), C.bool(prefixOnly), &errBuffer)
	return tryExtractError(errBuffer)
}

// RegisterTextAnalyzerEdgeNgram registers a text analyzer using edge n-grams with the index.
//
// Parameters:
//   - tokenizerName (string): The name of the tokenizer to be used.
//   - minGram (uintptr): The minimum length of the edge n-grams.
//   - maxGram (uintptr): The maximum length of the edge n-grams.
//   - limit (uintptr): The maximum number of edge n-grams to generate.
//
// Returns:
//   - error: An error if the registration fails.
func (tc *TantivyContext) RegisterTextAnalyzerEdgeNgram(tokenizerName string, minGram, maxGram uintptr, limit uintptr) error {
	cTokenizerName, freeTokenizerName := newCString(tokenizerName)
	defer freeTokenizerName()

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return err
	}
	defer unlock()

	var errBuffer *C.char
	C.context_register_text_analyzer_edge_ngram(ptr, cTokenizerName, C.uintptr_t(minGram), C.uintptr_t(maxGram), C.uintptr_t(limit), &errBuffer)
	return tryExtractError(errBuffer)
}

// RegisterTextAnalyzerSimple registers a simple text analyzer with the index.
//
// Parameters:
//   - tokenizerName (string): The name of the simple tokenizer to be used.
//   - textLimit (uintptr): The limit on the length of the text to be analyzed.
//   - lang (Language): The language code for the text analyzer.
//
// Returns:
//   - error: An error if the registration fails.
func (tc *TantivyContext) RegisterTextAnalyzerSimple(tokenizerName string, textLimit uintptr, lang Language) error {
	cTokenizerName, freeTokenizerName := newCString(tokenizerName)
	defer freeTokenizerName()
	cLang, freeLang := newCString(string(lang))
	defer freeLang()

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return err
	}
	defer unlock()

	var errBuffer *C.char
	C.context_register_text_analyzer_simple(ptr, cTokenizerName, C.uintptr_t(textLimit), cLang, &errBuffer)
	return tryExtractError(errBuffer)
}

// RegisterTextAnalyzerRaw registers a raw text analyzer with the index.
//
// Parameters:
//   - tokenizerName (string): The name of the raw tokenizer to be used.
//
// Returns:
//   - error: An error if the registration fails.
func (tc *TantivyContext) RegisterTextAnalyzerRaw(tokenizerName string) error {
	cTokenizerName, freeTokenizerName := newCString(tokenizerName)
	defer freeTokenizerName()

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return err
	}
	defer unlock()

	var errBuffer *C.char
	C.context_register_text_analyzer_raw(ptr, cTokenizerName, &errBuffer)
	return tryExtractError(errBuffer)
}

// GetSearchResults extracts search results from a SearchResult and converts them into a slice of models.
//
// Parameters:
//   - searchResult (*SearchResult): The search results to process.
//   - schema (*Schema): The schema to use for converting documents to models.
//   - f (func(json string) (T, error)): A function to convert JSON strings to models.
//   - includeFields (...string): Optional list of fields to include in the result.
//
// Returns:
//   - ([]T, error): A slice of models obtained from the search results, and an error if something goes wrong.
func GetSearchResults[T any](
	searchResult *SearchResult,
	tc *TantivyContext,
	f func(json string) (T, error),
	includeFields ...string,
) ([]T, error) {
	defer searchResult.Free()

	size, err := searchResult.GetSize()
	if err != nil {
		return nil, err
	}

	models := make([]T, 0, size)
	for next := uint64(0); next < size; next++ {
		doc, err := searchResult.Get(next)
		if err != nil {
			return nil, err
		}

		model, err := ToModel(doc, tc, includeFields, f)
		doc.Free()
		if err != nil {
			return nil, err
		}

		models = append(models, model)
	}
	return models, nil
}

func (tc *TantivyContext) extractFields(fieldNames []string) ([]C.uint, error) {
	if len(fieldNames) == 0 {
		return nil, errors.New("field names is empty")
	}
	if tc == nil || tc.schema == nil {
		return nil, ErrClosedContext
	}

	includeFieldsPtr := make([]C.uint, len(fieldNames))
	for i, fieldName := range fieldNames {
		fieldID, contains := tc.schema.fieldNames[fieldName]
		if !contains {
			return nil, errors.New("field not found in schema")
		}
		includeFieldsPtr[i] = C.uint(fieldID)
	}
	return includeFieldsPtr, nil
}

func (tc *TantivyContext) extractFieldsOrAll(fieldNames []string) ([]C.uint, error) {
	if tc == nil || tc.schema == nil {
		return nil, ErrClosedContext
	}
	if len(fieldNames) == 0 {
		includeFieldsPtr := make([]C.uint, 0, len(tc.schema.fieldNames))
		for _, fieldID := range tc.schema.fieldNames {
			includeFieldsPtr = append(includeFieldsPtr, C.uint(fieldID))
		}
		if len(includeFieldsPtr) == 0 {
			return nil, errors.New("schema has no fields")
		}
		return includeFieldsPtr, nil
	}
	return tc.extractFields(fieldNames)
}

// CommitOpstamp gets the opstamp of the last commit.
//
// Note: Due to a bug in Tantivy (https://github.com/quickwit-oss/tantivy/issues/2666),
// this returns the INITIAL commit opstamp, not the latest one. The value is only
// updated after the index is closed and reopened. During an active session, this
// will return 0 for a new index or the opstamp from when the index was opened.
func (tc *TantivyContext) CommitOpstamp() uint64 {
	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return 0
	}
	defer unlock()
	return uint64(C.context_commit_opstamp(ptr))
}

// ReloadReader forces the index reader to reload and check for new commits.
//
// Note: This method is called automatically during search operations (Search, SearchJSON, NumDocs),
// so manual calls are typically not necessary. The reader uses ReloadPolicy::Manual internally,
// but reloading happens automatically when needed.
//
// Returns:
//   - error: An error if reloading the reader fails.
func (tc *TantivyContext) ReloadReader() error {
	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return err
	}
	defer unlock()

	var errBuffer *C.char
	C.context_reload_reader(ptr, &errBuffer)
	return tryExtractError(errBuffer)
}

// GarbageCollectFiles performs garbage collection on unused index files.
// This method removes files that were created by tantivy and are no longer
// used by any segment.
//
// Returns:
//   - uint64: The number of files that were deleted.
//   - error: An error if garbage collection fails.
func (tc *TantivyContext) GarbageCollectFiles() (uint64, error) {
	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return 0, err
	}
	defer unlock()

	var errBuffer *C.char
	deletedCount := C.context_garbage_collect_files(ptr, &errBuffer)
	if err := tryExtractError(errBuffer); err != nil {
		return 0, err
	}
	return uint64(deletedCount), nil
}
