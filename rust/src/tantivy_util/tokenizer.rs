use crate::tantivy_util::stemmer::create_stemmer;
use crate::tantivy_util::{EdgeNgramTokenizer, TantivyGoError};
use tantivy::tokenizer::{
    AsciiFoldingFilter, LowerCaser, NgramTokenizer, RawTokenizer, RemoveLongFilter,
    SimpleTokenizer, TextAnalyzer,
};
use tantivy::Index;

fn register_tokenizer(index: &Index, tokenizer_name: &str, text_analyzer: TextAnalyzer) {
    index.tokenizers().register(tokenizer_name, text_analyzer)
}

pub fn register_edge_ngram_tokenizer(
    min_gram: usize,
    max_gram: usize,
    limit: usize,
    index: &Index,
    tokenizer_name: &str,
) {
    let text_analyzer = TextAnalyzer::builder(EdgeNgramTokenizer::new(min_gram, max_gram, limit))
        .filter(LowerCaser)
        .filter(AsciiFoldingFilter)
        .build();

    register_tokenizer(index, tokenizer_name, text_analyzer);
}

pub fn register_simple_tokenizer(
    text_limit: usize,
    index: &Index,
    tokenizer_name: &str,
    lang: &str,
) -> Result<(), TantivyGoError> {
    let stemmer = create_stemmer(lang)?;
    let text_analyzer = TextAnalyzer::builder(SimpleTokenizer::default())
        .filter(RemoveLongFilter::limit(text_limit))
        .filter(LowerCaser)
        .filter(AsciiFoldingFilter)
        .filter(stemmer)
        .build();

    register_tokenizer(index, tokenizer_name, text_analyzer);
    Ok(())
}

pub fn register_raw_tokenizer(index: &Index, tokenizer_name: &str) {
    let text_analyzer = TextAnalyzer::builder(RawTokenizer::default()).build();
    register_tokenizer(index, tokenizer_name, text_analyzer);
}

pub fn register_ngram_tokenizer(
    min_gram: usize,
    max_gram: usize,
    prefix_only: bool,
    index: &Index,
    tokenizer_name: &str,
) -> Result<(), TantivyGoError> {
    let tokenizer = NgramTokenizer::new(min_gram, max_gram, prefix_only)
        .map_err(|e| TantivyGoError::from_err("ngram tokenizer", &e.to_string()))?;

    let text_analyzer = TextAnalyzer::builder(tokenizer)
        .filter(LowerCaser)
        .filter(AsciiFoldingFilter)
        .build();

    register_tokenizer(index, tokenizer_name, text_analyzer);
    Ok(())
}
