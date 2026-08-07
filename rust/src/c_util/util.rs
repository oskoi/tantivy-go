use crate::config;
use crate::queries::parse_query_from_json;
use crate::tantivy_util::{
    convert_document_to_json, find_highlights, read_date_fast_field_values,
    read_f64_fast_field_values, read_fast_field_values, read_i64_fast_field_values,
    read_u64_fast_field_values, Document, SearchResult, TantivyContext, TantivyGoError,
    DOCUMENT_BUDGET_BYTES,
};
use log::debug;
use serde_json::json;
use std::borrow::Cow;
use std::collections::HashMap;
use std::ffi::{c_uint, CStr, CString};
use std::os::raw::{c_char, c_float};
use std::panic::PanicHookInfo;
use std::path::Path;
use std::{fs, panic, slice};
use tantivy::directory::MmapDirectory;
use tantivy::indexer::UserOperation;
use tantivy::query::{Query, QueryParser};
use tantivy::schema::{Field, Schema};
use tantivy::{
    DocAddress, Index, IndexWriter, Opstamp, ReloadPolicy, Score, TantivyDocument, TantivyError,
    Term,
};

pub fn set_error(err: &str, error_buffer: *mut *mut c_char) {
    let err_str = match CString::new(err) {
        Ok(s) => s,
        Err(_) => return,
    };
    write_buffer(error_buffer, err_str);
}

fn write_buffer(error_buffer: *mut *mut c_char, err_str: CString) {
    unsafe {
        if !error_buffer.is_null() {
            let c_str = err_str.into_raw();
            *error_buffer = c_str;
        }
    }
}

// Try not to copy one-time-living strings if possible
pub fn assert_str<'a>(str_ptr: *const c_char) -> Result<Cow<'a, str>, TantivyGoError> {
    unsafe {
        if str_ptr.is_null() {
            return Err(TantivyGoError(POINTER_IS_NULL.to_owned()));
        }
        let is_lenient = match config::CONFIG.read() {
            Ok(config) => config.utf8_lenient,
            Err(err) => {
                return Err(TantivyGoError(err.to_string()));
            }
        };
        let cstr = CStr::from_ptr(str_ptr);
        if is_lenient {
            Ok(cstr.to_string_lossy())
        } else {
            match cstr.to_str() {
                Ok(valid_str) => Ok(Cow::Borrowed(valid_str)),
                Err(err) => Err(TantivyGoError(err.to_string())),
            }
        }
    }
}

// Always copy long-living strings for safety reasons
pub fn assert_string(str_ptr: *const c_char) -> Result<String, TantivyGoError> {
    assert_str(str_ptr).map(|cow| cow.into_owned())
}

pub fn assert_pointer<'a, T>(ptr: *mut T) -> Result<&'a mut T, TantivyGoError> {
    if ptr.is_null() {
        return Err(TantivyGoError(POINTER_IS_NULL.to_owned()));
    }
    unsafe { Ok(&mut *ptr) }
}

pub fn process_type_slice<'a, T, F>(
    ptr: *mut *mut T,
    len: usize,
    mut func: F,
) -> Result<(), TantivyGoError>
where
    F: FnMut(*mut T) -> Result<(), TantivyGoError>,
{
    let slice = unsafe { slice::from_raw_parts(assert_pointer(ptr)?, len) };
    slice
        .iter()
        .try_for_each(|&item| func(assert_pointer(item)?))?;
    Ok(())
}

pub fn process_string_slice<'a, F>(
    ptr: *mut *const c_char,
    len: usize,
    mut func: F,
) -> Result<(), TantivyGoError>
where
    F: FnMut(Cow<'a, str>) -> Result<(), TantivyGoError>,
{
    let slice = unsafe { slice::from_raw_parts(assert_pointer(ptr)?, len) };
    slice.iter().try_for_each(|&item| func(assert_str(item)?))?;
    Ok(())
}

pub fn process_slice<'a, F, T>(ptr: *mut T, len: usize, mut func: F) -> Result<(), TantivyGoError>
where
    F: FnMut(usize, T) -> Result<(), TantivyGoError>,
    T: Copy,
{
    let slice = unsafe { slice::from_raw_parts(assert_pointer(ptr)?, len) };
    slice
        .iter()
        .enumerate()
        .try_for_each(|(i, &item)| func(i, item))?;
    Ok(())
}

pub fn convert_document_as_json(
    field_ids: *mut c_uint,
    field_ids_len: usize,
    doc: &mut Document,
    schema: Schema,
) -> Result<String, TantivyGoError> {
    let mut field_to_name = HashMap::new();

    process_slice(field_ids, field_ids_len, |_, field_id| {
        let field = Field::from_field_id(field_id);
        field_to_name.insert(field, Cow::from(schema.get_field_name(field)));
        Ok(())
    })?;

    let doc_json = convert_document_to_json(doc, &schema, &field_to_name)?;

    Ok(json!(doc_json).to_string())
}

pub fn start_lib_init(log_level: &str, clear_on_panic: bool, utf8_lenient: bool) {
    let old_hook = panic::take_hook();
    if clear_on_panic {
        handle_panic(old_hook);
    }

    set_utf8_lenient(utf8_lenient);

    let _ = env_logger::Builder::from_env(env_logger::Env::default().default_filter_or(log_level))
        .try_init();
}

fn set_utf8_lenient(utf8_lenient: bool) {
    if let Err(e) = config::CONFIG.write().map(|mut config| {
        config.update_utf8_lenient(utf8_lenient);
    }) {
        debug!("Failed to set utf8_lenient: {}", e);
    }
}

fn handle_panic(old_hook: Box<dyn Fn(&PanicHookInfo) + Sync + Send>) {
    panic::set_hook(Box::new(move |panic_info| {
        if let Ok(config) = config::CONFIG.read() {
            let fts_path = config.fts_path.as_str();
            if fts_path.is_empty() {
                debug!("fts path is empty");
            } else if let Err(e) = fs::remove_dir_all(Path::new(fts_path)) {
                debug!("Failed to remove directory: {}", e);
            }
        } else {
            debug!("Failed to read config.");
        }
        old_hook(panic_info)
    }));
}

pub fn create_context_with_schema(
    schema: Schema,
    path: String,
) -> Result<*mut TantivyContext, TantivyGoError> {
    config::CONFIG
        .write()
        .map_err(|e| TantivyGoError::from_err("Failed to set path", &e.to_string()))?
        .update_fts_path(path.clone());

    fs::create_dir_all(Path::new(&path))
        .map_err(|e| TantivyGoError::from_err("Failed to create directories", &e.to_string()))?;

    let dir =
        MmapDirectory::open(&path).map_err(|err| TantivyGoError::from_str(&err.to_string()))?;

    let ctx = create_tantivy_context(dir, schema)
        .map_err(|err| TantivyGoError::from_str(&err.to_string()))?;

    Ok(Box::into_raw(Box::new(ctx)))
}

fn create_tantivy_context(
    dir: MmapDirectory,
    schema: Schema,
) -> Result<TantivyContext, TantivyError> {
    let index = Index::open_or_create(dir, schema)?;
    let writer = index.writer(DOCUMENT_BUDGET_BYTES)?;
    let reader = index
        .reader_builder()
        .reload_policy(ReloadPolicy::Manual)
        .try_into()?;
    Ok(TantivyContext::new(index, writer, reader))
}

pub fn consume_documents(
    docs_ptr: *mut *mut Document,
    docs_len: usize,
) -> Result<Vec<TantivyDocument>, TantivyGoError> {
    if docs_len == 0 {
        return Ok(Vec::new());
    }

    let mut documents = Vec::with_capacity(docs_len);
    process_type_slice(docs_ptr, docs_len, |doc| {
        let doc = *box_from(doc);
        documents.push(doc.tantivy_doc);
        Ok(())
    })?;
    Ok(documents)
}

pub fn add_and_consume_documents(
    docs_ptr: *mut *mut Document,
    docs_len: usize,
    writer: &mut IndexWriter,
) -> Result<Opstamp, TantivyGoError> {
    let documents = consume_documents(docs_ptr, docs_len)?;
    if !documents.is_empty() {
        let operations: Vec<UserOperation<TantivyDocument>> =
            documents.into_iter().map(UserOperation::Add).collect();
        writer
            .run(operations)
            .map_err(|err| TantivyGoError::from_err("Add document", &err.to_string()))?;
    }

    let opstamp = commit(writer, "Failed to commit the document")?;
    Ok(opstamp)
}

fn commit(writer: &mut IndexWriter, message: &str) -> Result<Opstamp, TantivyGoError> {
    writer.commit().map_err(|err| {
        rollback(writer);
        TantivyGoError::from_err(message, &err.to_string())
    })
}

pub fn delete_docs<'a>(
    delete_ids_ptr: *mut *const c_char,
    delete_ids_len: usize,
    context: &mut TantivyContext,
    field_id: u32,
) -> Result<Opstamp, TantivyGoError> {
    let field = Field::from_field_id(field_id);

    process_string_slice(delete_ids_ptr, delete_ids_len, |id_value| {
        context
            .writer
            .delete_term(Term::from_field_text(field, &id_value));
        Ok(())
    })
    .map_err(|err| {
        rollback(&mut context.writer);
        err
    })?;

    let opstamp = commit(&mut context.writer, "Failed to commit removing")?;
    Ok(opstamp)
}

fn rollback(writer: &mut IndexWriter) {
    // TEMPORARILY DISABLED: Tantivy has a critical bug in the rollback mechanism
    // https://github.com/quickwit-oss/tantivy/issues/2666
    // TODO: Re-enable when the bug is fixed
    // let _ = writer.rollback();
    let _ = writer; // Suppress unused variable warning
}

pub fn get_doc<'a>(
    index: usize,
    result: &mut SearchResult,
) -> Result<*mut Document, TantivyGoError> {
    let len = result.documents.len();
    if index >= len {
        return Err(TantivyGoError(format!(
            "document index {} out of range for {} search results",
            index, len
        )));
    }

    let doc = result.documents[index].clone();
    Ok(Box::into_raw(Box::new(doc)))
}

pub fn add_fields<'a>(
    doc: &mut Document,
    field_ids: *mut c_uint,
    field_ids_len: usize,
    field_value: &str,
) -> Result<(), TantivyGoError> {
    process_slice(field_ids, field_ids_len, |_, field_id| {
        doc.tantivy_doc
            .add_text(Field::from_field_id(field_id), field_value);
        Ok(())
    })?;

    Ok(())
}

pub fn add_field<'a>(
    doc: &mut Document,
    field_id: u32,
    field_value: &str,
) -> Result<(), TantivyGoError> {
    doc.tantivy_doc
        .add_text(Field::from_field_id(field_id), field_value);
    Ok(())
}

pub fn add_u64_field<'a>(
    doc: &mut Document,
    field_id: u32,
    field_value: u64,
) -> Result<(), TantivyGoError> {
    doc.tantivy_doc
        .add_u64(Field::from_field_id(field_id), field_value);
    Ok(())
}

pub fn add_i64_field<'a>(
    doc: &mut Document,
    field_id: u32,
    field_value: i64,
) -> Result<(), TantivyGoError> {
    doc.tantivy_doc
        .add_i64(Field::from_field_id(field_id), field_value);
    Ok(())
}

pub fn add_f64_field<'a>(
    doc: &mut Document,
    field_id: u32,
    field_value: f64,
) -> Result<(), TantivyGoError> {
    doc.tantivy_doc
        .add_f64(Field::from_field_id(field_id), field_value);
    Ok(())
}

pub fn add_date_field<'a>(
    doc: &mut Document,
    field_id: u32,
    timestamp_millis: i64,
) -> Result<(), TantivyGoError> {
    let datetime = tantivy::DateTime::from_timestamp_millis(timestamp_millis);
    doc.tantivy_doc
        .add_date(Field::from_field_id(field_id), datetime);
    Ok(())
}

pub fn add_bytes_field<'a>(
    doc: &mut Document,
    field_id: u32,
    field_value: &[u8],
) -> Result<(), TantivyGoError> {
    doc.tantivy_doc
        .add_bytes(Field::from_field_id(field_id), field_value);
    Ok(())
}

pub fn add_json_field<'a>(
    doc: &mut Document,
    field_id: u32,
    field_value: &str,
) -> Result<(), TantivyGoError> {
    let parsed: serde_json::Value = serde_json::from_str(field_value)
        .map_err(|e| TantivyGoError::from_err("Invalid JSON value", &e.to_string()))?;
    let owned_value: tantivy::schema::OwnedValue = parsed.into();
    doc.tantivy_doc
        .add_field_value(Field::from_field_id(field_id), &owned_value);
    Ok(())
}

fn perform_search<F>(
    query_parser_fn: F,
    docs_limit: usize,
    context: &mut TantivyContext,
    with_highlights: bool,
) -> Result<*mut SearchResult, TantivyGoError>
where
    F: FnOnce(&Index) -> Result<Box<dyn Query>, String>,
{
    let searcher = &context.reader().searcher();
    let schema = context.index.schema();

    let query = query_parser_fn(&context.index).map_err(|err| TantivyGoError(err))?;

    let top_docs = searcher
        .search(
            &query,
            &tantivy::collector::TopDocs::with_limit(docs_limit).order_by_score(),
        )
        .map_err(|err| TantivyGoError::from_err("Search err", &err.to_string()))?;

    let mut documents = Vec::new();
    for (score, doc_address) in top_docs {
        //let explanation = query.explain(&searcher, doc_address).unwrap();
        //debug!("### exp {:#?}", explanation);
        let doc = searcher
            .doc::<TantivyDocument>(doc_address)
            .map_err(|err| TantivyGoError(err.to_string()))?;
        let highlights = find_highlights(with_highlights, &searcher, &query, &doc, schema.clone())
            .map_err(|err| TantivyGoError(err.to_string()))?;
        documents.push(Document {
            tantivy_doc: doc,
            highlights,
            score,
        });
    }

    Ok(Box::into_raw(Box::new(SearchResult::new(documents))))
}

pub fn search(
    field_ids: *mut c_uint,
    field_weights_ptr: *mut c_float,
    field_ids_len: usize,
    query_ptr: *const c_char,
    docs_limit: usize,
    context: &mut TantivyContext,
    with_highlights: bool,
) -> Result<*mut SearchResult, TantivyGoError> {
    let mut fields = Vec::with_capacity(field_ids_len);
    process_slice(field_ids, field_ids_len, |_, field_id| {
        fields.push(Field::from_field_id(field_id));
        Ok(())
    })?;

    let mut weights = HashMap::with_capacity(field_ids_len);
    process_slice(field_weights_ptr, field_ids_len, |i, field_weight| {
        weights.insert(fields[i], field_weight);
        Ok(())
    })?;

    let query_str = assert_string(query_ptr)?;

    perform_search(
        |index: &Index| {
            let mut query_parser = QueryParser::for_index(index, fields);
            for (field, weight) in weights {
                query_parser.set_field_boost(field, weight as Score);
            }
            query_parser
                .parse_query(query_str.as_str())
                .map_err(|e| e.to_string())
        },
        docs_limit,
        context,
        with_highlights,
    )
}

pub fn search_json(
    query_ptr: *const c_char,
    docs_limit: usize,
    context: &mut TantivyContext,
    with_highlights: bool,
) -> Result<*mut SearchResult, TantivyGoError> {
    let schema = context.index.schema();

    let query_str = assert_string(query_ptr)?;

    perform_search(
        |index: &Index| {
            parse_query_from_json(index, &schema, &query_str).map_err(|e| e.to_string())
        },
        docs_limit,
        context,
        with_highlights,
    )
}

/// Performs a search using a query parser string (supports range queries, fuzzy queries, etc.)
pub fn search_query_parser(
    query_ptr: *const c_char,
    docs_limit: usize,
    context: &mut TantivyContext,
    with_highlights: bool,
    allow_regexes: bool,
) -> Result<*mut SearchResult, TantivyGoError> {
    let query_str = assert_string(query_ptr)?;

    let schema = context.index.schema();
    let mut fields = Vec::new();
    for (field, _entry) in schema.fields() {
        fields.push(field);
    }

    let mut query_parser = QueryParser::for_index(&context.index, fields);
    if allow_regexes {
        query_parser.allow_regexes();
    }
    let query = query_parser
        .parse_query(&query_str)
        .map_err(|e| TantivyGoError(e.to_string()))?;

    perform_search(|_index| Ok(query), docs_limit, context, with_highlights)
}

fn search_top_doc_addresses_with_query(
    searcher: &tantivy::Searcher,
    query: &dyn Query,
    docs_limit: usize,
) -> Result<(Vec<Score>, Vec<DocAddress>), TantivyGoError> {
    let top_docs = searcher
        .search(
            query,
            &tantivy::collector::TopDocs::with_limit(docs_limit).order_by_score(),
        )
        .map_err(|err| TantivyGoError::from_err("Search err", &err.to_string()))?;

    let mut scores = Vec::with_capacity(top_docs.len());
    let mut doc_addresses = Vec::with_capacity(top_docs.len());
    for (score, doc_address) in top_docs {
        scores.push(score);
        doc_addresses.push(doc_address);
    }

    Ok((scores, doc_addresses))
}

fn build_query_parser_query(
    field_ids: *mut c_uint,
    field_weights_ptr: *mut c_float,
    field_ids_len: usize,
    query_ptr: *const c_char,
    index: &Index,
) -> Result<Box<dyn Query>, TantivyGoError> {
    let mut fields = Vec::with_capacity(field_ids_len);
    process_slice(field_ids, field_ids_len, |_, field_id| {
        fields.push(Field::from_field_id(field_id));
        Ok(())
    })?;

    let mut weights = HashMap::with_capacity(field_ids_len);
    process_slice(field_weights_ptr, field_ids_len, |i, field_weight| {
        weights.insert(fields[i], field_weight);
        Ok(())
    })?;

    let query_str = assert_string(query_ptr)?;
    let mut query_parser = QueryParser::for_index(index, fields);
    for (field, weight) in weights {
        query_parser.set_field_boost(field, weight as Score);
    }
    query_parser
        .parse_query(&query_str)
        .map_err(|e| TantivyGoError(e.to_string()))
}

fn search_fast_field_with_query_parser<T>(
    field_ids: *mut c_uint,
    field_weights_ptr: *mut c_float,
    field_ids_len: usize,
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
    read_values: fn(
        &tantivy::Searcher,
        &Schema,
        Field,
        &[DocAddress],
    ) -> Result<Vec<Option<T>>, TantivyGoError>,
) -> Result<(Vec<f32>, Vec<Option<T>>), TantivyGoError> {
    let query = build_query_parser_query(
        field_ids,
        field_weights_ptr,
        field_ids_len,
        query_ptr,
        &context.index,
    )?;
    let searcher = context.reader().searcher();
    let schema = context.index.schema();
    let fast_field = Field::from_field_id(fast_field_id);

    let (scores, doc_addresses) =
        search_top_doc_addresses_with_query(&searcher, query.as_ref(), docs_limit)?;
    if doc_addresses.is_empty() {
        return Ok((scores, vec![]));
    }

    let values = read_values(&searcher, &schema, fast_field, &doc_addresses)?;
    Ok((scores, values))
}

fn search_fast_field_with_json<T>(
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
    read_values: fn(
        &tantivy::Searcher,
        &Schema,
        Field,
        &[DocAddress],
    ) -> Result<Vec<Option<T>>, TantivyGoError>,
) -> Result<(Vec<f32>, Vec<Option<T>>), TantivyGoError> {
    let query_str = assert_string(query_ptr)?;
    let schema = context.index.schema();
    let query = parse_query_from_json(&context.index, &schema, &query_str)
        .map_err(|e| TantivyGoError(e.to_string()))?;
    let searcher = context.reader().searcher();
    let fast_field = Field::from_field_id(fast_field_id);

    let (scores, doc_addresses) =
        search_top_doc_addresses_with_query(&searcher, query.as_ref(), docs_limit)?;
    if doc_addresses.is_empty() {
        return Ok((scores, vec![]));
    }

    let values = read_values(&searcher, &schema, fast_field, &doc_addresses)?;
    Ok((scores, values))
}

/// Performs a search and returns only fast field values (no full document loading).
pub fn search_fast_field(
    field_ids: *mut c_uint,
    field_weights_ptr: *mut c_float,
    field_ids_len: usize,
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
) -> Result<(Vec<f32>, Vec<Option<String>>), TantivyGoError> {
    search_fast_field_with_query_parser(
        field_ids,
        field_weights_ptr,
        field_ids_len,
        query_ptr,
        fast_field_id,
        docs_limit,
        context,
        read_fast_field_values,
    )
}

/// Performs a search using JSON query and returns only fast field values (no full document loading).
pub fn search_fast_field_json(
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
) -> Result<(Vec<f32>, Vec<Option<String>>), TantivyGoError> {
    search_fast_field_with_json(
        query_ptr,
        fast_field_id,
        docs_limit,
        context,
        read_fast_field_values,
    )
}

pub fn search_fast_field_u64(
    field_ids: *mut c_uint,
    field_weights_ptr: *mut c_float,
    field_ids_len: usize,
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
) -> Result<(Vec<f32>, Vec<Option<u64>>), TantivyGoError> {
    search_fast_field_with_query_parser(
        field_ids,
        field_weights_ptr,
        field_ids_len,
        query_ptr,
        fast_field_id,
        docs_limit,
        context,
        read_u64_fast_field_values,
    )
}

pub fn search_fast_field_i64(
    field_ids: *mut c_uint,
    field_weights_ptr: *mut c_float,
    field_ids_len: usize,
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
) -> Result<(Vec<f32>, Vec<Option<i64>>), TantivyGoError> {
    search_fast_field_with_query_parser(
        field_ids,
        field_weights_ptr,
        field_ids_len,
        query_ptr,
        fast_field_id,
        docs_limit,
        context,
        read_i64_fast_field_values,
    )
}

pub fn search_fast_field_f64(
    field_ids: *mut c_uint,
    field_weights_ptr: *mut c_float,
    field_ids_len: usize,
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
) -> Result<(Vec<f32>, Vec<Option<f64>>), TantivyGoError> {
    search_fast_field_with_query_parser(
        field_ids,
        field_weights_ptr,
        field_ids_len,
        query_ptr,
        fast_field_id,
        docs_limit,
        context,
        read_f64_fast_field_values,
    )
}

pub fn search_fast_field_date(
    field_ids: *mut c_uint,
    field_weights_ptr: *mut c_float,
    field_ids_len: usize,
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
) -> Result<(Vec<f32>, Vec<Option<i64>>), TantivyGoError> {
    search_fast_field_with_query_parser(
        field_ids,
        field_weights_ptr,
        field_ids_len,
        query_ptr,
        fast_field_id,
        docs_limit,
        context,
        read_date_fast_field_values,
    )
}

pub fn search_fast_field_u64_json(
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
) -> Result<(Vec<f32>, Vec<Option<u64>>), TantivyGoError> {
    search_fast_field_with_json(
        query_ptr,
        fast_field_id,
        docs_limit,
        context,
        read_u64_fast_field_values,
    )
}

pub fn search_fast_field_i64_json(
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
) -> Result<(Vec<f32>, Vec<Option<i64>>), TantivyGoError> {
    search_fast_field_with_json(
        query_ptr,
        fast_field_id,
        docs_limit,
        context,
        read_i64_fast_field_values,
    )
}

pub fn search_fast_field_f64_json(
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
) -> Result<(Vec<f32>, Vec<Option<f64>>), TantivyGoError> {
    search_fast_field_with_json(
        query_ptr,
        fast_field_id,
        docs_limit,
        context,
        read_f64_fast_field_values,
    )
}

pub fn search_fast_field_date_json(
    query_ptr: *const c_char,
    fast_field_id: c_uint,
    docs_limit: usize,
    context: &mut TantivyContext,
) -> Result<(Vec<f32>, Vec<Option<i64>>), TantivyGoError> {
    search_fast_field_with_json(
        query_ptr,
        fast_field_id,
        docs_limit,
        context,
        read_date_fast_field_values,
    )
}

pub fn drop_any<T>(ptr: *mut T) {
    if !ptr.is_null() {
        unsafe {
            drop(Box::from_raw(ptr));
        }
    }
}

pub fn box_from<T>(ptr: *mut T) -> Box<T> {
    unsafe { Box::from_raw(ptr) }
}

const POINTER_IS_NULL: &'static str = "Pointer is null";
