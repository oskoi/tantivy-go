#include "binding_typedefs.h"
#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

#define DOCUMENT_BUDGET_BYTES 50000000

typedef struct Document Document;

typedef struct SearchResult SearchResult;

typedef struct TantivyContext TantivyContext;

typedef struct SortedSearchField {
  /**
   * Input field-name bytes are borrowed only for the duration of a sorted search;
   * they remain caller-owned and must not be retained.
   */
  const char *name_ptr;
  uint8_t direction;
} SortedSearchField;

typedef struct SortedSearchValue {
  uint8_t kind;
  bool missing;
  /**
   * For input cursors, text bytes are borrowed only for
   * the duration of a sorted search; they remain caller-owned.
   * For output tuples, this borrows `SearchResult` storage until `search_result_free`;
   * Go copies it first.
   */
  const char *text_ptr;
  uintptr_t text_len;
  uint64_t u64_value;
  int64_t i64_value;
  double f64_value;
  bool bool_value;
} SortedSearchValue;

SchemaBuilder *schema_builder_new(void);

uint32_t schema_builder_add_text_field(SchemaBuilder *builder_ptr,
                                       const char *field_name_ptr,
                                       bool stored,
                                       bool is_text,
                                       bool is_fast,
                                       bool is_indexed,
                                       uintptr_t index_record_option_const,
                                       const char *tokenizer_name_ptr,
                                       char **error_buffer);

Schema *schema_builder_build(SchemaBuilder *builder_ptr, char **error_buffer);

void schema_free(Schema *schema_ptr);

uint32_t schema_builder_add_u64_field(SchemaBuilder *builder_ptr,
                                      const char *field_name_ptr,
                                      bool stored,
                                      bool is_fast,
                                      bool is_indexed,
                                      char **error_buffer);

uint32_t schema_builder_add_i64_field(SchemaBuilder *builder_ptr,
                                      const char *field_name_ptr,
                                      bool stored,
                                      bool is_fast,
                                      bool is_indexed,
                                      char **error_buffer);

uint32_t schema_builder_add_f64_field(SchemaBuilder *builder_ptr,
                                      const char *field_name_ptr,
                                      bool stored,
                                      bool is_fast,
                                      bool is_indexed,
                                      char **error_buffer);

uint32_t schema_builder_add_date_field(SchemaBuilder *builder_ptr,
                                       const char *field_name_ptr,
                                       bool stored,
                                       bool is_fast,
                                       bool is_indexed,
                                       char **error_buffer);

uint32_t schema_builder_add_bytes_field(SchemaBuilder *builder_ptr,
                                        const char *field_name_ptr,
                                        bool stored,
                                        bool is_fast,
                                        bool is_indexed,
                                        char **error_buffer);

uint32_t schema_builder_add_json_field(SchemaBuilder *builder_ptr,
                                       const char *field_name_ptr,
                                       bool stored,
                                       bool is_fast,
                                       bool is_indexed,
                                       uintptr_t index_record_option_const,
                                       const char *index_tokenizer_name_ptr,
                                       const char *fast_tokenizer_name_ptr,
                                       bool expand_dots_enabled,
                                       char **error_buffer);

struct TantivyContext *context_create_with_schema(const char *path_ptr,
                                                  Schema *schema_ptr,
                                                  char **error_buffer);

void context_register_text_analyzer_ngram(struct TantivyContext *context_ptr,
                                          const char *tokenizer_name_ptr,
                                          uintptr_t min_gram,
                                          uintptr_t max_gram,
                                          bool prefix_only,
                                          char **error_buffer);

void context_register_text_analyzer_edge_ngram(struct TantivyContext *context_ptr,
                                               const char *tokenizer_name_ptr,
                                               uintptr_t min_gram,
                                               uintptr_t max_gram,
                                               uintptr_t limit,
                                               char **error_buffer);

void context_register_text_analyzer_simple(struct TantivyContext *context_ptr,
                                           const char *tokenizer_name_ptr,
                                           uintptr_t text_limit,
                                           const char *lang_str_ptr,
                                           char **error_buffer);

void context_register_text_analyzer_raw(struct TantivyContext *context_ptr,
                                        const char *tokenizer_name_ptr,
                                        char **error_buffer);

uint64_t context_add_and_consume_documents(struct TantivyContext *context_ptr,
                                           struct Document **docs_ptr,
                                           uintptr_t docs_len,
                                           char **error_buffer);

uint64_t context_delete_documents(struct TantivyContext *context_ptr,
                                  unsigned int field_id,
                                  const char **delete_ids_ptr,
                                  uintptr_t delete_ids_len,
                                  char **error_buffer);

uint64_t context_batch_add_and_delete_documents(struct TantivyContext *context_ptr,
                                                struct Document **add_docs_ptr,
                                                uintptr_t add_docs_len,
                                                unsigned int delete_field_id,
                                                const char **delete_ids_ptr,
                                                uintptr_t delete_ids_len,
                                                char **error_buffer);

uint64_t context_num_docs(struct TantivyContext *context_ptr, char **error_buffer);

struct SearchResult *context_search(struct TantivyContext *context_ptr,
                                    unsigned int *field_ids_ptr,
                                    float *field_weights_ptr,
                                    uintptr_t field_ids_len,
                                    const char *query_ptr,
                                    char **error_buffer,
                                    uintptr_t docs_limit,
                                    bool with_highlights);

struct SearchResult *context_search_json(struct TantivyContext *context_ptr,
                                         const char *query_ptr,
                                         char **error_buffer,
                                         uintptr_t docs_limit,
                                         bool with_highlights);

struct SearchResult *context_search_sorted(struct TantivyContext *context_ptr,
                                           const char *query_ptr,
                                           const struct SortedSearchField *sort_fields_ptr,
                                           uintptr_t sort_fields_len,
                                           const struct SortedSearchValue *after_ptr,
                                           uintptr_t after_len,
                                           uintptr_t docs_limit,
                                           int64_t deadline_seconds,
                                           uint32_t deadline_nanos,
                                           char **error_buffer);

struct SearchResult *context_search_sorted_snapshot(const struct TantivyContext *context_ptr,
                                                    const char *query_ptr,
                                                    const struct SortedSearchField *sort_fields_ptr,
                                                    uintptr_t sort_fields_len,
                                                    const struct SortedSearchValue *after_ptr,
                                                    uintptr_t after_len,
                                                    uintptr_t docs_limit,
                                                    int64_t deadline_seconds,
                                                    uint32_t deadline_nanos,
                                                    char **error_buffer);

/**
 * Performs a search and returns only fast field values (no full document loading).
 * Returns the number of results found. Results are written to pre-allocated output arrays.
 */
uintptr_t context_search_fast_field(struct TantivyContext *context_ptr,
                                    unsigned int *field_ids_ptr,
                                    float *field_weights_ptr,
                                    uintptr_t field_ids_len,
                                    const char *query_ptr,
                                    unsigned int fast_field_id,
                                    uintptr_t docs_limit,
                                    float *out_scores_ptr,
                                    char **out_values_ptr,
                                    char **error_buffer);

/**
 * Free an array of strings returned by context_search_fast_field
 */
void fast_field_values_free(char **values_ptr, uintptr_t count);

/**
 * Performs a search using JSON query and returns only fast field values (no full document loading).
 * Returns the number of results found. Results are written to pre-allocated output arrays.
 */
uintptr_t context_search_fast_field_json(struct TantivyContext *context_ptr,
                                         const char *query_ptr,
                                         unsigned int fast_field_id,
                                         uintptr_t docs_limit,
                                         float *out_scores_ptr,
                                         char **out_values_ptr,
                                         char **error_buffer);

uintptr_t context_search_fast_field_u64(struct TantivyContext *context_ptr,
                                        unsigned int *field_ids_ptr,
                                        float *field_weights_ptr,
                                        uintptr_t field_ids_len,
                                        const char *query_ptr,
                                        unsigned int fast_field_id,
                                        uintptr_t docs_limit,
                                        float *out_scores_ptr,
                                        uint64_t *out_values_ptr,
                                        bool *out_valid_ptr,
                                        char **error_buffer);

uintptr_t context_search_fast_field_i64(struct TantivyContext *context_ptr,
                                        unsigned int *field_ids_ptr,
                                        float *field_weights_ptr,
                                        uintptr_t field_ids_len,
                                        const char *query_ptr,
                                        unsigned int fast_field_id,
                                        uintptr_t docs_limit,
                                        float *out_scores_ptr,
                                        int64_t *out_values_ptr,
                                        bool *out_valid_ptr,
                                        char **error_buffer);

uintptr_t context_search_fast_field_f64(struct TantivyContext *context_ptr,
                                        unsigned int *field_ids_ptr,
                                        float *field_weights_ptr,
                                        uintptr_t field_ids_len,
                                        const char *query_ptr,
                                        unsigned int fast_field_id,
                                        uintptr_t docs_limit,
                                        float *out_scores_ptr,
                                        double *out_values_ptr,
                                        bool *out_valid_ptr,
                                        char **error_buffer);

uintptr_t context_search_fast_field_date(struct TantivyContext *context_ptr,
                                         unsigned int *field_ids_ptr,
                                         float *field_weights_ptr,
                                         uintptr_t field_ids_len,
                                         const char *query_ptr,
                                         unsigned int fast_field_id,
                                         uintptr_t docs_limit,
                                         float *out_scores_ptr,
                                         int64_t *out_values_ptr,
                                         bool *out_valid_ptr,
                                         char **error_buffer);

uintptr_t context_search_fast_field_u64_json(struct TantivyContext *context_ptr,
                                             const char *query_ptr,
                                             unsigned int fast_field_id,
                                             uintptr_t docs_limit,
                                             float *out_scores_ptr,
                                             uint64_t *out_values_ptr,
                                             bool *out_valid_ptr,
                                             char **error_buffer);

uintptr_t context_search_fast_field_i64_json(struct TantivyContext *context_ptr,
                                             const char *query_ptr,
                                             unsigned int fast_field_id,
                                             uintptr_t docs_limit,
                                             float *out_scores_ptr,
                                             int64_t *out_values_ptr,
                                             bool *out_valid_ptr,
                                             char **error_buffer);

uintptr_t context_search_fast_field_f64_json(struct TantivyContext *context_ptr,
                                             const char *query_ptr,
                                             unsigned int fast_field_id,
                                             uintptr_t docs_limit,
                                             float *out_scores_ptr,
                                             double *out_values_ptr,
                                             bool *out_valid_ptr,
                                             char **error_buffer);

uintptr_t context_search_fast_field_date_json(struct TantivyContext *context_ptr,
                                              const char *query_ptr,
                                              unsigned int fast_field_id,
                                              uintptr_t docs_limit,
                                              float *out_scores_ptr,
                                              int64_t *out_values_ptr,
                                              bool *out_valid_ptr,
                                              char **error_buffer);

/**
 * Performs a search using a query parser string (supports range queries, fuzzy queries, etc.)
 * Returns a SearchResult pointer or null on error.
 */
struct SearchResult *context_search_query_parser(struct TantivyContext *context_ptr,
                                                 const char *query_ptr,
                                                 uintptr_t docs_limit,
                                                 bool with_highlights,
                                                 bool allow_regexes,
                                                 char **error_buffer);

void context_free(struct TantivyContext *context_ptr);

uintptr_t search_result_get_size(struct SearchResult *result_ptr, char **error_buffer);

bool search_result_get_has_more(struct SearchResult *result_ptr, char **error_buffer);

uintptr_t search_result_get_sort_values_len(struct SearchResult *result_ptr,
                                            uintptr_t index,
                                            char **error_buffer);

void search_result_copy_sort_values(struct SearchResult *result_ptr,
                                    uintptr_t index,
                                    struct SortedSearchValue *values_ptr,
                                    uintptr_t values_len,
                                    char **error_buffer);

struct Document *search_result_get_doc(struct SearchResult *result_ptr,
                                       uintptr_t index,
                                       char **error_buffer);

void search_result_free(struct SearchResult *result_ptr);

struct Document *document_create(void);

void document_add_field(struct Document *doc_ptr,
                        unsigned int field_id,
                        const char *field_value_ptr,
                        char **error_buffer);

void document_add_fields(struct Document *doc_ptr,
                         unsigned int *field_ids_ptr,
                         uintptr_t field_ids_len,
                         const char *field_value_ptr,
                         char **error_buffer);

void document_add_u64_field(struct Document *doc_ptr,
                            unsigned int field_id,
                            uint64_t field_value,
                            char **error_buffer);

void document_add_i64_field(struct Document *doc_ptr,
                            unsigned int field_id,
                            int64_t field_value,
                            char **error_buffer);

void document_add_f64_field(struct Document *doc_ptr,
                            unsigned int field_id,
                            double field_value,
                            char **error_buffer);

void document_add_date_field(struct Document *doc_ptr,
                             unsigned int field_id,
                             int64_t timestamp_millis,
                             char **error_buffer);

void document_add_bytes_field(struct Document *doc_ptr,
                              unsigned int field_id,
                              const char *field_value_ptr,
                              uintptr_t field_value_len,
                              char **error_buffer);

void document_add_json_field(struct Document *doc_ptr,
                             unsigned int field_id,
                             const char *field_value_ptr,
                             char **error_buffer);

char *document_as_json(struct Document *doc_ptr,
                       unsigned int *include_field_ids_ptr,
                       uintptr_t include_field_ids_len,
                       Schema *schema_ptr,
                       char **error_buffer);

void document_free(struct Document *doc_ptr);

void string_free(char *s);

void init_lib(const char *log_level_ptr,
              char **error_buffer,
              bool clear_on_panic,
              bool utf8_lenient);

void context_wait_and_free(struct TantivyContext *context_ptr, char **error_buffer);

uint64_t context_commit_opstamp(struct TantivyContext *context_ptr);

void context_reload_reader(struct TantivyContext *context_ptr, char **error_buffer);

uint64_t context_garbage_collect_files(struct TantivyContext *context_ptr, char **error_buffer);
