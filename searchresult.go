package tantivy_go

//#include "bindings.h"
import "C"
import "errors"

type SearchResult struct{ ptr *C.SearchResult }

func (r *SearchResult) ensureOpen() error {
	if r == nil || r.ptr == nil {
		return errors.New("search result is closed")
	}
	return nil
}

// Get retrieves a document from the search result at the specified index.
//
// Parameters:
// - index: The index of the document to retrieve.
//
// Returns:
// - A pointer to the Document if successful, or nil if not found.
// - An error if there was an issue retrieving the document.
func (r *SearchResult) Get(index uint64) (*Document, error) {
	if err := r.ensureOpen(); err != nil {
		return nil, err
	}
	var errBuffer *C.char
	ptr := C.search_result_get_doc(r.ptr, C.uintptr_t(index), &errBuffer)
	if ptr == nil {
		if err := tryExtractError(errBuffer); err != nil {
			return nil, err
		}
		return nil, errors.New("search result document is nil")
	}
	return &Document{ptr: ptr}, nil
}

// GetSize returns the number of documents in the search result.
//
// Returns:
// - The size of the search result if successful.
// - An error if there was an issue getting the size.
func (r *SearchResult) GetSize() (uint64, error) {
	if err := r.ensureOpen(); err != nil {
		return 0, err
	}
	var errBuffer *C.char
	size := C.search_result_get_size(r.ptr, &errBuffer)
	if err := tryExtractError(errBuffer); err != nil {
		return 0, err
	}
	return uint64(size), nil
}

func (r *SearchResult) Free() {
	if r == nil || r.ptr == nil {
		return
	}
	C.search_result_free(r.ptr)
	r.ptr = nil
}
