use crate::c_util::util::assert_string;
use crate::tantivy_util::{Document, SearchResult, TantivyContext, TantivyGoError};
use std::cmp::Ordering;
use std::collections::BinaryHeap;
use std::ffi::c_char;
use std::slice;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use tantivy::columnar::{Column, ColumnIndex, ColumnType, StrColumn};
use tantivy::query::{EnableScoring, Query, QueryParser};
use tantivy::schema::FieldType;
use tantivy::{
    DateTime, DocAddress, DocId, DocSet, Index, Searcher, SegmentReader, TantivyDocument,
    TERMINATED,
};

pub const SEARCH_TIMEOUT_ERROR: &str = "tantivy-go sorted search deadline exceeded";

const MAX_SORT_FIELDS: usize = 4;
const MAX_SORTED_SEARCH_LIMIT: usize = 10_000;
const DEADLINE_CHECK_INTERVAL: usize = 1_024;

#[repr(C)]
#[derive(Clone, Copy)]
pub struct SortedSearchField {
    /// Input field-name bytes are borrowed only for `context_search_query_sorted`;
    /// they remain caller-owned and must not be retained.
    pub name_ptr: *const c_char,
    pub direction: u8,
}

#[repr(C)]
#[derive(Clone, Copy)]
pub struct SortedSearchValue {
    pub kind: u8,
    pub missing: bool,
    /// For input cursors, text bytes are borrowed only for
    /// `context_search_query_sorted`; they remain caller-owned.
    /// For output tuples, this borrows `SearchResult` storage until `search_result_free`;
    /// Go copies it first.
    pub text_ptr: *const c_char,
    pub text_len: usize,
    pub u64_value: u64,
    pub i64_value: i64,
    pub f64_value: f64,
    pub bool_value: bool,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(crate) enum SortDirection {
    Ascending,
    Descending,
}

impl SortDirection {
    fn from_ffi(value: u8) -> Result<Self, TantivyGoError> {
        match value {
            1 => Ok(Self::Ascending),
            2 => Ok(Self::Descending),
            _ => Err(TantivyGoError(format!(
                "invalid sorted search direction {value}"
            ))),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(crate) enum SortValueKind {
    Text,
    U64,
    I64,
    F64,
    Bool,
    Date,
}

impl SortValueKind {
    fn from_ffi(value: u8) -> Result<Self, TantivyGoError> {
        match value {
            1 => Ok(Self::Text),
            2 => Ok(Self::U64),
            3 => Ok(Self::I64),
            4 => Ok(Self::F64),
            5 => Ok(Self::Bool),
            6 => Ok(Self::Date),
            _ => Err(TantivyGoError(format!(
                "invalid sorted search value kind {value}"
            ))),
        }
    }

    fn as_ffi(self) -> u8 {
        match self {
            Self::Text => 1,
            Self::U64 => 2,
            Self::I64 => 3,
            Self::F64 => 4,
            Self::Bool => 5,
            Self::Date => 6,
        }
    }
}

#[derive(Clone, Debug, PartialEq)]
pub(crate) enum SortAtom {
    Missing(SortValueKind),
    Text(String),
    U64(u64),
    I64(i64),
    F64(f64),
    Bool(bool),
    Date(i64),
}

impl SortAtom {
    pub(crate) fn kind(&self) -> SortValueKind {
        match self {
            Self::Missing(kind) => *kind,
            Self::Text(_) => SortValueKind::Text,
            Self::U64(_) => SortValueKind::U64,
            Self::I64(_) => SortValueKind::I64,
            Self::F64(_) => SortValueKind::F64,
            Self::Bool(_) => SortValueKind::Bool,
            Self::Date(_) => SortValueKind::Date,
        }
    }

    fn is_missing(&self) -> bool {
        matches!(self, Self::Missing(_))
    }
}

#[derive(Clone, Debug, PartialEq)]
pub(crate) struct SortTuple {
    pub(crate) atoms: [SortAtom; MAX_SORT_FIELDS],
    pub(crate) len: usize,
}

impl SortTuple {
    pub(crate) fn new(len: usize) -> Self {
        debug_assert!(len <= MAX_SORT_FIELDS);
        Self {
            atoms: std::array::from_fn(|_| SortAtom::Missing(SortValueKind::Text)),
            len,
        }
    }

    pub(crate) fn atoms(&self) -> &[SortAtom] {
        &self.atoms[..self.len]
    }
}

struct RuntimeSortField {
    name: String,
    kind: SortValueKind,
}

struct RuntimeSortDescriptor {
    fields: Vec<RuntimeSortField>,
    order: SortOrder,
}

impl RuntimeSortDescriptor {
    fn from_ffi(
        schema: &tantivy::schema::Schema,
        searcher: &Searcher,
        fields: &[SortedSearchField],
        deadline: &SearchDeadline,
    ) -> Result<Self, TantivyGoError> {
        if !(1..=MAX_SORT_FIELDS).contains(&fields.len()) {
            return Err(TantivyGoError(format!(
                "sorted search requires between 1 and {MAX_SORT_FIELDS} sort fields"
            )));
        }

        let mut runtime_fields = Vec::with_capacity(fields.len());
        let mut directions = [SortDirection::Ascending; MAX_SORT_FIELDS];
        for (index, field) in fields.iter().enumerate() {
            let name = assert_string(field.name_ptr)?;
            if name.is_empty() {
                return Err(TantivyGoError(
                    "sorted search field name is empty".to_string(),
                ));
            }

            let direction = SortDirection::from_ffi(field.direction)?;
            let kind = resolve_sort_value_kind(schema, searcher, &name, deadline)?;
            directions[index] = direction;
            runtime_fields.push(RuntimeSortField { name, kind });
        }

        Ok(Self {
            fields: runtime_fields,
            order: SortOrder {
                directions,
                len: fields.len(),
            },
        })
    }

    fn parse_after(
        &self,
        values: &[SortedSearchValue],
    ) -> Result<Option<SortTuple>, TantivyGoError> {
        if values.is_empty() {
            return Ok(None);
        }
        if values.len() != self.fields.len() {
            return Err(TantivyGoError(format!(
                "sorted search after tuple has {} values for {} sort fields",
                values.len(),
                self.fields.len()
            )));
        }

        let mut tuple = SortTuple::new(values.len());
        for (index, (value, field)) in values.iter().zip(&self.fields).enumerate() {
            tuple.atoms[index] = sort_atom_from_ffi(value, field.kind)?;
        }
        Ok(Some(tuple))
    }

    fn open_segment(&self, segment: &SegmentReader) -> Result<SegmentSortColumns, TantivyGoError> {
        SegmentSortColumns::open(&self.fields, segment)
    }
}

fn resolve_sort_value_kind(
    schema: &tantivy::schema::Schema,
    searcher: &Searcher,
    name: &str,
    deadline: &SearchDeadline,
) -> Result<SortValueKind, TantivyGoError> {
    let Some((field, path)) = schema.find_field(name) else {
        return Err(TantivyGoError(format!(
            "sorted search field {name:?} does not exist"
        )));
    };
    let field_entry = schema.get_field_entry(field);
    if !field_entry.is_fast() {
        return Err(TantivyGoError(format!(
            "sorted search field {name:?} is not configured as a fast field"
        )));
    }

    match field_entry.field_type() {
        FieldType::Str(_) if path.is_empty() => Ok(SortValueKind::Text),
        FieldType::U64(_) if path.is_empty() => Ok(SortValueKind::U64),
        FieldType::I64(_) if path.is_empty() => Ok(SortValueKind::I64),
        FieldType::F64(_) if path.is_empty() => Ok(SortValueKind::F64),
        FieldType::Bool(_) if path.is_empty() => Ok(SortValueKind::Bool),
        FieldType::Date(_) if path.is_empty() => Ok(SortValueKind::Date),
        FieldType::JsonObject(_) if !path.is_empty() => {
            resolve_json_sort_value_kind(searcher, name, deadline)
        }
        _ => Err(TantivyGoError(format!(
            "sorted search field {name:?} is not a supported fast field"
        ))),
    }
}

fn resolve_json_sort_value_kind(
    searcher: &Searcher,
    name: &str,
    deadline: &SearchDeadline,
) -> Result<SortValueKind, TantivyGoError> {
    let mut kind = None;
    for segment in searcher.segment_readers() {
        ensure_deadline(deadline)?;
        let columns = segment
            .fast_fields()
            .dynamic_column_handles(name)
            .map_err(|err| {
                TantivyGoError::from_err("open JSON sorted fast field", &err.to_string())
            })?;
        for column in columns {
            let column = column.open().map_err(|err| {
                TantivyGoError::from_err("open JSON sorted fast field column", &err.to_string())
            })?;
            let has_live_value =
                any_live_column_value(segment.doc_ids_alive(), column.column_index(), deadline)?;
            merge_live_json_column_kind(&mut kind, column.column_type(), has_live_value, name)?;
        }
    }

    kind.ok_or_else(|| {
        TantivyGoError(format!(
            "JSON sorted search field {name:?} has no populated sortable column"
        ))
    })
}

fn any_live_column_value(
    docs: impl Iterator<Item = DocId>,
    column_index: &ColumnIndex,
    deadline: &SearchDeadline,
) -> Result<bool, TantivyGoError> {
    let mut inspected_documents = 0usize;
    for doc in docs {
        check_deadline_before_document(deadline, &mut inspected_documents)?;
        if column_index.has_value(doc) {
            return Ok(true);
        }
    }
    Ok(false)
}

fn merge_live_json_column_kind(
    kind: &mut Option<SortValueKind>,
    column_type: ColumnType,
    has_live_value: bool,
    name: &str,
) -> Result<(), TantivyGoError> {
    if !has_live_value {
        return Ok(());
    }

    let column_kind = match column_type {
        ColumnType::Str => SortValueKind::Text,
        ColumnType::U64 => SortValueKind::U64,
        ColumnType::I64 => SortValueKind::I64,
        ColumnType::F64 => SortValueKind::F64,
        ColumnType::Bool => SortValueKind::Bool,
        ColumnType::DateTime => SortValueKind::Date,
        unsupported => {
            return Err(TantivyGoError(format!(
                "JSON sorted search field {name:?} has unsupported column type {unsupported:?}"
            )));
        }
    };
    if let Some(existing) = *kind {
        if existing != column_kind {
            return Err(TantivyGoError(format!(
                "JSON sorted search field {name:?} has multiple value kinds"
            )));
        }
    } else {
        *kind = Some(column_kind);
    }
    Ok(())
}

enum SegmentSortColumn {
    Text(Option<StrColumn>),
    U64(Option<Column<u64>>),
    I64(Option<Column<i64>>),
    F64(Option<Column<f64>>),
    Bool(Option<Column<bool>>),
    Date(Option<Column<DateTime>>),
}

impl SegmentSortColumn {
    fn open(field: &RuntimeSortField, segment: &SegmentReader) -> Result<Self, TantivyGoError> {
        let fast_fields = segment.fast_fields();
        match field.kind {
            SortValueKind::Text => fast_fields.str(&field.name).map(Self::Text).map_err(|err| {
                TantivyGoError::from_err("open text sorted fast field", &err.to_string())
            }),
            SortValueKind::U64 => fast_fields
                .column_opt::<u64>(&field.name)
                .map(Self::U64)
                .map_err(|err| {
                    TantivyGoError::from_err("open U64 sorted fast field", &err.to_string())
                }),
            SortValueKind::I64 => fast_fields
                .column_opt::<i64>(&field.name)
                .map(Self::I64)
                .map_err(|err| {
                    TantivyGoError::from_err("open I64 sorted fast field", &err.to_string())
                }),
            SortValueKind::F64 => fast_fields
                .column_opt::<f64>(&field.name)
                .map(Self::F64)
                .map_err(|err| {
                    TantivyGoError::from_err("open F64 sorted fast field", &err.to_string())
                }),
            SortValueKind::Bool => fast_fields
                .column_opt::<bool>(&field.name)
                .map(Self::Bool)
                .map_err(|err| {
                    TantivyGoError::from_err("open Bool sorted fast field", &err.to_string())
                }),
            SortValueKind::Date => fast_fields
                .column_opt::<DateTime>(&field.name)
                .map(Self::Date)
                .map_err(|err| {
                    TantivyGoError::from_err("open Date sorted fast field", &err.to_string())
                }),
        }
    }

    fn value_for_doc(&self, doc: DocId) -> Result<SortAtom, TantivyGoError> {
        match self {
            Self::Text(column) => {
                let Some(column) = column else {
                    return Ok(SortAtom::Missing(SortValueKind::Text));
                };
                let Some(term_ord) = column.ords().first(doc) else {
                    return Ok(SortAtom::Missing(SortValueKind::Text));
                };
                let mut value = String::new();
                if !column.ord_to_str(term_ord, &mut value).map_err(|err| {
                    TantivyGoError::from_err("read text sorted fast field", &err.to_string())
                })? {
                    return Ok(SortAtom::Missing(SortValueKind::Text));
                }
                Ok(SortAtom::Text(value))
            }
            Self::U64(column) => Ok(column
                .as_ref()
                .and_then(|column| column.first(doc))
                .map(SortAtom::U64)
                .unwrap_or(SortAtom::Missing(SortValueKind::U64))),
            Self::I64(column) => Ok(column
                .as_ref()
                .and_then(|column| column.first(doc))
                .map(SortAtom::I64)
                .unwrap_or(SortAtom::Missing(SortValueKind::I64))),
            Self::F64(column) => {
                let value = column.as_ref().and_then(|column| column.first(doc));
                match value {
                    Some(value) if value.is_nan() => Err(TantivyGoError(
                        "F64 sorted search value cannot be NaN".to_string(),
                    )),
                    Some(value) => Ok(SortAtom::F64(value)),
                    None => Ok(SortAtom::Missing(SortValueKind::F64)),
                }
            }
            Self::Bool(column) => Ok(column
                .as_ref()
                .and_then(|column| column.first(doc))
                .map(SortAtom::Bool)
                .unwrap_or(SortAtom::Missing(SortValueKind::Bool))),
            Self::Date(column) => Ok(column
                .as_ref()
                .and_then(|column| column.first(doc))
                .map(|value| SortAtom::Date(value.into_timestamp_millis()))
                .unwrap_or(SortAtom::Missing(SortValueKind::Date))),
        }
    }
}

struct SegmentSortColumns {
    columns: Vec<SegmentSortColumn>,
}

impl SegmentSortColumns {
    fn open(fields: &[RuntimeSortField], segment: &SegmentReader) -> Result<Self, TantivyGoError> {
        let mut columns = Vec::with_capacity(fields.len());
        for field in fields {
            columns.push(SegmentSortColumn::open(field, segment)?);
        }
        Ok(Self { columns })
    }

    fn tuple_for_doc(&self, doc: DocId) -> Result<SortTuple, TantivyGoError> {
        let mut tuple = SortTuple::new(self.columns.len());
        for (index, column) in self.columns.iter().enumerate() {
            tuple.atoms[index] = column.value_for_doc(doc)?;
        }
        Ok(tuple)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
struct SortOrder {
    directions: [SortDirection; MAX_SORT_FIELDS],
    len: usize,
}

fn compare_sort_tuples(left: &SortTuple, right: &SortTuple, order: SortOrder) -> Ordering {
    debug_assert_eq!(left.len, order.len);
    debug_assert_eq!(right.len, order.len);
    for index in 0..order.len {
        let ordering = compare_sort_atoms(
            &left.atoms[index],
            &right.atoms[index],
            order.directions[index],
        );
        if ordering != Ordering::Equal {
            return ordering;
        }
    }
    Ordering::Equal
}

fn compare_sort_atoms(left: &SortAtom, right: &SortAtom, direction: SortDirection) -> Ordering {
    match (left.is_missing(), right.is_missing()) {
        (true, true) => return Ordering::Equal,
        (true, false) => return Ordering::Greater,
        (false, true) => return Ordering::Less,
        (false, false) => {}
    }

    debug_assert_eq!(left.kind(), right.kind());
    let ordering = match (left, right) {
        (SortAtom::Text(left), SortAtom::Text(right)) => left.cmp(right),
        (SortAtom::U64(left), SortAtom::U64(right)) => left.cmp(right),
        (SortAtom::I64(left), SortAtom::I64(right)) => left.cmp(right),
        (SortAtom::F64(left), SortAtom::F64(right)) => left.total_cmp(right),
        (SortAtom::Bool(left), SortAtom::Bool(right)) => left.cmp(right),
        (SortAtom::Date(left), SortAtom::Date(right)) => left.cmp(right),
        _ => unreachable!("sort atom kinds must match"),
    };
    match direction {
        SortDirection::Ascending => ordering,
        SortDirection::Descending => ordering.reverse(),
    }
}

struct Candidate {
    address: DocAddress,
    tuple: SortTuple,
    order: SortOrder,
}

impl Ord for Candidate {
    fn cmp(&self, other: &Self) -> Ordering {
        debug_assert_eq!(self.order, other.order);
        compare_sort_tuples(&self.tuple, &other.tuple, self.order)
            .then_with(|| self.address.cmp(&other.address))
    }
}

impl PartialOrd for Candidate {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl PartialEq for Candidate {
    fn eq(&self, other: &Self) -> bool {
        self.cmp(other) == Ordering::Equal
    }
}

impl Eq for Candidate {}

struct BoundedTopK {
    capacity: usize,
    order: SortOrder,
    candidates: BinaryHeap<Candidate>,
}

impl BoundedTopK {
    fn new(capacity: usize, order: SortOrder) -> Self {
        Self {
            capacity,
            order,
            candidates: BinaryHeap::with_capacity(capacity),
        }
    }

    fn push(&mut self, address: DocAddress, tuple: SortTuple) {
        let candidate = Candidate {
            address,
            tuple,
            order: self.order,
        };
        if self.candidates.len() < self.capacity {
            self.candidates.push(candidate);
            return;
        }

        let should_replace = self
            .candidates
            .peek()
            .map(|worst| candidate.cmp(worst) == Ordering::Less)
            .unwrap_or(false);
        if should_replace {
            *self
                .candidates
                .peek_mut()
                .expect("bounded sorted search collector has a worst candidate") = candidate;
        }
    }

    fn into_sorted(self) -> Vec<Candidate> {
        let mut candidates = self.candidates.into_vec();
        candidates.sort();
        candidates
    }
}

enum SearchDeadline {
    Instant(Instant),
    System(SystemTime),
    #[cfg(test)]
    Test(std::cell::Cell<usize>),
}

impl SearchDeadline {
    fn from_unix(deadline_seconds: i64, deadline_nanos: u32) -> Result<Self, TantivyGoError> {
        if deadline_nanos >= 1_000_000_000 {
            return Err(TantivyGoError(
                "invalid sorted search deadline nanoseconds".to_string(),
            ));
        }

        let seconds = Duration::from_secs(deadline_seconds.unsigned_abs());
        let deadline = if deadline_seconds >= 0 {
            UNIX_EPOCH.checked_add(seconds)
        } else {
            UNIX_EPOCH.checked_sub(seconds)
        }
        .and_then(|time| time.checked_add(Duration::from_nanos(deadline_nanos as u64)))
        .ok_or_else(|| TantivyGoError("sorted search deadline is out of range".to_string()))?;

        let remaining = deadline
            .duration_since(SystemTime::now())
            .unwrap_or(Duration::ZERO);
        match Instant::now().checked_add(remaining) {
            Some(deadline) => Ok(Self::Instant(deadline)),
            None => Ok(Self::System(deadline)),
        }
    }

    #[cfg(test)]
    fn test_after_successful_checks(checks: usize) -> Self {
        Self::Test(std::cell::Cell::new(checks))
    }

    fn is_expired(&self) -> bool {
        match self {
            Self::Instant(deadline) => Instant::now() >= *deadline,
            Self::System(deadline) => SystemTime::now().duration_since(deadline.clone()).is_ok(),
            #[cfg(test)]
            Self::Test(checks) => {
                let remaining = checks.get();
                if remaining == 0 {
                    true
                } else {
                    checks.set(remaining - 1);
                    false
                }
            }
        }
    }
}

fn ensure_deadline(deadline: &SearchDeadline) -> Result<(), TantivyGoError> {
    if deadline.is_expired() {
        Err(TantivyGoError(SEARCH_TIMEOUT_ERROR.to_string()))
    } else {
        Ok(())
    }
}

fn check_deadline_after<T>(
    deadline: &SearchDeadline,
    operation: impl FnOnce() -> Result<T, TantivyGoError>,
) -> Result<T, TantivyGoError> {
    let result = operation();
    ensure_deadline(deadline)?;
    result
}

fn check_deadline_before_document(
    deadline: &SearchDeadline,
    inspected_documents: &mut usize,
) -> Result<(), TantivyGoError> {
    if *inspected_documents % DEADLINE_CHECK_INTERVAL == 0 {
        ensure_deadline(deadline)?;
    }
    *inspected_documents += 1;
    Ok(())
}

fn parse_sorted_query_parser(index: &Index, query: &str) -> Result<Box<dyn Query>, TantivyGoError> {
    let mut parser = QueryParser::for_index(index, vec![]);
    parser.allow_regexes();
    parser
        .parse_query(query)
        .map_err(|err| TantivyGoError::from_err("parse sorted search query", &err.to_string()))
}

pub fn search_query_sorted(
    query_ptr: *const c_char,
    fields_ptr: *const SortedSearchField,
    fields_len: usize,
    after_ptr: *const SortedSearchValue,
    after_len: usize,
    docs_limit: usize,
    deadline_seconds: i64,
    deadline_nanos: u32,
    context: &mut TantivyContext,
) -> Result<*mut SearchResult, TantivyGoError> {
    search_sorted(
        query_ptr,
        fields_ptr,
        fields_len,
        after_ptr,
        after_len,
        docs_limit,
        deadline_seconds,
        deadline_nanos,
        context,
    )
}

fn search_sorted(
    query_ptr: *const c_char,
    fields_ptr: *const SortedSearchField,
    fields_len: usize,
    after_ptr: *const SortedSearchValue,
    after_len: usize,
    docs_limit: usize,
    deadline_seconds: i64,
    deadline_nanos: u32,
    context: &mut TantivyContext,
) -> Result<*mut SearchResult, TantivyGoError> {
    let capacity = sorted_search_capacity(docs_limit)?;
    let deadline = SearchDeadline::from_unix(deadline_seconds, deadline_nanos)?;
    search_sorted_with_deadline(
        query_ptr, fields_ptr, fields_len, after_ptr, after_len, docs_limit, capacity, &deadline,
        context,
    )
}

fn search_sorted_with_deadline(
    query_ptr: *const c_char,
    fields_ptr: *const SortedSearchField,
    fields_len: usize,
    after_ptr: *const SortedSearchValue,
    after_len: usize,
    docs_limit: usize,
    capacity: usize,
    deadline: &SearchDeadline,
    context: &mut TantivyContext,
) -> Result<*mut SearchResult, TantivyGoError> {
    let schema = context.index.schema();
    let query = construct_sorted_query(query_ptr, &context.index, deadline)?;
    let searcher = context.reader()?.searcher();
    let fields = ffi_slice(fields_ptr, fields_len, "sorted search fields")?;
    let after_values = ffi_slice(after_ptr, after_len, "sorted search after values")?;
    let descriptor = RuntimeSortDescriptor::from_ffi(&schema, &searcher, fields, deadline)?;
    let after = descriptor.parse_after(after_values)?;

    ensure_deadline(deadline)?;
    let weight = check_deadline_after(deadline, || {
        query
            .weight(EnableScoring::disabled_from_searcher(&searcher))
            .map_err(|err| TantivyGoError::from_err("build sorted search weight", &err.to_string()))
    })?;

    let mut collector = BoundedTopK::new(capacity, descriptor.order);
    for (segment_ord, segment) in searcher.segment_readers().iter().enumerate() {
        ensure_deadline(deadline)?;
        let columns = descriptor.open_segment(segment)?;
        let alive_bitset = segment.alive_bitset();
        let mut scorer = weight.scorer(segment, 1.0).map_err(|err| {
            TantivyGoError::from_err("build sorted search scorer", &err.to_string())
        })?;
        let mut doc = scorer.doc();
        let mut scorer_documents = 0usize;
        while doc != TERMINATED {
            check_deadline_before_document(deadline, &mut scorer_documents)?;
            if alive_bitset
                .map(|alive_bitset| alive_bitset.is_alive(doc))
                .unwrap_or(true)
            {
                let tuple = columns.tuple_for_doc(doc)?;
                if after
                    .as_ref()
                    .map(|cursor| {
                        compare_sort_tuples(&tuple, cursor, descriptor.order) == Ordering::Greater
                    })
                    .unwrap_or(true)
                {
                    collector.push(DocAddress::new(segment_ord as u32, doc), tuple);
                }
            }
            doc = scorer.advance();
        }
    }
    ensure_deadline(deadline)?;

    let mut candidates = collector.into_sorted();
    let has_more = candidates.len() > docs_limit;
    candidates.truncate(docs_limit);

    let mut documents = Vec::with_capacity(candidates.len());
    let mut sort_tuples = Vec::with_capacity(candidates.len());
    for candidate in candidates {
        ensure_deadline(deadline)?;
        let document = searcher
            .doc::<TantivyDocument>(candidate.address)
            .map_err(|err| {
                TantivyGoError::from_err("load sorted search document", &err.to_string())
            })?;
        documents.push(Document {
            tantivy_doc: document,
            highlights: Vec::new(),
            score: 0.0,
        });
        sort_tuples.push(candidate.tuple);
    }

    Ok(Box::into_raw(Box::new(SearchResult::new_sorted(
        documents,
        sort_tuples,
        has_more,
    ))))
}

fn sorted_search_capacity(docs_limit: usize) -> Result<usize, TantivyGoError> {
    if docs_limit == 0 {
        return Err(TantivyGoError(
            "sorted search limit must be greater than zero".to_string(),
        ));
    }
    if docs_limit > MAX_SORTED_SEARCH_LIMIT {
        return Err(TantivyGoError(format!(
            "sorted search limit must not exceed {MAX_SORTED_SEARCH_LIMIT}"
        )));
    }
    docs_limit
        .checked_add(1)
        .ok_or_else(|| TantivyGoError("sorted search limit is too large".to_string()))
}

fn construct_sorted_query(
    query_ptr: *const c_char,
    index: &Index,
    deadline: &SearchDeadline,
) -> Result<Box<dyn Query>, TantivyGoError> {
    ensure_deadline(deadline)?;
    check_deadline_after(deadline, || {
        let query = assert_string(query_ptr)?;
        parse_sorted_query_parser(index, &query)
    })
}

pub fn search_result_has_more(result: &SearchResult) -> bool {
    result.has_more()
}

fn result_sort_tuple(result: &SearchResult, index: usize) -> Result<&SortTuple, TantivyGoError> {
    let tuples = result.sort_tuples().ok_or_else(|| {
        TantivyGoError("sort values are unavailable for this search result".to_string())
    })?;
    tuples.get(index).ok_or_else(|| {
        TantivyGoError(format!(
            "sort values index {index} out of range for {} search results",
            tuples.len()
        ))
    })
}

pub fn search_result_sort_values_len(
    result: &SearchResult,
    index: usize,
) -> Result<usize, TantivyGoError> {
    Ok(result_sort_tuple(result, index)?.len)
}

pub fn search_result_copy_sort_values(
    result: &SearchResult,
    index: usize,
    values_ptr: *mut SortedSearchValue,
    values_len: usize,
) -> Result<(), TantivyGoError> {
    let tuple = result_sort_tuple(result, index)?;
    if values_len != tuple.len {
        return Err(TantivyGoError(format!(
            "sorted search result tuple has {} values, output has {values_len}",
            tuple.len
        )));
    }
    if values_len == 0 {
        return Ok(());
    }
    if values_ptr.is_null() {
        return Err(TantivyGoError(
            "sorted search output values pointer is null".to_string(),
        ));
    }

    let values = unsafe { slice::from_raw_parts_mut(values_ptr, values_len) };
    for (output, atom) in values.iter_mut().zip(tuple.atoms()) {
        *output = sort_atom_to_ffi(atom);
    }
    Ok(())
}

fn ffi_slice<'a, T>(ptr: *const T, len: usize, name: &str) -> Result<&'a [T], TantivyGoError> {
    if len == 0 {
        return Ok(&[]);
    }
    if ptr.is_null() {
        return Err(TantivyGoError(format!("{name} pointer is null")));
    }
    Ok(unsafe { slice::from_raw_parts(ptr, len) })
}

fn sort_atom_from_ffi(
    value: &SortedSearchValue,
    expected_kind: SortValueKind,
) -> Result<SortAtom, TantivyGoError> {
    let kind = SortValueKind::from_ffi(value.kind)?;
    if kind != expected_kind {
        return Err(TantivyGoError(format!(
            "sorted search after value kind {kind:?} does not match field kind {expected_kind:?}"
        )));
    }
    if value.missing {
        return Ok(SortAtom::Missing(kind));
    }

    match kind {
        SortValueKind::Text => {
            let bytes = if value.text_len == 0 {
                &[]
            } else {
                if value.text_ptr.is_null() {
                    return Err(TantivyGoError(
                        "sorted search text after pointer is null".to_string(),
                    ));
                }
                unsafe { slice::from_raw_parts(value.text_ptr.cast::<u8>(), value.text_len) }
            };
            let text = std::str::from_utf8(bytes).map_err(|err| {
                TantivyGoError::from_err("invalid sorted search text after value", &err.to_string())
            })?;
            Ok(SortAtom::Text(text.to_owned()))
        }
        SortValueKind::U64 => Ok(SortAtom::U64(value.u64_value)),
        SortValueKind::I64 => Ok(SortAtom::I64(value.i64_value)),
        SortValueKind::F64 => {
            if value.f64_value.is_nan() {
                return Err(TantivyGoError(
                    "F64 sorted search after value cannot be NaN".to_string(),
                ));
            }
            Ok(SortAtom::F64(value.f64_value))
        }
        SortValueKind::Bool => Ok(SortAtom::Bool(value.bool_value)),
        SortValueKind::Date => Ok(SortAtom::Date(value.i64_value)),
    }
}

fn sort_atom_to_ffi(atom: &SortAtom) -> SortedSearchValue {
    let mut value = SortedSearchValue {
        kind: atom.kind().as_ffi(),
        missing: atom.is_missing(),
        text_ptr: std::ptr::null(),
        text_len: 0,
        u64_value: 0,
        i64_value: 0,
        f64_value: 0.0,
        bool_value: false,
    };
    match atom {
        SortAtom::Missing(_) => {}
        SortAtom::Text(text) => {
            value.text_ptr = text.as_ptr().cast::<c_char>();
            value.text_len = text.len();
        }
        SortAtom::U64(number) => value.u64_value = *number,
        SortAtom::I64(number) | SortAtom::Date(number) => value.i64_value = *number,
        SortAtom::F64(number) => value.f64_value = *number,
        SortAtom::Bool(boolean) => value.bool_value = *boolean,
    }
    value
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tantivy_util::DOCUMENT_BUDGET_BYTES;

    use std::ffi::CString;
    use tantivy::collector::Count;
    use tantivy::indexer::NoMergePolicy;
    use tantivy::schema::{DateOptions, DateTimePrecision, Schema, FAST, INDEXED, STORED, STRING};
    use tantivy::{doc, ReloadPolicy};

    fn query_test_context() -> TantivyContext {
        let mut schema_builder = Schema::builder();
        let keyword = schema_builder.add_text_field("keyword", STRING | FAST | STORED);
        let sort = schema_builder.add_u64_field("sort", FAST | INDEXED | STORED);
        let u64v = schema_builder.add_u64_field("u64v", FAST | INDEXED | STORED);
        let i64v = schema_builder.add_i64_field("i64v", FAST | INDEXED | STORED);
        let f64v = schema_builder.add_f64_field("f64v", FAST | INDEXED | STORED);
        let boolv = schema_builder.add_bool_field("boolv", FAST | INDEXED | STORED);
        let datev = schema_builder.add_date_field(
            "datev",
            DateOptions::default()
                .set_fast()
                .set_indexed()
                .set_stored()
                .set_precision(DateTimePrecision::Milliseconds),
        );
        let index = tantivy::Index::create_in_ram(schema_builder.build());
        let mut writer = index
            .writer_with_num_threads(1, DOCUMENT_BUDGET_BYTES)
            .expect("create test index writer");
        writer.set_merge_policy(Box::new(NoMergePolicy));
        writer
            .add_document(doc!(
                keyword => "alpha",
                sort => 2u64,
                u64v => 42u64,
                i64v => -7i64,
                f64v => 3.5f64,
                boolv => true,
                datev => tantivy::DateTime::from_timestamp_millis(1_704_067_200_000),
            ))
            .expect("add first test document");
        writer.commit().expect("commit first test segment");
        writer
            .add_document(doc!(
                keyword => "beta",
                sort => 1u64,
                u64v => 99u64,
                i64v => 8i64,
                f64v => 4.5f64,
                boolv => false,
                datev => tantivy::DateTime::from_timestamp_millis(1_704_153_600_000),
            ))
            .expect("add second test document");
        writer.commit().expect("commit second test segment");

        let reader = index
            .reader_builder()
            .reload_policy(ReloadPolicy::Manual)
            .try_into()
            .expect("create test index reader");
        TantivyContext::new(index, writer, reader)
    }

    fn strict_query_count(context: &mut TantivyContext, source: &str) -> usize {
        let query = parse_sorted_query_parser(&context.index, source).expect("parse strict query");
        let searcher = context.reader().expect("reload test reader").searcher();
        searcher
            .search(&*query, &Count)
            .expect("execute strict query")
    }

    fn expected_error<T>(result: Result<T, TantivyGoError>) -> TantivyGoError {
        match result {
            Ok(_) => panic!("expected sorted search error"),
            Err(error) => error,
        }
    }

    fn assert_timeout<T>(result: Result<T, TantivyGoError>) {
        assert_eq!(expected_error(result).to_string(), SEARCH_TIMEOUT_ERROR);
    }

    fn operation_error_after_deadline_check(
        deadline: &SearchDeadline,
    ) -> Result<(), TantivyGoError> {
        ensure_deadline(deadline)?;
        Err(TantivyGoError("operation error".to_string()))
    }

    fn sorted_result_with_deadline(
        context: &mut TantivyContext,
        deadline: &SearchDeadline,
    ) -> Result<Box<SearchResult>, TantivyGoError> {
        let query = CString::new("*").expect("query-parser all-query source");
        let sort_name = CString::new("sort").expect("sort field name");
        let fields = [SortedSearchField {
            name_ptr: sort_name.as_ptr(),
            direction: 1,
        }];
        let result = search_sorted_with_deadline(
            query.as_ptr(),
            fields.as_ptr(),
            fields.len(),
            std::ptr::null(),
            0,
            10,
            sorted_search_capacity(10).expect("test sorted search capacity"),
            deadline,
            context,
        )?;
        Ok(unsafe { Box::from_raw(result) })
    }

    fn tuple(atoms: Vec<SortAtom>) -> SortTuple {
        let mut tuple = SortTuple::new(atoms.len());
        for (slot, atom) in tuple.atoms.iter_mut().zip(atoms) {
            *slot = atom;
        }
        tuple
    }

    fn order(directions: &[SortDirection]) -> SortOrder {
        let mut values = [SortDirection::Ascending; MAX_SORT_FIELDS];
        for (slot, direction) in values.iter_mut().zip(directions) {
            *slot = *direction;
        }
        SortOrder {
            directions: values,
            len: directions.len(),
        }
    }

    #[test]
    fn compares_lexicographic_mixed_directions() {
        let order = order(&[SortDirection::Ascending, SortDirection::Descending]);
        let first = tuple(vec![SortAtom::U64(1), SortAtom::Text("z".to_string())]);
        let second = tuple(vec![SortAtom::U64(1), SortAtom::Text("a".to_string())]);
        let third = tuple(vec![SortAtom::U64(2), SortAtom::Text("z".to_string())]);

        assert_eq!(compare_sort_tuples(&first, &second, order), Ordering::Less);
        assert_eq!(compare_sort_tuples(&second, &third, order), Ordering::Less);
    }

    #[test]
    fn puts_missing_last_for_both_directions() {
        let present_low = tuple(vec![SortAtom::I64(-1)]);
        let present_high = tuple(vec![SortAtom::I64(1)]);
        let missing = tuple(vec![SortAtom::Missing(SortValueKind::I64)]);

        let ascending = order(&[SortDirection::Ascending]);
        assert_eq!(
            compare_sort_tuples(&present_low, &present_high, ascending),
            Ordering::Less
        );
        assert_eq!(
            compare_sort_tuples(&present_high, &missing, ascending),
            Ordering::Less
        );

        let descending = order(&[SortDirection::Descending]);
        assert_eq!(
            compare_sort_tuples(&present_high, &present_low, descending),
            Ordering::Less
        );
        assert_eq!(
            compare_sort_tuples(&present_low, &missing, descending),
            Ordering::Less
        );
    }

    #[test]
    fn excludes_cursor_tuple_strictly() {
        let order = order(&[SortDirection::Ascending]);
        let cursor = tuple(vec![SortAtom::U64(4)]);
        let equal = tuple(vec![SortAtom::U64(4)]);
        let before = tuple(vec![SortAtom::U64(3)]);
        let after = tuple(vec![SortAtom::U64(5)]);

        assert_ne!(
            compare_sort_tuples(&equal, &cursor, order),
            Ordering::Greater
        );
        assert_ne!(
            compare_sort_tuples(&before, &cursor, order),
            Ordering::Greater
        );
        assert_eq!(
            compare_sort_tuples(&after, &cursor, order),
            Ordering::Greater
        );
    }

    #[test]
    fn orders_f64_boundaries_with_total_ordering() {
        let order = order(&[SortDirection::Ascending]);
        let values = [
            f64::NEG_INFINITY,
            -f64::MAX,
            -0.0,
            0.0,
            f64::MAX,
            f64::INFINITY,
        ];
        let tuples: Vec<_> = values
            .into_iter()
            .map(|value| tuple(vec![SortAtom::F64(value)]))
            .collect();

        for pair in tuples.windows(2) {
            assert_eq!(
                compare_sort_tuples(&pair[0], &pair[1], order),
                Ordering::Less
            );
        }
    }

    #[test]
    fn reports_already_expired_deadline() {
        let deadline = SearchDeadline::Instant(Instant::now() - Duration::from_nanos(1));
        let error = ensure_deadline(&deadline).expect_err("deadline must be expired");
        assert_eq!(error.to_string(), SEARCH_TIMEOUT_ERROR);
    }

    #[test]
    fn ignores_non_live_json_column_kinds() {
        let mut kind = None;
        merge_live_json_column_kind(&mut kind, ColumnType::I64, false, "payload.active")
            .expect("a deleted JSON type must not affect the sort kind");
        merge_live_json_column_kind(&mut kind, ColumnType::Bool, true, "payload.active")
            .expect("the live JSON type must be accepted");

        assert_eq!(kind, Some(SortValueKind::Bool));
    }

    #[test]
    fn checks_deadline_before_alive_filter_for_every_checkpoint() {
        let deadline = SearchDeadline::Instant(Instant::now() - Duration::from_nanos(1));
        let mut scorer_documents = DEADLINE_CHECK_INTERVAL;
        let error = check_deadline_before_document(&deadline, &mut scorer_documents)
            .expect_err("the checkpoint must stop before inspecting liveness");

        assert_eq!(error.to_string(), SEARCH_TIMEOUT_ERROR);
    }

    #[test]
    fn reports_expiry_during_json_kind_resolution() {
        let deadline = SearchDeadline::Instant(Instant::now() - Duration::from_nanos(1));
        let error = any_live_column_value(0u32..1, &ColumnIndex::Empty { num_docs: 1 }, &deadline)
            .expect_err("kind resolution must check before inspecting live documents");

        assert_eq!(error.to_string(), SEARCH_TIMEOUT_ERROR);
    }
    #[test]
    fn query_parser_requires_explicit_field() {
        let context = query_test_context();
        let error = expected_error(parse_sorted_query_parser(&context.index, "alpha"));

        assert!(error
            .to_string()
            .contains("No default field declared and no field specified in query"));
    }

    #[test]
    fn query_parser_enables_keyword_regexes() {
        let mut context = query_test_context();

        assert_eq!(strict_query_count(&mut context, "keyword:/alpha.*/"), 1);
    }

    #[test]
    fn query_parser_reports_parser_failures() {
        let context = query_test_context();
        let error = expected_error(parse_sorted_query_parser(&context.index, "keyword:("));

        assert!(error.to_string().contains("parse sorted search query"));
    }

    #[test]
    fn query_parser_matches_scalar_schema_values() {
        let mut context = query_test_context();
        let queries = [
            "keyword:alpha",
            "u64v:42",
            "i64v:-7",
            "f64v:3.5",
            "boolv:true",
            r#"datev:"2024-01-01T00:00:00Z""#,
        ];

        for source in queries {
            assert_eq!(
                strict_query_count(&mut context, source),
                1,
                "query {source:?} must match the typed field"
            );
        }
    }

    #[test]
    fn post_operation_deadline_check_prioritizes_timeout_over_operation_error() {
        let expired = SearchDeadline::test_after_successful_checks(1);
        assert_timeout(check_deadline_after(&expired, || {
            operation_error_after_deadline_check(&expired)
        }));

        let active = SearchDeadline::test_after_successful_checks(2);
        let error = expected_error(check_deadline_after(&active, || {
            operation_error_after_deadline_check(&active)
        }));
        assert_eq!(error.to_string(), "operation error");
    }

    #[test]
    fn query_construction_error_observes_the_post_operation_deadline() {
        let context = query_test_context();
        let invalid_query = CString::new("keyword:(").expect("invalid parser source");

        let expired = SearchDeadline::test_after_successful_checks(1);
        assert_timeout(construct_sorted_query(
            invalid_query.as_ptr(),
            &context.index,
            &expired,
        ));

        let active = SearchDeadline::test_after_successful_checks(2);
        let error = expected_error(construct_sorted_query(
            invalid_query.as_ptr(),
            &context.index,
            &active,
        ));
        assert!(error.to_string().contains("parse sorted search query"));
    }

    #[test]
    fn checks_deadline_before_and_after_query_construction() {
        let context = query_test_context();
        let query = CString::new("*").expect("query-parser all-query source");

        let deadline = SearchDeadline::test_after_successful_checks(0);
        assert_timeout(construct_sorted_query(
            query.as_ptr(),
            &context.index,
            &deadline,
        ));

        let deadline = SearchDeadline::test_after_successful_checks(1);
        assert_timeout(construct_sorted_query(
            query.as_ptr(),
            &context.index,
            &deadline,
        ));
    }

    #[test]
    fn checks_deadline_during_weighting_collection_and_result_loading() {
        let checkpoints = [
            ("before weight creation", 2),
            ("after weight creation", 3),
            ("before document liveness", 5),
            ("between segments", 6),
            ("before result document loading", 9),
        ];

        for (boundary, successful_checks) in checkpoints {
            let mut context = query_test_context();
            assert_eq!(
                context
                    .reader()
                    .expect("reload test reader")
                    .searcher()
                    .segment_readers()
                    .len(),
                2,
                "test fixture must retain two segments"
            );
            let deadline = SearchDeadline::test_after_successful_checks(successful_checks);
            let error = expected_error(sorted_result_with_deadline(&mut context, &deadline));
            assert_eq!(
                error.to_string(),
                SEARCH_TIMEOUT_ERROR,
                "deadline must stop at {boundary}"
            );
        }
    }
}
