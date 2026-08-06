package tantivy_go

// #include "bindings.h"
import "C"

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unsafe"
)

// SortDirection controls the ordering of one sorted-search field.
type SortDirection uint8

const (
	SortAscending SortDirection = iota + 1
	SortDescending
)

// SortValueKind identifies the populated member of a SortValue.
type SortValueKind uint8

const (
	SortValueText SortValueKind = iota + 1
	SortValueU64
	SortValueI64
	SortValueF64
	SortValueBool
	SortValueDate
)

// SortField describes one fast field in a sorted search tuple.
type SortField struct {
	Name      string
	Direction SortDirection
}

// SortValue is one atom in a sorted-search cursor or returned sort tuple.
// Date values use I64 as Unix milliseconds.
type SortValue struct {
	Kind    SortValueKind
	Missing bool
	Text    string
	U64     uint64
	I64     int64
	F64     float64
	Bool    bool
}

// SortedSearchRequest performs a filter-only JSON search ordered by Sort.
type SortedSearchRequest struct {
	QueryJSON string
	Limit     int
	Sort      []SortField
	After     []SortValue
	Timeout   time.Duration
}

const (
	nativeSortedSearchTimeout = "tantivy-go sorted search deadline exceeded"
	maxSortedSearchLimit      = 10_000
)

func validateSortedSearchRequest(request SortedSearchRequest) error {
	if request.Limit <= 0 {
		return fmt.Errorf("sorted search limit: %w", ErrInvalidDocsLimit)
	}
	if request.Limit > maxSortedSearchLimit {
		return fmt.Errorf("sorted search limit must not exceed %d", maxSortedSearchLimit)
	}
	if request.Timeout <= 0 {
		return errors.New("sorted search timeout must be greater than zero")
	}
	if strings.IndexByte(request.QueryJSON, 0) >= 0 {
		return errors.New("sorted search query JSON contains a NUL byte")
	}
	if len(request.Sort) == 0 || len(request.Sort) > 4 {
		return errors.New("sorted search requires between one and four sort fields")
	}
	if len(request.After) != 0 && len(request.After) != len(request.Sort) {
		return errors.New("sorted search after tuple length must match sort fields")
	}

	for index, field := range request.Sort {
		if field.Name == "" {
			return fmt.Errorf("sorted search field %d has an empty name", index)
		}
		if strings.IndexByte(field.Name, 0) >= 0 {
			return fmt.Errorf("sorted search field %d contains a NUL byte", index)
		}
		if field.Direction != SortAscending && field.Direction != SortDescending {
			return fmt.Errorf("sorted search field %d has an invalid direction", index)
		}
	}
	for index, value := range request.After {
		if err := validateSortValue(value); err != nil {
			return fmt.Errorf("sorted search after value %d: %w", index, err)
		}
	}
	return nil
}

func validateSortValue(value SortValue) error {
	if value.Kind < SortValueText || value.Kind > SortValueDate {
		return errors.New("has an invalid kind")
	}
	if value.Missing {
		if value.Text != "" || value.U64 != 0 || value.I64 != 0 || value.F64 != 0 || value.Bool {
			return errors.New("is missing and also has a union value")
		}
		return nil
	}

	switch value.Kind {
	case SortValueText:
		if value.U64 != 0 || value.I64 != 0 || value.F64 != 0 || value.Bool {
			return errors.New("has multiple union values")
		}
	case SortValueU64:
		if value.Text != "" || value.I64 != 0 || value.F64 != 0 || value.Bool {
			return errors.New("has multiple union values")
		}
	case SortValueI64, SortValueDate:
		if value.Text != "" || value.U64 != 0 || value.F64 != 0 || value.Bool {
			return errors.New("has multiple union values")
		}
	case SortValueF64:
		if math.IsNaN(value.F64) {
			return errors.New("cannot be NaN")
		}
		if value.Text != "" || value.U64 != 0 || value.I64 != 0 || value.Bool {
			return errors.New("has multiple union values")
		}
	case SortValueBool:
		if value.Text != "" || value.U64 != 0 || value.I64 != 0 || value.F64 != 0 {
			return errors.New("has multiple union values")
		}
	}
	return nil
}

type sortedSearchDescriptors struct {
	fields []C.SortedSearchField
	after  []C.SortedSearchValue
	free   func()
}

func newSortedSearchDescriptors(request SortedSearchRequest) (sortedSearchDescriptors, error) {
	allocated := make([]unsafe.Pointer, 0, len(request.Sort)+len(request.After))
	free := func() {
		for _, pointer := range allocated {
			C.free(pointer)
		}
	}
	fail := func(err error) (sortedSearchDescriptors, error) {
		free()
		return sortedSearchDescriptors{}, err
	}

	descriptors := sortedSearchDescriptors{
		fields: make([]C.SortedSearchField, len(request.Sort)),
		after:  make([]C.SortedSearchValue, len(request.After)),
		free:   free,
	}
	for index, field := range request.Sort {
		name := C.CString(field.Name)
		if name == nil {
			return fail(errors.New("allocate sorted search field name"))
		}
		allocated = append(allocated, unsafe.Pointer(name))
		descriptors.fields[index] = C.SortedSearchField{
			name_ptr:  name,
			direction: C.uint8_t(field.Direction),
		}
	}
	for index, value := range request.After {
		cValue := C.SortedSearchValue{
			kind:       C.uint8_t(value.Kind),
			missing:    C.bool(value.Missing),
			u64_value:  C.uint64_t(value.U64),
			i64_value:  C.int64_t(value.I64),
			f64_value:  C.double(value.F64),
			bool_value: C.bool(value.Bool),
		}
		if value.Kind == SortValueText && !value.Missing && value.Text != "" {
			text := C.CString(value.Text)
			if text == nil {
				return fail(errors.New("allocate sorted search text cursor"))
			}
			allocated = append(allocated, unsafe.Pointer(text))
			cValue.text_ptr = text
			cValue.text_len = C.uintptr_t(len(value.Text))
		}
		descriptors.after[index] = cValue
	}
	return descriptors, nil
}

// SearchJSONSorted runs a JSON query without relevance scoring and returns at
// most Limit documents ordered by the requested fast-field tuple.
func (tc *TantivyContext) SearchJSONSorted(request SortedSearchRequest) (*SearchResult, error) {
	if err := validateSortedSearchRequest(request); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(request.Timeout)

	descriptors, err := newSortedSearchDescriptors(request)
	if err != nil {
		return nil, err
	}
	defer descriptors.free()

	query, freeQuery := newCString(request.QueryJSON)
	defer freeQuery()

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	result := C.context_search_json_sorted(
		ptr,
		query,
		&descriptors.fields[0],
		C.uintptr_t(len(descriptors.fields)),
		cSortedSearchAfterPointer(descriptors.after),
		C.uintptr_t(len(descriptors.after)),
		C.uintptr_t(request.Limit),
		C.int64_t(deadline.Unix()),
		C.uint32_t(deadline.Nanosecond()),
		&errBuffer,
	)
	if result == nil {
		if nativeErr := tryExtractError(errBuffer); nativeErr != nil {
			if nativeErr.Error() == nativeSortedSearchTimeout {
				return nil, ErrSearchTimeout
			}
			return nil, fmt.Errorf("search JSON sorted: %w", nativeErr)
		}
		return nil, errors.New("search JSON sorted result is nil")
	}
	return &SearchResult{ptr: result}, nil
}

func cSortedSearchAfterPointer(values []C.SortedSearchValue) *C.SortedSearchValue {
	if len(values) == 0 {
		return nil
	}
	return &values[0]
}
