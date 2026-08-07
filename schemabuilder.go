package tantivy_go

//#include "bindings.h"
import "C"
import "errors"

type (
	SchemaBuilder struct {
		ptr        *C.SchemaBuilder
		fieldNames map[string]int
	}
	Schema struct {
		ptr        *C.Schema
		fieldNames map[string]int
	}
	JSONFieldOptions struct {
		Stored            bool
		IsFast            bool
		IsIndexed         bool
		IndexRecordOption int
		IndexTokenizer    string
		FastTokenizer     string
		ExpandDotsEnabled bool
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

func NewJSONFieldOptions() JSONFieldOptions {
	return JSONFieldOptions{
		IndexRecordOption: IndexRecordOptionWithFreqsAndPositions,
		IndexTokenizer:    DefaultTokenizer,
	}
}

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

func (b *SchemaBuilder) ensureOpen() error {
	if b == nil || b.ptr == nil {
		return ErrClosedSchemaBuilder
	}
	return nil
}

func (s *Schema) ensureOpen() error {
	if s == nil || s.ptr == nil {
		return ErrClosedSchema
	}
	return nil
}

func (s *Schema) Free() {
	if s == nil || s.ptr == nil {
		return
	}
	C.schema_free(s.ptr)
	s.ptr = nil
}

func (b *SchemaBuilder) ensureNewField(name string) error {
	if err := b.ensureOpen(); err != nil {
		return err
	}
	if _, contains := b.fieldNames[name]; contains {
		return errors.New("field already defined: " + name)
	}
	return nil
}

// AddTextField adds a text field to the schema being built.
//
// Parameters:
// - name: The name of the field.
// - stored: Whether the field should be stored in the index.
// - isText: Whether the field should be treated as tantivy text or string for full-text search.
// - isFast: Whether the field should be a Tantivy fast field.
// - isIndexed: Whether the field should have an inverted index and be queryable.
// - indexRecordOption: The indexing option for an indexed field (e.g., basic, with frequencies, with frequencies and positions).
// - tokenizer: The tokenizer for an indexed field.
//
// Returns an error if the field could not be added.
func (b *SchemaBuilder) AddTextField(
	name string,
	stored bool,
	isText bool,
	isFast bool,
	isIndexed bool,
	indexRecordOption int,
	tokenizer string,
) error {
	if err := b.ensureNewField(name); err != nil {
		return err
	}

	cName, freeName := newCString(name)
	defer freeName()
	cTokenizer, freeTokenizer := newCString(tokenizer)
	defer freeTokenizer()

	var errBuffer *C.char
	fieldID := C.schema_builder_add_text_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isText),
		C._Bool(isFast),
		C._Bool(isIndexed),
		pointerCType(indexRecordOption),
		cTokenizer,
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return err
	}

	b.fieldNames[name] = int(fieldID)
	return nil
}

// BuildSchema finalizes the schema building process and returns the resulting Schema.
// Returns a pointer to the Schema and an error if the schema could not be built.
func (b *SchemaBuilder) BuildSchema() (*Schema, error) {
	if err := b.ensureOpen(); err != nil {
		return nil, err
	}

	var errBuffer *C.char
	ptr := C.schema_builder_build(b.ptr, &errBuffer)
	if ptr == nil {
		if err := tryExtractError(errBuffer); err != nil {
			return nil, err
		}
		return nil, errors.New("failed to build schema")
	}

	b.ptr = nil
	fieldNames := make(map[string]int, len(b.fieldNames))
	for k, v := range b.fieldNames {
		fieldNames[k] = v
	}

	return &Schema{ptr: ptr, fieldNames: fieldNames}, nil
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
	if err := b.ensureNewField(name); err != nil {
		return err
	}

	cName, freeName := newCString(name)
	defer freeName()

	var errBuffer *C.char
	fieldID := C.schema_builder_add_u64_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isFast),
		C._Bool(isIndexed),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return err
	}

	b.fieldNames[name] = int(fieldID)
	return nil
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
	if err := b.ensureNewField(name); err != nil {
		return err
	}

	cName, freeName := newCString(name)
	defer freeName()

	var errBuffer *C.char
	fieldID := C.schema_builder_add_i64_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isFast),
		C._Bool(isIndexed),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return err
	}

	b.fieldNames[name] = int(fieldID)
	return nil
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
	if err := b.ensureNewField(name); err != nil {
		return err
	}

	cName, freeName := newCString(name)
	defer freeName()

	var errBuffer *C.char
	fieldID := C.schema_builder_add_f64_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isFast),
		C._Bool(isIndexed),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return err
	}

	b.fieldNames[name] = int(fieldID)
	return nil
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
	if err := b.ensureNewField(name); err != nil {
		return err
	}

	cName, freeName := newCString(name)
	defer freeName()

	var errBuffer *C.char
	fieldID := C.schema_builder_add_date_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isFast),
		C._Bool(isIndexed),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return err
	}

	b.fieldNames[name] = int(fieldID)
	return nil
}

// AddBytesField adds a bytes field to the schema.
// Parameters:
//   - name: The name of the field.
//   - stored: Whether the field should be stored in the index.
//   - isFast: Whether the field should be a fast field.
//   - isIndexed: Whether the field should be indexed.
func (b *SchemaBuilder) AddBytesField(name string, stored bool, isFast bool, isIndexed bool) error {
	if err := b.ensureNewField(name); err != nil {
		return err
	}

	cName, freeName := newCString(name)
	defer freeName()

	var errBuffer *C.char
	fieldID := C.schema_builder_add_bytes_field(
		b.ptr,
		cName,
		C._Bool(stored),
		C._Bool(isFast),
		C._Bool(isIndexed),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return err
	}

	b.fieldNames[name] = int(fieldID)
	return nil
}

// AddJSONField adds a JSON object field to the schema.
func (b *SchemaBuilder) AddJSONField(name string, opts JSONFieldOptions) error {
	if err := b.ensureNewField(name); err != nil {
		return err
	}

	if opts.IndexTokenizer == "" {
		opts.IndexTokenizer = DefaultTokenizer
	}

	cName, freeName := newCString(name)
	defer freeName()
	cIndexTokenizer, freeIndexTokenizer := newCString(opts.IndexTokenizer)
	defer freeIndexTokenizer()

	var cFastTokenizer *C.char
	if opts.FastTokenizer != "" {
		var freeFastTokenizer func()
		cFastTokenizer, freeFastTokenizer = newCString(opts.FastTokenizer)
		defer freeFastTokenizer()
	}

	var errBuffer *C.char
	fieldID := C.schema_builder_add_json_field(
		b.ptr,
		cName,
		C._Bool(opts.Stored),
		C._Bool(opts.IsFast),
		C._Bool(opts.IsIndexed),
		pointerCType(opts.IndexRecordOption),
		cIndexTokenizer,
		cFastTokenizer,
		C._Bool(opts.ExpandDotsEnabled),
		&errBuffer,
	)
	if err := tryExtractError(errBuffer); err != nil {
		return err
	}

	b.fieldNames[name] = int(fieldID)
	return nil
}
