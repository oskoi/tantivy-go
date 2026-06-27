use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine;
use std::collections::HashMap;
use tantivy::schema::{Field, Schema};
use tantivy::{DocAddress, Searcher};

use crate::tantivy_util::TantivyGoError;

fn fast_field_name<'a>(schema: &'a Schema, field: Field) -> Result<&'a str, TantivyGoError> {
    let field_id = field.field_id() as usize;
    if field_id >= schema.num_fields() {
        return Err(TantivyGoError(format!(
            "fast field id {} out of range for {} schema fields",
            field_id,
            schema.num_fields()
        )));
    }
    Ok(schema.get_field_name(field))
}

/// Reads fast field values for doc addresses, grouped by segment for efficiency.
pub fn read_fast_field_values(
    searcher: &Searcher,
    schema: &Schema,
    field: Field,
    doc_addresses: &[DocAddress],
) -> Result<Vec<Option<String>>, TantivyGoError> {
    if doc_addresses.is_empty() {
        return Ok(vec![]);
    }

    let field_name = fast_field_name(schema, field)?;

    let mut segment_groups: HashMap<u32, Vec<(usize, u32)>> = HashMap::new();
    for (idx, addr) in doc_addresses.iter().enumerate() {
        segment_groups
            .entry(addr.segment_ord)
            .or_default()
            .push((idx, addr.doc_id));
    }

    let mut results: Vec<Option<String>> = vec![None; doc_addresses.len()];
    let mut buffer = String::new();

    for (segment_ord, docs) in segment_groups {
        let segment_reader = searcher.segment_reader(segment_ord);
        let fast_fields = segment_reader.fast_fields();

        if let Some(str_column) = fast_fields.str(field_name).map_err(|e| {
            TantivyGoError::from_err(
                &format!("Failed to get fast field '{}'", field_name),
                &e.to_string(),
            )
        })? {
            for (result_idx, doc_id) in docs {
                buffer.clear();
                if let Some(ord) = str_column.term_ords(doc_id).next() {
                    if str_column.ord_to_str(ord, &mut buffer).is_ok() && !buffer.is_empty() {
                        results[result_idx] = Some(buffer.clone());
                    }
                }
            }
            continue;
        }

        if let Some(bytes_column) = fast_fields.bytes(field_name).map_err(|e| {
            TantivyGoError::from_err(
                &format!("Failed to get fast field '{}'", field_name),
                &e.to_string(),
            )
        })? {
            let mut bytes_buffer = Vec::new();
            for (result_idx, doc_id) in docs {
                bytes_buffer.clear();
                if let Some(ord) = bytes_column.term_ords(doc_id).next() {
                    if bytes_column.ord_to_bytes(ord, &mut bytes_buffer).is_ok()
                        && !bytes_buffer.is_empty()
                    {
                        results[result_idx] = Some(BASE64.encode(&bytes_buffer));
                    }
                }
            }
            continue;
        }

        return Err(TantivyGoError(format!(
            "Fast field '{}' not found in segment {}",
            field_name, segment_ord
        )));
    }

    Ok(results)
}

fn read_typed_fast_field_values<T, F>(
    searcher: &Searcher,
    schema: &Schema,
    field: Field,
    doc_addresses: &[DocAddress],
    open_column: F,
) -> Result<Vec<Option<T>>, TantivyGoError>
where
    T: Copy + PartialOrd + std::fmt::Debug + Send + Sync + 'static,
    F: Fn(
        &tantivy::fastfield::FastFieldReaders,
        &str,
    ) -> tantivy::Result<tantivy::fastfield::Column<T>>,
{
    if doc_addresses.is_empty() {
        return Ok(vec![]);
    }

    let field_name = fast_field_name(schema, field)?;
    let mut results: Vec<Option<T>> = vec![None; doc_addresses.len()];

    let mut segment_groups: HashMap<u32, Vec<(usize, u32)>> = HashMap::new();
    for (idx, addr) in doc_addresses.iter().enumerate() {
        segment_groups
            .entry(addr.segment_ord)
            .or_default()
            .push((idx, addr.doc_id));
    }

    for (segment_ord, docs) in segment_groups {
        let segment_reader = searcher.segment_reader(segment_ord);
        let column = open_column(segment_reader.fast_fields(), field_name).map_err(|e| {
            TantivyGoError::from_err(
                &format!("Failed to get fast field '{}'", field_name),
                &e.to_string(),
            )
        })?;

        for (result_idx, doc_id) in docs {
            results[result_idx] = column.first(doc_id);
        }
    }

    Ok(results)
}

pub fn read_u64_fast_field_values(
    searcher: &Searcher,
    schema: &Schema,
    field: Field,
    doc_addresses: &[DocAddress],
) -> Result<Vec<Option<u64>>, TantivyGoError> {
    read_typed_fast_field_values(
        searcher,
        schema,
        field,
        doc_addresses,
        |fast_fields, field_name| fast_fields.u64(field_name),
    )
}

pub fn read_i64_fast_field_values(
    searcher: &Searcher,
    schema: &Schema,
    field: Field,
    doc_addresses: &[DocAddress],
) -> Result<Vec<Option<i64>>, TantivyGoError> {
    read_typed_fast_field_values(
        searcher,
        schema,
        field,
        doc_addresses,
        |fast_fields, field_name| fast_fields.i64(field_name),
    )
}

pub fn read_f64_fast_field_values(
    searcher: &Searcher,
    schema: &Schema,
    field: Field,
    doc_addresses: &[DocAddress],
) -> Result<Vec<Option<f64>>, TantivyGoError> {
    read_typed_fast_field_values(
        searcher,
        schema,
        field,
        doc_addresses,
        |fast_fields, field_name| fast_fields.f64(field_name),
    )
}

pub fn read_date_fast_field_values(
    searcher: &Searcher,
    schema: &Schema,
    field: Field,
    doc_addresses: &[DocAddress],
) -> Result<Vec<Option<i64>>, TantivyGoError> {
    let values = read_typed_fast_field_values(
        searcher,
        schema,
        field,
        doc_addresses,
        |fast_fields, field_name| fast_fields.date(field_name),
    )?;
    Ok(values
        .into_iter()
        .map(|value| value.map(|date| date.into_timestamp_millis()))
        .collect())
}

#[cfg(test)]
mod tests {
    use std::collections::HashSet;

    use tantivy::collector::TopDocs;
    use tantivy::query::AllQuery;
    use tantivy::schema::{DateOptions, DateTimePrecision, Field, Schema, FAST, STORED};
    use tantivy::{doc, DateTime, Index, ReloadPolicy};

    use super::{
        read_date_fast_field_values, read_f64_fast_field_values, read_fast_field_values,
        read_i64_fast_field_values, read_u64_fast_field_values,
    };

    #[test]
    fn reads_bytes_fast_field_as_base64() {
        let mut schema_builder = Schema::builder();
        let blob_field = schema_builder.add_bytes_field("blob", FAST);
        let schema = schema_builder.build();

        let index = Index::create_in_ram(schema.clone());
        let mut writer = index.writer(15_000_000).unwrap();
        writer
            .add_document(doc!(blob_field => vec![0u8, 1u8, 2u8]))
            .unwrap();
        writer
            .add_document(doc!(blob_field => vec![255u8, 254u8]))
            .unwrap();
        writer.commit().unwrap();

        let reader = index
            .reader_builder()
            .reload_policy(ReloadPolicy::Manual)
            .try_into()
            .unwrap();
        reader.reload().unwrap();
        let searcher = reader.searcher();

        let top_docs = searcher
            .search(&AllQuery, &TopDocs::with_limit(10).order_by_score())
            .unwrap();
        let doc_addresses: Vec<_> = top_docs.into_iter().map(|(_, addr)| addr).collect();

        let values =
            read_fast_field_values(&searcher, &schema, blob_field, &doc_addresses).unwrap();

        let non_empty: HashSet<String> = values.into_iter().flatten().collect();
        assert_eq!(non_empty.len(), 2);
        assert!(non_empty.contains("AAEC"));
        assert!(non_empty.contains("//4="));
    }

    #[test]
    fn reads_numeric_and_date_fast_fields() {
        let mut schema_builder = Schema::builder();
        let u64_field = schema_builder.add_u64_field("u64v", FAST | STORED);
        let i64_field = schema_builder.add_i64_field("i64v", FAST | STORED);
        let f64_field = schema_builder.add_f64_field("f64v", FAST | STORED);
        let date_field = schema_builder.add_date_field(
            "datev",
            DateOptions::default()
                .set_fast()
                .set_stored()
                .set_precision(DateTimePrecision::Milliseconds),
        );
        let schema = schema_builder.build();

        let index = Index::create_in_ram(schema.clone());
        let mut writer = index.writer(15_000_000).unwrap();
        writer
            .add_document(doc!(
                u64_field => 42u64,
                i64_field => -7i64,
                f64_field => 3.5f64,
                date_field => DateTime::from_timestamp_millis(1_700_000_000_123),
            ))
            .unwrap();
        writer.commit().unwrap();

        let reader = index.reader().unwrap();
        reader.reload().unwrap();
        let searcher = reader.searcher();
        let top_docs = searcher
            .search(&AllQuery, &TopDocs::with_limit(1).order_by_score())
            .unwrap();
        let doc_addresses: Vec<_> = top_docs.into_iter().map(|(_, addr)| addr).collect();

        assert_eq!(
            read_u64_fast_field_values(&searcher, &schema, u64_field, &doc_addresses).unwrap(),
            vec![Some(42)]
        );
        assert_eq!(
            read_i64_fast_field_values(&searcher, &schema, i64_field, &doc_addresses).unwrap(),
            vec![Some(-7)]
        );
        assert_eq!(
            read_f64_fast_field_values(&searcher, &schema, f64_field, &doc_addresses).unwrap(),
            vec![Some(3.5)]
        );
        assert_eq!(
            read_date_fast_field_values(&searcher, &schema, date_field, &doc_addresses).unwrap(),
            vec![Some(1_700_000_000_123)]
        );
    }

    #[test]
    fn rejects_invalid_fast_field_id() {
        let mut schema_builder = Schema::builder();
        let u64_field = schema_builder.add_u64_field("u64v", FAST | STORED);
        let schema = schema_builder.build();

        let index = Index::create_in_ram(schema.clone());
        let mut writer = index.writer(15_000_000).unwrap();
        writer.add_document(doc!(u64_field => 42u64)).unwrap();
        writer.commit().unwrap();

        let reader = index.reader().unwrap();
        reader.reload().unwrap();
        let searcher = reader.searcher();
        let top_docs = searcher
            .search(&AllQuery, &TopDocs::with_limit(1).order_by_score())
            .unwrap();
        let doc_addresses: Vec<_> = top_docs.into_iter().map(|(_, addr)| addr).collect();

        let err = read_u64_fast_field_values(
            &searcher,
            &schema,
            Field::from_field_id(99),
            &doc_addresses,
        )
        .unwrap_err();
        assert!(err.to_string().contains("fast field id 99 out of range"));
    }
}
