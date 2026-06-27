package tantivy_go

//#include "bindings.h"
import "C"
import (
	"errors"
	"unsafe"
)

type Document struct {
	ptr      *C.Document
	consumed bool
}

// NewDocument creates a new instance of Document.
//
// Returns:
//   - *Document: a pointer to a newly created Document instance.
func NewDocument() *Document {
	ptr := C.document_create()
	return &Document{ptr: ptr}
}

func (d *Document) ensureOpen() error {
	if d == nil || d.ptr == nil {
		if d != nil && d.consumed {
			return ErrConsumedDocument
		}
		return ErrClosedDocument
	}
	if d.consumed {
		return ErrConsumedDocument
	}
	return nil
}

func (d *Document) markConsumed() {
	if d != nil {
		d.ptr = nil
		d.consumed = true
	}
}

func fieldIDFromContext(tc *TantivyContext, fieldName string) (int, error) {
	if tc == nil || tc.schema == nil {
		return 0, ErrClosedContext
	}
	fieldID, contains := tc.schema.fieldNames[fieldName]
	if !contains {
		return 0, errors.New("field not found in schema")
	}
	return fieldID, nil
}

// AddField adds a field with the specified name and value to the document using the given index.
// Returns an error if adding the field fails.
//
// Parameters:
//   - fieldValue: the value of the field to add
//   - index: the index to use for adding the field
//   - fieldName: the name of the field to add
//
// Returns:
//   - error: an error if adding the field fails, or nil if the operation is successful
func (d *Document) AddField(fieldValue string, tc *TantivyContext, fieldName string) error {
	if err := d.ensureOpen(); err != nil {
		return err
	}
	fieldID, err := fieldIDFromContext(tc, fieldName)
	if err != nil {
		return err
	}

	cFieldValue, freeFieldValue := newCString(fieldValue)
	defer freeFieldValue()

	var errBuffer *C.char
	C.document_add_field(d.ptr, C.uint(fieldID), cFieldValue, &errBuffer)
	return tryExtractError(errBuffer)
}

// AddFields adds a field with the specified name and value to the document using the given index.
// Returns an error if adding the field fails.
//
// Parameters:
//   - fieldValue: the value of the field to add
//   - index: the index to use for adding the field
//   - fieldNames: the names of the fields to add
//
// Returns:
//   - error: an error if adding the field fails, or nil if the operation is successful
func (d *Document) AddFields(fieldValue string, tc *TantivyContext, fieldNames ...string) error {
	if err := d.ensureOpen(); err != nil {
		return err
	}
	includeFieldsPtr, err := tc.extractFields(fieldNames)
	if err != nil {
		return err
	}

	cFieldValue, freeFieldValue := newCString(fieldValue)
	defer freeFieldValue()

	var errBuffer *C.char
	C.document_add_fields(d.ptr, (*C.uint)(unsafe.Pointer(&includeFieldsPtr[0])), C.uintptr_t(len(includeFieldsPtr)), cFieldValue, &errBuffer)
	return tryExtractError(errBuffer)
}

// AddU64Field adds an unsigned 64-bit integer field to the document.
func (d *Document) AddU64Field(value uint64, tc *TantivyContext, fieldName string) error {
	if err := d.ensureOpen(); err != nil {
		return err
	}
	fieldID, err := fieldIDFromContext(tc, fieldName)
	if err != nil {
		return err
	}
	var errBuffer *C.char
	C.document_add_u64_field(d.ptr, C.uint(fieldID), C.uint64_t(value), &errBuffer)
	return tryExtractError(errBuffer)
}

// AddI64Field adds a signed 64-bit integer field to the document.
func (d *Document) AddI64Field(value int64, tc *TantivyContext, fieldName string) error {
	if err := d.ensureOpen(); err != nil {
		return err
	}
	fieldID, err := fieldIDFromContext(tc, fieldName)
	if err != nil {
		return err
	}
	var errBuffer *C.char
	C.document_add_i64_field(d.ptr, C.uint(fieldID), C.int64_t(value), &errBuffer)
	return tryExtractError(errBuffer)
}

// AddF64Field adds a 64-bit floating point field to the document.
func (d *Document) AddF64Field(value float64, tc *TantivyContext, fieldName string) error {
	if err := d.ensureOpen(); err != nil {
		return err
	}
	fieldID, err := fieldIDFromContext(tc, fieldName)
	if err != nil {
		return err
	}
	var errBuffer *C.char
	C.document_add_f64_field(d.ptr, C.uint(fieldID), C.double(value), &errBuffer)
	return tryExtractError(errBuffer)
}

// AddDateField adds a date field to the document.
// The value should be a Unix timestamp in milliseconds.
func (d *Document) AddDateField(timestampMillis int64, tc *TantivyContext, fieldName string) error {
	if err := d.ensureOpen(); err != nil {
		return err
	}
	fieldID, err := fieldIDFromContext(tc, fieldName)
	if err != nil {
		return err
	}
	var errBuffer *C.char
	C.document_add_date_field(d.ptr, C.uint(fieldID), C.int64_t(timestampMillis), &errBuffer)
	return tryExtractError(errBuffer)
}

// AddBytesField adds a bytes field to the document.
func (d *Document) AddBytesField(value []byte, tc *TantivyContext, fieldName string) error {
	if err := d.ensureOpen(); err != nil {
		return err
	}
	fieldID, err := fieldIDFromContext(tc, fieldName)
	if err != nil {
		return err
	}

	var ptr *C.char
	if len(value) > 0 {
		ptr = (*C.char)(unsafe.Pointer(&value[0]))
	}

	var errBuffer *C.char
	C.document_add_bytes_field(d.ptr, C.uint(fieldID), ptr, C.uintptr_t(len(value)), &errBuffer)
	return tryExtractError(errBuffer)
}

// AddJSONField adds a JSON object value to a JSON field in the document.
func (d *Document) AddJSONField(value string, tc *TantivyContext, fieldName string) error {
	if err := d.ensureOpen(); err != nil {
		return err
	}
	fieldID, err := fieldIDFromContext(tc, fieldName)
	if err != nil {
		return err
	}

	cValue, freeValue := newCString(value)
	defer freeValue()

	var errBuffer *C.char
	C.document_add_json_field(d.ptr, C.uint(fieldID), cValue, &errBuffer)
	return tryExtractError(errBuffer)
}

// ToJson converts the document to its JSON representation based on the provided schema.
// Optionally, specific fields can be included in the JSON output.
//
// Parameters:
//   - schema: the schema to use for converting the document to JSON
//   - includeFields: optional variadic parameter specifying the fields to include in the JSON output
//
// Returns:
//   - string: the JSON representation of the document
//   - error: an error if the conversion fails, or nil if the operation is successful
func (d *Document) ToJson(tc *TantivyContext, includeFields ...string) (string, error) {
	if err := d.ensureOpen(); err != nil {
		return "", err
	}
	if tc == nil || tc.schema == nil {
		return "", ErrClosedContext
	}
	if err := tc.schema.ensureOpen(); err != nil {
		return "", err
	}

	includeFieldsPtr, err := tc.extractFieldsOrAll(includeFields)
	if err != nil {
		return "", err
	}

	var errBuffer *C.char
	cStr := C.document_as_json(
		d.ptr,
		(*C.uint)(unsafe.Pointer(&includeFieldsPtr[0])),
		C.uintptr_t(len(includeFieldsPtr)),
		tc.schema.ptr,
		&errBuffer,
	)
	if cStr == nil {
		return "", tryExtractError(errBuffer)
	}
	defer C.string_free(cStr)

	return C.GoString(cStr), nil
}

// ToModel converts a document to a model of type T using the provided schema and a conversion function.
//
// Parameters:
//   - doc: the document to convert
//   - schema: the schema to use for converting the document to JSON
//   - includeFields: optional fields to include in the JSON output
//   - f: a function that takes a JSON string and converts it to a model of type T
//
// Returns:
//   - T: the model of type T resulting from the conversion
//   - error: an error if the conversion fails, or nil if the operation is successful
func ToModel[T any](doc *Document, tc *TantivyContext, includeFields []string, f func(json string) (T, error)) (T, error) {
	json, err := doc.ToJson(tc, includeFields...)
	if err != nil {
		var zero T
		return zero, err
	}
	return f(json)
}

func (d *Document) Free() {
	if d == nil || d.ptr == nil {
		return
	}
	C.document_free(d.ptr)
	d.ptr = nil
}
