package tantivy_go

//#include "bindings.h"
import "C"
import (
	"errors"
	"fmt"
	"unsafe"
)

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

// HasMore reports whether the sorted search retained an additional matching hit.
func (r *SearchResult) HasMore() (bool, error) {
	if err := r.ensureOpen(); err != nil {
		return false, err
	}
	var errBuffer *C.char
	hasMore := C.search_result_get_has_more(r.ptr, &errBuffer)
	if err := tryExtractError(errBuffer); err != nil {
		return false, err
	}
	return bool(hasMore), nil
}

// SortValues returns the native sort tuple for one sorted-search result.
func (r *SearchResult) SortValues(index uint64) ([]SortValue, error) {
	if err := r.ensureOpen(); err != nil {
		return nil, err
	}

	var errBuffer *C.char
	count := C.search_result_get_sort_values_len(r.ptr, C.uintptr_t(index), &errBuffer)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}
	maxInt := uint64(^uint(0) >> 1)
	if uint64(count) > maxInt {
		return nil, errors.New("sorted search tuple length exceeds Go slice capacity")
	}
	values := make([]C.SortedSearchValue, int(count))
	if len(values) == 0 {
		return []SortValue{}, nil
	}

	C.search_result_copy_sort_values(
		r.ptr,
		C.uintptr_t(index),
		&values[0],
		C.uintptr_t(len(values)),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}

	result := make([]SortValue, len(values))
	for i := range values {
		value, err := sortedSearchValueFromC(values[i])
		if err != nil {
			return nil, fmt.Errorf("decode sorted search value %d: %w", i, err)
		}
		result[i] = value
	}
	return result, nil
}

func sortedSearchValueFromC(value C.SortedSearchValue) (SortValue, error) {
	kind := SortValueKind(value.kind)
	if kind < SortValueText || kind > SortValueDate {
		return SortValue{}, errors.New("native sorted search value has an invalid kind")
	}

	result := SortValue{Kind: kind, Missing: bool(value.missing)}
	if result.Missing {
		return result, nil
	}
	switch kind {
	case SortValueText:
		textLen := uint64(value.text_len)
		if textLen > uint64(^uint(0)>>1) {
			return SortValue{}, errors.New("native sorted search text exceeds Go string capacity")
		}
		if textLen == 0 {
			return result, nil
		}
		if value.text_ptr == nil {
			return SortValue{}, errors.New("native sorted search text pointer is nil")
		}
		// The native SearchResult owns this buffer through Free; copy it before returning.
		result.Text = string(unsafe.Slice((*byte)(unsafe.Pointer(value.text_ptr)), int(textLen)))
	case SortValueU64:
		result.U64 = uint64(value.u64_value)
	case SortValueI64, SortValueDate:
		result.I64 = int64(value.i64_value)
	case SortValueF64:
		result.F64 = float64(value.f64_value)
	case SortValueBool:
		result.Bool = bool(value.bool_value)
	}
	return result, nil
}

func (r *SearchResult) Free() {
	if r == nil || r.ptr == nil {
		return
	}
	C.search_result_free(r.ptr)
	r.ptr = nil
}
