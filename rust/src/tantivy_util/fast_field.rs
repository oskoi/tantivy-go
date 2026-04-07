use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine;
use std::collections::HashMap;
use tantivy::schema::{Field, Schema};
use tantivy::{DocAddress, Searcher};

use crate::tantivy_util::TantivyGoError;

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

    let field_name = schema.get_field_name(field);

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

#[cfg(test)]
mod tests {
    use std::collections::HashSet;

    use tantivy::collector::TopDocs;
    use tantivy::query::AllQuery;
    use tantivy::schema::{Schema, FAST};
    use tantivy::{doc, Index, ReloadPolicy};

    use super::read_fast_field_values;

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
}
