package tantivy_go

// #include "bindings.h"
import "C"
import (
	"errors"
	"fmt"
	"time"
	"unsafe"
)

// FastFieldResult holds the results of a fast field search.
type FastFieldResult struct {
	Values []string
	Scores []float32
}

// TypedFastFieldResult holds typed fast-field values for matched documents.
// Valid[i] is false when document i matched the query but has no value for the requested fast field.
type TypedFastFieldResult[T any] struct {
	Values []T
	Valid  []bool
	Scores []float32
}

func (tc *TantivyContext) prepareFastFieldSearch(sCtx SearchContext, fastFieldName string, requireQueryFields bool) ([]C.uint, []C.float, C.uint, uintptr, *C.char, func(), error) {
	if sCtx == nil {
		return nil, nil, 0, 0, nil, nil, errors.New("search context is nil")
	}
	if tc == nil || tc.schema == nil {
		return nil, nil, 0, 0, nil, nil, ErrClosedContext
	}

	docsLimit := sCtx.GetDocsLimit()
	if docsLimit == 0 {
		return nil, nil, 0, 0, nil, nil, ErrInvalidDocsLimit
	}

	fastFieldID, contains := tc.schema.fieldNames[fastFieldName]
	if !contains {
		return nil, nil, 0, 0, nil, nil, errors.New("fast field not found in schema")
	}

	var fieldIDs []C.uint
	var fieldWeights []C.float
	if requireQueryFields {
		fieldNames, weights := sCtx.GetFieldAndWeights()
		if len(fieldNames) == 0 {
			return nil, nil, 0, 0, nil, nil, fmt.Errorf("fieldNames must not be empty")
		}
		ids, err := tc.extractFields(fieldNames)
		if err != nil {
			return nil, nil, 0, 0, nil, nil, err
		}
		fieldIDs = ids
		fieldWeights, err = cFieldWeights(fieldNames, weights)
		if err != nil {
			return nil, nil, 0, 0, nil, nil, err
		}
	}

	cQuery, freeQuery := newCString(sCtx.GetQuery())
	return fieldIDs, fieldWeights, C.uint(fastFieldID), docsLimit, cQuery, freeQuery, nil
}

func emptyFastFieldResult() *FastFieldResult {
	return &FastFieldResult{Values: []string{}, Scores: []float32{}}
}

// SearchFastField performs a search returning only fast field values without loading full documents.
// The field must be configured with isFast=true in the schema.
func (tc *TantivyContext) SearchFastField(sCtx SearchContext, fastFieldName string) (*FastFieldResult, error) {
	fieldIDs, fieldWeights, fastFieldID, docsLimit, cQuery, freeQuery, err := tc.prepareFastFieldSearch(sCtx, fastFieldName, true)
	if err != nil {
		return nil, err
	}
	defer freeQuery()

	outScores := make([]C.float, docsLimit)
	outValues := make([]*C.char, docsLimit)

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	count := C.context_search_fast_field(
		ptr,
		(*C.uint)(unsafe.Pointer(&fieldIDs[0])),
		(*C.float)(unsafe.Pointer(&fieldWeights[0])),
		C.uintptr_t(len(fieldIDs)),
		cQuery,
		fastFieldID,
		pointerCType(docsLimit),
		(*C.float)(unsafe.Pointer(&outScores[0])),
		(**C.char)(unsafe.Pointer(&outValues[0])),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}
	if count == 0 {
		return emptyFastFieldResult(), nil
	}
	defer C.fast_field_values_free((**C.char)(unsafe.Pointer(&outValues[0])), C.uintptr_t(count))

	result := &FastFieldResult{
		Values: make([]string, count),
		Scores: make([]float32, count),
	}
	for i := 0; i < int(count); i++ {
		result.Scores[i] = float32(outScores[i])
		if outValues[i] != nil {
			result.Values[i] = C.GoString(outValues[i])
		}
	}
	return result, nil
}

// SearchFastFieldJson performs a search using JSON query returning only fast field values.
// The field must be configured with isFast=true in the schema.
// Use this with AllQuery or other JSON-based queries.
func (tc *TantivyContext) SearchFastFieldJson(sCtx SearchContext, fastFieldName string) (*FastFieldResult, error) {
	_, _, fastFieldID, docsLimit, cQuery, freeQuery, err := tc.prepareFastFieldSearch(sCtx, fastFieldName, false)
	if err != nil {
		return nil, err
	}
	defer freeQuery()

	outScores := make([]C.float, docsLimit)
	outValues := make([]*C.char, docsLimit)

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	count := C.context_search_fast_field_json(
		ptr,
		cQuery,
		fastFieldID,
		pointerCType(docsLimit),
		(*C.float)(unsafe.Pointer(&outScores[0])),
		(**C.char)(unsafe.Pointer(&outValues[0])),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}
	if count == 0 {
		return emptyFastFieldResult(), nil
	}
	defer C.fast_field_values_free((**C.char)(unsafe.Pointer(&outValues[0])), C.uintptr_t(count))

	result := &FastFieldResult{
		Values: make([]string, count),
		Scores: make([]float32, count),
	}
	for i := 0; i < int(count); i++ {
		result.Scores[i] = float32(outScores[i])
		if outValues[i] != nil {
			result.Values[i] = C.GoString(outValues[i])
		}
	}
	return result, nil
}

func buildTypedFastFieldResult[T any, V any](count C.uintptr_t, outScores []C.float, outValues []V, outValid []C.bool, convert func(V) T) *TypedFastFieldResult[T] {
	result := &TypedFastFieldResult[T]{
		Values: make([]T, count),
		Valid:  make([]bool, count),
		Scores: make([]float32, count),
	}
	for i := 0; i < int(count); i++ {
		result.Valid[i] = bool(outValid[i])
		if result.Valid[i] {
			result.Values[i] = convert(outValues[i])
		}
		result.Scores[i] = float32(outScores[i])
	}
	return result
}

func (tc *TantivyContext) SearchFastFieldU64(sCtx SearchContext, fastFieldName string) (*TypedFastFieldResult[uint64], error) {
	fieldIDs, fieldWeights, fastFieldID, docsLimit, cQuery, freeQuery, err := tc.prepareFastFieldSearch(sCtx, fastFieldName, true)
	if err != nil {
		return nil, err
	}
	defer freeQuery()

	outScores := make([]C.float, docsLimit)
	outValues := make([]C.uint64_t, docsLimit)
	outValid := make([]C.bool, docsLimit)

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	count := C.context_search_fast_field_u64(
		ptr,
		(*C.uint)(unsafe.Pointer(&fieldIDs[0])),
		(*C.float)(unsafe.Pointer(&fieldWeights[0])),
		C.uintptr_t(len(fieldIDs)),
		cQuery,
		fastFieldID,
		pointerCType(docsLimit),
		(*C.float)(unsafe.Pointer(&outScores[0])),
		(*C.uint64_t)(unsafe.Pointer(&outValues[0])),
		(*C.bool)(unsafe.Pointer(&outValid[0])),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}
	return buildTypedFastFieldResult(count, outScores, outValues, outValid, func(v C.uint64_t) uint64 { return uint64(v) }), nil
}

func (tc *TantivyContext) SearchFastFieldI64(sCtx SearchContext, fastFieldName string) (*TypedFastFieldResult[int64], error) {
	fieldIDs, fieldWeights, fastFieldID, docsLimit, cQuery, freeQuery, err := tc.prepareFastFieldSearch(sCtx, fastFieldName, true)
	if err != nil {
		return nil, err
	}
	defer freeQuery()

	outScores := make([]C.float, docsLimit)
	outValues := make([]C.int64_t, docsLimit)
	outValid := make([]C.bool, docsLimit)

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	count := C.context_search_fast_field_i64(
		ptr,
		(*C.uint)(unsafe.Pointer(&fieldIDs[0])),
		(*C.float)(unsafe.Pointer(&fieldWeights[0])),
		C.uintptr_t(len(fieldIDs)),
		cQuery,
		fastFieldID,
		pointerCType(docsLimit),
		(*C.float)(unsafe.Pointer(&outScores[0])),
		(*C.int64_t)(unsafe.Pointer(&outValues[0])),
		(*C.bool)(unsafe.Pointer(&outValid[0])),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}
	return buildTypedFastFieldResult(count, outScores, outValues, outValid, func(v C.int64_t) int64 { return int64(v) }), nil
}

func (tc *TantivyContext) SearchFastFieldF64(sCtx SearchContext, fastFieldName string) (*TypedFastFieldResult[float64], error) {
	fieldIDs, fieldWeights, fastFieldID, docsLimit, cQuery, freeQuery, err := tc.prepareFastFieldSearch(sCtx, fastFieldName, true)
	if err != nil {
		return nil, err
	}
	defer freeQuery()

	outScores := make([]C.float, docsLimit)
	outValues := make([]C.double, docsLimit)
	outValid := make([]C.bool, docsLimit)

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	count := C.context_search_fast_field_f64(
		ptr,
		(*C.uint)(unsafe.Pointer(&fieldIDs[0])),
		(*C.float)(unsafe.Pointer(&fieldWeights[0])),
		C.uintptr_t(len(fieldIDs)),
		cQuery,
		fastFieldID,
		pointerCType(docsLimit),
		(*C.float)(unsafe.Pointer(&outScores[0])),
		(*C.double)(unsafe.Pointer(&outValues[0])),
		(*C.bool)(unsafe.Pointer(&outValid[0])),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}
	return buildTypedFastFieldResult(count, outScores, outValues, outValid, func(v C.double) float64 { return float64(v) }), nil
}

func (tc *TantivyContext) SearchFastFieldDate(sCtx SearchContext, fastFieldName string) (*TypedFastFieldResult[time.Time], error) {
	fieldIDs, fieldWeights, fastFieldID, docsLimit, cQuery, freeQuery, err := tc.prepareFastFieldSearch(sCtx, fastFieldName, true)
	if err != nil {
		return nil, err
	}
	defer freeQuery()

	outScores := make([]C.float, docsLimit)
	outValues := make([]C.int64_t, docsLimit)
	outValid := make([]C.bool, docsLimit)

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	count := C.context_search_fast_field_date(
		ptr,
		(*C.uint)(unsafe.Pointer(&fieldIDs[0])),
		(*C.float)(unsafe.Pointer(&fieldWeights[0])),
		C.uintptr_t(len(fieldIDs)),
		cQuery,
		fastFieldID,
		pointerCType(docsLimit),
		(*C.float)(unsafe.Pointer(&outScores[0])),
		(*C.int64_t)(unsafe.Pointer(&outValues[0])),
		(*C.bool)(unsafe.Pointer(&outValid[0])),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}
	return buildTypedFastFieldResult(count, outScores, outValues, outValid, func(v C.int64_t) time.Time { return time.UnixMilli(int64(v)).UTC() }), nil
}

func (tc *TantivyContext) SearchFastFieldU64Json(sCtx SearchContext, fastFieldName string) (*TypedFastFieldResult[uint64], error) {
	_, _, fastFieldID, docsLimit, cQuery, freeQuery, err := tc.prepareFastFieldSearch(sCtx, fastFieldName, false)
	if err != nil {
		return nil, err
	}
	defer freeQuery()

	outScores := make([]C.float, docsLimit)
	outValues := make([]C.uint64_t, docsLimit)
	outValid := make([]C.bool, docsLimit)

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	count := C.context_search_fast_field_u64_json(
		ptr,
		cQuery,
		fastFieldID,
		pointerCType(docsLimit),
		(*C.float)(unsafe.Pointer(&outScores[0])),
		(*C.uint64_t)(unsafe.Pointer(&outValues[0])),
		(*C.bool)(unsafe.Pointer(&outValid[0])),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}
	return buildTypedFastFieldResult(count, outScores, outValues, outValid, func(v C.uint64_t) uint64 { return uint64(v) }), nil
}

func (tc *TantivyContext) SearchFastFieldI64Json(sCtx SearchContext, fastFieldName string) (*TypedFastFieldResult[int64], error) {
	_, _, fastFieldID, docsLimit, cQuery, freeQuery, err := tc.prepareFastFieldSearch(sCtx, fastFieldName, false)
	if err != nil {
		return nil, err
	}
	defer freeQuery()

	outScores := make([]C.float, docsLimit)
	outValues := make([]C.int64_t, docsLimit)
	outValid := make([]C.bool, docsLimit)

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	count := C.context_search_fast_field_i64_json(
		ptr,
		cQuery,
		fastFieldID,
		pointerCType(docsLimit),
		(*C.float)(unsafe.Pointer(&outScores[0])),
		(*C.int64_t)(unsafe.Pointer(&outValues[0])),
		(*C.bool)(unsafe.Pointer(&outValid[0])),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}
	return buildTypedFastFieldResult(count, outScores, outValues, outValid, func(v C.int64_t) int64 { return int64(v) }), nil
}

func (tc *TantivyContext) SearchFastFieldF64Json(sCtx SearchContext, fastFieldName string) (*TypedFastFieldResult[float64], error) {
	_, _, fastFieldID, docsLimit, cQuery, freeQuery, err := tc.prepareFastFieldSearch(sCtx, fastFieldName, false)
	if err != nil {
		return nil, err
	}
	defer freeQuery()

	outScores := make([]C.float, docsLimit)
	outValues := make([]C.double, docsLimit)
	outValid := make([]C.bool, docsLimit)

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	count := C.context_search_fast_field_f64_json(
		ptr,
		cQuery,
		fastFieldID,
		pointerCType(docsLimit),
		(*C.float)(unsafe.Pointer(&outScores[0])),
		(*C.double)(unsafe.Pointer(&outValues[0])),
		(*C.bool)(unsafe.Pointer(&outValid[0])),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}
	return buildTypedFastFieldResult(count, outScores, outValues, outValid, func(v C.double) float64 { return float64(v) }), nil
}

func (tc *TantivyContext) SearchFastFieldDateJson(sCtx SearchContext, fastFieldName string) (*TypedFastFieldResult[time.Time], error) {
	_, _, fastFieldID, docsLimit, cQuery, freeQuery, err := tc.prepareFastFieldSearch(sCtx, fastFieldName, false)
	if err != nil {
		return nil, err
	}
	defer freeQuery()

	outScores := make([]C.float, docsLimit)
	outValues := make([]C.int64_t, docsLimit)
	outValid := make([]C.bool, docsLimit)

	ptr, unlock, err := tc.lockNative()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var errBuffer *C.char
	count := C.context_search_fast_field_date_json(
		ptr,
		cQuery,
		fastFieldID,
		pointerCType(docsLimit),
		(*C.float)(unsafe.Pointer(&outScores[0])),
		(*C.int64_t)(unsafe.Pointer(&outValues[0])),
		(*C.bool)(unsafe.Pointer(&outValid[0])),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return nil, err
	}
	return buildTypedFastFieldResult(count, outScores, outValues, outValid, func(v C.int64_t) time.Time { return time.UnixMilli(int64(v)).UTC() }), nil
}
