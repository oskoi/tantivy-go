package tantivy_go

//#include "bindings.h"
import "C"
import (
	"errors"
)

type (
	SchemaBuilder struct {
		ptr        *C.SchemaBuilder
		fieldNames map[string]int
	}
	Schema struct {
		ptr        *C.Schema
		fieldNames map[string]int
	}
)

const (
	// IndexRecordOptionBasic specifies that only basic indexing information should be used.
	IndexRecordOptionBasic = iota
	// IndexRecordOptionWithFreqs specifies that indexing should include term frequencies.
	IndexRecordOptionWithFreqs
	// IndexRecordOptionWithFreqsAndPositions specifies that indexing should include term frequencies and term positions.
	IndexRecordOptionWithFreqsAndPositions
)

const DefaultTokenizer = "default"

type Language string

const (
	Arabic     Language = "ar"
	Danish     Language = "da"
	Dutch      Language = "nl"
	English    Language = "en"
	Finnish    Language = "fi"
	French     Language = "fr"
	German     Language = "de"
	Greek      Language = "el"
	Hungarian  Language = "hu"
	Italian    Language = "it"
	Norwegian  Language = "no"
	Portuguese Language = "pt"
	Romanian   Language = "ro"
	Russian    Language = "ru"
	Spanish    Language = "es"
	Swedish    Language = "sv"
	Tamil      Language = "ta"
	Turkish    Language = "tr"
)

// NewSchemaBuilder creates a new SchemaBuilder instance.
// Returns a pointer to the SchemaBuilder and an error if creation fails.
func NewSchemaBuilder() (*SchemaBuilder, error) {
	ptr := C.schema_builder_new()
	if ptr == nil {
		return nil, errors.New("failed to create schema builder")
	}
	return &SchemaBuilder{ptr: ptr, fieldNames: make(map[string]int)}, nil
}

// AddTextField adds a text field to the schema being built.
//
// Parameters:
// - name: The name of the field.
// - stored: Whether the field should be stored in the index.
// - isText: Whether the field should be treated as tantivy text or string for full-text search.
// - isFast: Whether the field should be isText as tantivy quick field.
// - indexRecordOption: The indexing option to be used (e.g., basic, with frequencies, with frequencies and positions).
// - tokenizer: The name of the tokenizer to be used for the field.
//
// Returns an error if the field could not be added.
func (b *SchemaBuilder) AddTextField(
	name string,
	stored bool,
	isText bool,
	isFast bool,
	indexRecordOption int,
	tokenizer string,
) error {
	if _, contains := b.fieldNames[name]; contains {
		return errors.New("field already defined: " + name)
	}
	b.fieldNames[name] = -1
	cName := C.CString(name)
	cTokenizer := C.CString(tokenizer)
	defer C.string_free(cName)
	defer C.string_free(cTokenizer)
	var errBuffer *C.char
	fieldId := C.schema_builder_add_text_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isText),
		C._Bool(isFast),
		pointerCType(indexRecordOption),
		cTokenizer,
		&errBuffer,
	)
	b.fieldNames[name] = int(fieldId)
	return tryExtractError(errBuffer)
}

// BuildSchema finalizes the schema building process and returns the resulting Schema.
// Returns a pointer to the Schema and an error if the schema could not be built.
func (b *SchemaBuilder) BuildSchema() (*Schema, error) {
	var errBuffer *C.char
	ptr := C.schema_builder_build(b.ptr, &errBuffer)
	if ptr == nil {
		defer C.string_free(errBuffer)
		return nil, errors.New(C.GoString(errBuffer))
	}
	return &Schema{
		ptr:        ptr,
		fieldNames: b.fieldNames,
	}, nil
}

// AddU64Field adds an unsigned 64-bit integer field to the schema.
// Parameters:
//   - name: The name of the field.
//   - stored: Whether the field should be stored in the index.
//   - isFast: Whether the field should be a fast field.
//   - isIndexed: Whether the field should be indexed for range queries.
//
// Returns an error if the field could not be added.
func (b *SchemaBuilder) AddU64Field(name string, stored bool, isFast bool, isIndexed bool) error {
	if _, contains := b.fieldNames[name]; contains {
		return errors.New("field already defined: " + name)
	}
	b.fieldNames[name] = -1
	cName := C.CString(name)
	defer C.string_free(cName)
	var errBuffer *C.char
	fieldId := C.schema_builder_add_u64_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isFast),
		C._Bool(isIndexed),
		&errBuffer,
	)
	b.fieldNames[name] = int(fieldId)
	return tryExtractError(errBuffer)
}

// AddI64Field adds a signed 64-bit integer field to the schema.
// Parameters:
//   - name: The name of the field.
//   - stored: Whether the field should be stored in the index.
//   - isFast: Whether the field should be a fast field.
//   - isIndexed: Whether the field should be indexed for range queries.
//
// Returns an error if the field could not be added.
func (b *SchemaBuilder) AddI64Field(name string, stored bool, isFast bool, isIndexed bool) error {
	if _, contains := b.fieldNames[name]; contains {
		return errors.New("field already defined: " + name)
	}
	b.fieldNames[name] = -1
	cName := C.CString(name)
	defer C.string_free(cName)
	var errBuffer *C.char
	fieldId := C.schema_builder_add_i64_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isFast),
		C._Bool(isIndexed),
		&errBuffer,
	)
	b.fieldNames[name] = int(fieldId)
	return tryExtractError(errBuffer)
}

// AddF64Field adds a 64-bit floating point field to the schema.
// Parameters:
//   - name: The name of the field.
//   - stored: Whether the field should be stored in the index.
//   - isFast: Whether the field should be a fast field.
//   - isIndexed: Whether the field should be indexed for range queries.
//
// Returns an error if the field could not be added.
func (b *SchemaBuilder) AddF64Field(name string, stored bool, isFast bool, isIndexed bool) error {
	if _, contains := b.fieldNames[name]; contains {
		return errors.New("field already defined: " + name)
	}
	b.fieldNames[name] = -1
	cName := C.CString(name)
	defer C.string_free(cName)
	var errBuffer *C.char
	fieldId := C.schema_builder_add_f64_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isFast),
		C._Bool(isIndexed),
		&errBuffer,
	)
	b.fieldNames[name] = int(fieldId)
	return tryExtractError(errBuffer)
}

// AddDateField adds a date field to the schema.
// Parameters:
//   - name: The name of the field.
//   - stored: Whether the field should be stored in the index.
//   - isFast: Whether the field should be a fast field.
//   - isIndexed: Whether the field should be indexed for range queries.
//
// Returns an error if the field could not be added.
// The date value should be provided as Unix timestamp in milliseconds.
func (b *SchemaBuilder) AddDateField(name string, stored bool, isFast bool, isIndexed bool) error {
	if _, contains := b.fieldNames[name]; contains {
		return errors.New("field already defined: " + name)
	}
	b.fieldNames[name] = -1
	cName := C.CString(name)
	defer C.string_free(cName)
	var errBuffer *C.char
	fieldId := C.schema_builder_add_date_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isFast),
		C._Bool(isIndexed),
		&errBuffer,
	)
	b.fieldNames[name] = int(fieldId)
	return tryExtractError(errBuffer)
}

// AddBytesField adds a bytes field to the schema.
// Parameters:
//   - name: The name of the field.
//   - stored: Whether the field should be stored in the index.
//   - isFast: Whether the field should be a fast field.
//   - isIndexed: Whether the field should be indexed.
func (b *SchemaBuilder) AddBytesField(name string, stored bool, isFast bool, isIndexed bool) error {
	if _, contains := b.fieldNames[name]; contains {
		return errors.New("field already defined: " + name)
	}
	b.fieldNames[name] = -1
	cName := C.CString(name)
	defer C.string_free(cName)
	var errBuffer *C.char
	fieldId := C.schema_builder_add_bytes_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isFast),
		C._Bool(isIndexed),
		&errBuffer,
	)
	b.fieldNames[name] = int(fieldId)
	return tryExtractError(errBuffer)
}
