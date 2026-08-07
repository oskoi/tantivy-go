use super::util::TantivyGoError;
use crate::c_util::sorted_search::SortTuple;
use serde::Serialize;
use tantivy::{Index, IndexReader, IndexWriter, TantivyDocument};

pub struct TantivyContext {
    pub index: Index,
    pub writer: IndexWriter,
    reader: IndexReader,
}

impl TantivyContext {
    pub fn new(index: Index, writer: IndexWriter, reader: IndexReader) -> TantivyContext {
        TantivyContext {
            index,
            writer,
            reader,
        }
    }

    pub fn reader(&mut self) -> Result<&IndexReader, TantivyGoError> {
        self.reader
            .reload()
            .map_err(|err| TantivyGoError::from_err("Reload index reader", &err.to_string()))?;
        Ok(&self.reader)
    }
}

#[derive(Clone)]
pub struct Document {
    pub tantivy_doc: TantivyDocument,
    pub highlights: Vec<Highlight>,
    pub score: f32,
}

#[derive(Clone, Serialize)]
pub struct Highlight {
    pub field_name: String,
    pub fragment: Fragment,
}

#[derive(Clone, Serialize)]
pub struct Fragment {
    pub t: String,              //to comply with bleve temporarily
    pub r: Vec<(usize, usize)>, //to comply with bleve temporarily
}

pub struct SearchResult {
    pub documents: Vec<Document>,
    pub size: usize,
    sort_tuples: Option<Vec<SortTuple>>,
    has_more: bool,
}

impl SearchResult {
    pub(crate) fn new(documents: Vec<Document>) -> Self {
        let size = documents.len();
        Self {
            documents,
            size,
            sort_tuples: None,
            has_more: false,
        }
    }

    pub(crate) fn new_sorted(
        documents: Vec<Document>,
        sort_tuples: Vec<SortTuple>,
        has_more: bool,
    ) -> Self {
        debug_assert_eq!(documents.len(), sort_tuples.len());
        let size = documents.len();
        Self {
            documents,
            size,
            sort_tuples: Some(sort_tuples),
            has_more,
        }
    }

    pub(crate) fn has_more(&self) -> bool {
        self.has_more
    }

    pub(crate) fn sort_tuples(&self) -> Option<&[SortTuple]> {
        self.sort_tuples.as_deref()
    }
}
