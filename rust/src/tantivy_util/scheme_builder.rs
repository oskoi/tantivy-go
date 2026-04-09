use tantivy::schema::{
    BytesOptions, DateOptions, IndexRecordOption, JsonObjectOptions, NumericOptions, SchemaBuilder,
    TextFieldIndexing, FAST, STORED, STRING, TEXT,
};

pub fn add_text_field(
    stored: bool,
    is_text: bool,
    is_fast: bool,
    builder: &mut SchemaBuilder,
    tokenizer_name: &str,
    field_name: &str,
    index_record_option: IndexRecordOption,
) -> u32 {
    let mut text_options = if is_text { TEXT } else { STRING };
    text_options = if stored {
        text_options | STORED
    } else {
        text_options
    };
    text_options = if is_fast {
        text_options | FAST
    } else {
        text_options
    };
    text_options = text_options.set_indexing_options(
        TextFieldIndexing::default()
            .set_tokenizer(tokenizer_name)
            .set_index_option(index_record_option),
    );
    builder.add_text_field(field_name, text_options).field_id()
}

fn numeric_field_options(stored: bool, is_fast: bool, is_indexed: bool) -> NumericOptions {
    let mut options = NumericOptions::default();
    if stored {
        options = options.set_stored();
    }
    if is_fast {
        options = options.set_fast();
    }
    if is_indexed {
        options = options.set_indexed();
    }
    options
}

pub fn add_schema_u64_field(
    stored: bool,
    is_fast: bool,
    is_indexed: bool,
    builder: &mut SchemaBuilder,
    field_name: &str,
) -> u32 {
    let options = numeric_field_options(stored, is_fast, is_indexed);
    builder.add_u64_field(field_name, options).field_id()
}

pub fn add_schema_i64_field(
    stored: bool,
    is_fast: bool,
    is_indexed: bool,
    builder: &mut SchemaBuilder,
    field_name: &str,
) -> u32 {
    let options = numeric_field_options(stored, is_fast, is_indexed);
    builder.add_i64_field(field_name, options).field_id()
}

pub fn add_schema_f64_field(
    stored: bool,
    is_fast: bool,
    is_indexed: bool,
    builder: &mut SchemaBuilder,
    field_name: &str,
) -> u32 {
    let options = numeric_field_options(stored, is_fast, is_indexed);
    builder.add_f64_field(field_name, options).field_id()
}

fn date_field_options(stored: bool, is_fast: bool, is_indexed: bool) -> DateOptions {
    let mut options = DateOptions::default();
    if stored {
        options = options.set_stored();
    }
    if is_fast {
        options = options.set_fast();
    }
    if is_indexed {
        options = options.set_indexed();
    }
    options
}

pub fn add_schema_date_field(
    stored: bool,
    is_fast: bool,
    is_indexed: bool,
    builder: &mut SchemaBuilder,
    field_name: &str,
) -> u32 {
    let options = date_field_options(stored, is_fast, is_indexed);
    builder.add_date_field(field_name, options).field_id()
}

fn bytes_field_options(stored: bool, is_fast: bool, is_indexed: bool) -> BytesOptions {
    let mut options = BytesOptions::default();
    if stored {
        options = options.set_stored();
    }
    if is_fast {
        options = options.set_fast();
    }
    if is_indexed {
        options = options.set_indexed();
    }
    options
}

pub fn add_schema_bytes_field(
    stored: bool,
    is_fast: bool,
    is_indexed: bool,
    builder: &mut SchemaBuilder,
    field_name: &str,
) -> u32 {
    let options = bytes_field_options(stored, is_fast, is_indexed);
    builder.add_bytes_field(field_name, options).field_id()
}

pub fn add_schema_json_field(
    stored: bool,
    is_fast: bool,
    is_indexed: bool,
    builder: &mut SchemaBuilder,
    field_name: &str,
    index_tokenizer_name: &str,
    fast_tokenizer_name: Option<&str>,
    index_record_option: IndexRecordOption,
    expand_dots_enabled: bool,
) -> u32 {
    let mut options = JsonObjectOptions::default();
    if stored {
        options = options.set_stored();
    }
    if is_fast {
        options = options.set_fast(fast_tokenizer_name);
    }
    if is_indexed {
        options = options.set_indexing_options(
            TextFieldIndexing::default()
                .set_tokenizer(index_tokenizer_name)
                .set_index_option(index_record_option),
        );
    }
    if expand_dots_enabled {
        options = options.set_expand_dots_enabled();
    }

    builder.add_json_field(field_name, options).field_id()
}
