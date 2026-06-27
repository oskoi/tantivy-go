# Go Tantivy Bindings

This project provides Go bindings for the [Tantivy](https://github.com/quickwit-oss/tantivy) search engine library. Tantivy is a full-text search engine library written in Rust, and this project aims to make its powerful search capabilities available to Go developers.

The Go wrapper serializes access to each `TantivyContext` before crossing the Rust FFI boundary. A single context may be used from multiple goroutines, but long-running searches, commits, reloads, and close operations are executed one at a time for native pointer safety.

# Why

The only available FTS engine in the Golang community is [Bleve](https://github.com/blevesearch/bleve), which is surprisingly slow compared to [Tantivy](https://github.com/quickwit-oss/tantivy).
Check out the last link for details on the performance comparison.

![Search Benchmark](https://github.com/quickwit-oss/tantivy/blob/main/doc/assets/images/searchbenchmark.png)
Credits for the image to the Tantivy team

# Our Journey with Tantivy
We've been running it in [Anytype](https://github.com/anyproto/anytype-heart) for over a year across all major platforms and architectures without issues on 32-bit and 64-bit systems, x86 and ARM64, iOS, Android, PC, macOS, and Linux.

## Features
### Golang API to Create Custom Queries for Tantivy
See `searchquerybuilder.go`

### Numeric Field Support
Supports u64, i64, and f64 field types with indexing and fast field support:
```go
builder.AddU64Field("price", true, true, true)  // stored, fast, indexed
builder.AddI64Field("temperature", true, true, true)
builder.AddF64Field("score", true, true, true)
```

### Date Field Support
Supports date/datetime fields with Unix timestamp (milliseconds) storage:
```go
// Add date field to schema
builder.AddDateField("created_at", true, true, true)  // stored, fast, indexed

// Add date to document (Unix timestamp in milliseconds)
doc.AddDateField(time.Now().UnixMilli(), tc, "created_at")

// Search by date range using RFC3339 format
result, err := tc.SearchQueryParser("created_at:[2024-01-01T00:00:00Z TO 2024-12-31T23:59:59Z]", 10, false)
```

### Typed fast-field search
Numeric and date fast fields can be returned without loading full documents:

```go
result, err := tc.SearchFastFieldU64(searchCtx, "price")
if err != nil {
	return err
}
for i, value := range result.Values {
	if result.Valid[i] {
		fmt.Println(value, result.Scores[i])
	}
}
```

Date fast fields return `time.Time` values through `SearchFastFieldDate`.

### JSON Field Support
Supports native Tantivy JSON object fields with full options:
```go
jsonOpts := tantivy_go.NewJSONFieldOptions()
jsonOpts.Stored = true
jsonOpts.IsIndexed = true
jsonOpts.IsFast = true
jsonOpts.IndexTokenizer = tantivy_go.DefaultTokenizer
jsonOpts.FastTokenizer = tantivy_go.DefaultTokenizer
jsonOpts.ExpandDotsEnabled = true

err := builder.AddJSONField("payload", jsonOpts)

doc := tantivy_go.NewDocument()
_ = doc.AddJSONField(`{"meta":{"author":"alice"},"k8s.node.id":5}`, tc, "payload")

result, err := tc.SearchQueryParser("payload.meta.author:alice", 10, false)
result, err = tc.SearchQueryParser("payload.k8s.node.id:5", 10, false)
```

### Range Queries
Supports range queries using tantivy's query parser syntax:
```go
// Inclusive range: price between 10 and 100
result, err := tc.SearchQueryParser("price:[10 TO 100]", 10, false)

// Exclusive range: price > 10 and price < 100
result, err := tc.SearchQueryParser("price:{10 TO 100}", 10, false)

// Unbounded range: price >= 50
result, err := tc.SearchQueryParser("price:[50 TO *]", 10, false)

// Combined with text search
result, err := tc.SearchQueryParser("title:Product AND price:[20 TO 150]", 10, false)

// Optional: enable regex syntax in query parser
result, err := tc.SearchQueryParser("title:/prod.*/", 10, false, tantivy_go.WithRegexesEnabled())
```

**Query Parser Syntax:**
- `[lower TO upper]` - Inclusive range (includes bounds)
- `{lower TO upper}` - Exclusive range (excludes bounds)
- `*` - Unbounded (e.g., `[50 TO *]` means >= 50)
- Supports numeric and text ranges
- Can be combined with boolean operators (AND, OR, NOT)


## Lifecycle rules

- `TantivyContext.Close` is safe to call multiple times.
- Documents passed to `AddAndConsumeDocuments` or batch add are consumed and cannot be reused.
- `GetSearchResults` consumes and frees the `SearchResult` passed to it.

## Search quality testing
[Test quality](testquality/README.md)

## Installation

```bash
go get github.com/oskoi/tantivy-go
```

Ensure your libraries are in your `ld` path. Default builds without `tantivylocal` require `libtantivy_go` in the system linker path. Source checkouts can run tests with `go test -tags tantivylocal ./...` after local libraries are present under `libs/<platform>`.

### Example Run
- Run `make download-tantivy-all` inside the `rust` folder
- Run `main.go` in the `example` folder

## Development
Development and compilation are done on MacBooks and for Apple platforms. Therefore, the development steps provided are for macOS.

### Install environment
- [Install rustup](https://rust-lang.github.io/rustup/installation/other.html)
- Install Rust architectures: `make setup`
- Add Android libraries to your path: `export PATH=$PATH:$ANDROID_HOME/tools:$ANDROID_HOME/emulator:$ANDROID_HOME/platform-tools:$ANDROID_HOME/ndk/25.2.9519653/toolchains/llvm/prebuilt/darwin-x86_64/bin`
- Install Windows compiler:  `brew install mingw-w64`
- Install musl: `brew tap messense/macos-cross-toolchains && brew install x86_64-unknown-linux-musl`

### Install rust libraries
Run inside the `rust` folder:

`make install-all` - install release versions for all platforms

`make install-debug-all` - install debug versions for all platforms

`make install-ARCH-GOOS` - install release version for ARCH GOOS

`make install-debug-ARCH-GOOS` - install debug version for ARCH GOOS

### GCC support
To be done

### Validate min macos version

`otool -l libtantivy_go.a  | rg LC_BUILD_VERSION -A4 | rg minos | sort | uniq -c`
Expected output:
```
 880     minos 11.0
```

### Possible troubleshooting
If you experience SIGSEGV issues with musl or windows, try adding these flags to the linker:
```
-extldflags '-static -Wl,-z stack-size=1000000'
```

### Nix

`flake.nix` currently provides two versions of `devShell`: musl and gcc.

This command will make a bash shell with all required build dependencies:

```bash
nix develop .
```

Each `devShell` also contains a script which:

- builds rust into `.a` lib
- copies it to `../anytype-heart`
- builds `anytype-heart` `grpcServer`
- copies `grpcServer` to `../anytype-ts` `anytypeHelper`

> [!TIP]
> To enable musl, set `musl = true;` in `flake.nix`.

If you want to debug `tantivy` from `anytype-ts`, with `musl` or `gcc`, this scripts automates all the flow.

All together it would look like:
```bash
nix develop .
tantivy_compile_all_gcc
# or
tantivy_compile_all_musl
```

To check that it works, run `anytype-ts` and try to search something.

> [!NOTE]
> MacOS (Darwin) nix shell is not supported yet
