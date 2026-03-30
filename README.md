# bmgrep

A local-first BM25 search CLI for Markdown reference documentation, purpose-built for LLM agent workflows.

bmgrep indexes curated local Markdown collections into a SQLite FTS5 database and provides two search modes: **ranked document triage** and **excerpt sampling with line-numbered windows**. Every token in the output is designed to directly feed an agent's next action or inform a decision — no decorative formatting, no wasted context.

## Why bmgrep exists

LLM agents operate under constant context-window pressure. When an agent needs to locate information in reference documentation, it faces a fundamental tension: reading entire files is expensive, but not reading them risks missing critical content.

bmgrep resolves this with a two-stage pipeline:

1. **Document ranking** (BM25 via SQLite FTS5) — identifies which files are most relevant to a query across the entire corpus, using statistical term weighting.
2. **Passage extraction** (sliding window by term density) — within each ranked file, locates the most information-dense excerpts using IDF-weighted scoring.

This lets an agent triage a corpus in one tool call, preview specific passages in another, and commit to a full file read only when justified — the most context-efficient workflow possible.

## Install

```bash
go install github.com/moonstream-labs/bmgrep/cmd/bmgrep@latest
```

Or build from source:

```bash
git clone https://github.com/moonstream-labs/bmgrep.git
cd bmgrep
go build -o bmgrep ./cmd/bmgrep
```

bmgrep has no runtime dependencies beyond the single binary. The SQLite database (via [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)) is compiled in — no CGo, no system SQLite required.

## Quick start

```bash
# Create a collection from a directory of Markdown files
bmgrep collection create docs --path /home/user/reference/docs

# Search with excerpt sampling (default mode)
bmgrep "authentication middleware"

# Fast triage with ranked document list
bmgrep "authentication middleware" --rank 5
```

## Search modes

### Sample mode (default)

Returns ranked documents with line-numbered excerpt windows showing the most term-dense passages.

```
$ bmgrep "authentication middleware" -n 2 -l 4 -s 2

results: 2 of 8

[1] /home/user/reference/docs/express-auth.md
345-348:
   345	The authentication middleware intercepts incoming requests
   346	and validates the bearer token against the session store
   347	before passing control to the route handler. If validation
   348	fails, it short-circuits with a 401 response.
512-515:
   512	app.use('/api', authMiddleware({
   513	  tokenStore: sessionStore,
   514	  onFailure: (req, res) => res.status(401).json({ error: 'unauthorized' }),
   515	}));

[2] /home/user/reference/docs/security-overview.md
73-76:
    73	Authentication middleware should be registered before any
    74	route handlers that serve protected resources. Ordering
    75	matters — middleware registered after a route handler
    76	will not intercept requests to that route.
```

Flags:

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--limit` | `-n` | 5 | Number of ranked documents to return |
| `--lines` | `-l` | 4 | Lines per excerpt window |
| `--samples` | `-s` | 1 | Non-overlapping excerpt windows per document |

### Rank mode

Returns an index-only ranked list with line counts and match counts. No source files are read — this is a pure FTS5 index lookup.

```
$ bmgrep "authentication middleware" --rank 5

results: 5 of 12

[1] /home/user/reference/docs/express-auth.md (127 lines, 6 matches)
[2] /home/user/reference/docs/security-overview.md (843 lines, 3 matches)
[3] /home/user/reference/docs/session-management.md (234 lines, 2 matches)
[4] /home/user/reference/docs/api-reference.md (1,509 lines, 2 matches)
[5] /home/user/reference/docs/migration-guide-v3.md (412 lines, 1 match)
```

`--rank` is mutually exclusive with `--limit`, `--lines`, and `--samples`.

## Query construction

BM25 is a term-matching algorithm, not a semantic search engine. The agent's job when constructing a query is to **predict which specific words appear in the target document**, not to describe what it wants to learn.

**Use the vocabulary of the documents, not the vocabulary of the task.**

| Weak query | Strong query | Why |
|---|---|---|
| `"how to set up auth"` | `"authentication middleware configuration"` | `how`, `to`, `set`, `up` have near-zero IDF — they appear in every document |
| `"env vars dotenv"` | `"environment variables"` | BM25 has no synonym awareness; `env` ≠ `environment` |
| `"what is the syntax for pattern matching"` | `"syntax pattern matching"` | Question words waste tokens with zero discrimination |

**Guidelines:**

- Keep queries to **2-4 specific terms**. Each additional term that isn't in the target document dilutes scores.
- Favor **nouns and domain-specific terms** — API names, configuration keys, library names, named concepts.
- Prefer **canonical/unabbreviated terms**. Reference docs use formal terminology.
- If results are poor, **reformulate with different terms** rather than adding more terms.

## Workflow patterns

### Narrow, then broaden

Start with the most specific query. Only generalize if results are insufficient.

```bash
# Specific first
bmgrep "WebSocket heartbeat timeout" --rank 3

# Broaden only if needed
bmgrep "WebSocket connection" --rank 5
```

### Rank → Sample → Read

The most context-efficient workflow:

```bash
# 1. Triage: which documents are relevant?
bmgrep "database connection pool" --rank 3

# 2. Preview: what do the relevant passages look like?
bmgrep "database connection pool" -n 1 -l 5 -s 2

# 3. Read the file (using the path from bmgrep output)
cat -n /home/user/reference/docs/config-reference.md
```

### Interpreting the results header

```
results: 2 of 2   → Highly specific query, results are almost certainly on-target
results: 5 of 25  → Broad query, consider refining with more specific terms
results: 0 of 0   → Terms not in corpus, reformulate with different vocabulary
```

## Collection management

Collections define which directory of Markdown files to index. bmgrep always searches the **default collection**.

```bash
# Create a collection (first collection auto-becomes default)
bmgrep collection create docs --path /home/user/reference/docs

# List all collections (* marks default)
bmgrep collection list

# Switch default collection
bmgrep collection set other-docs

# Rename a collection
bmgrep collection rename docs docs-v2

# Delete a collection and all its indexed documents
bmgrep collection delete old-docs
```

## Ignore patterns

Each collection has a `.bmgrepignore` file at its root using `.gitignore`-style syntax. Ignored files are excluded from indexing and removed during reconciliation.

```bash
# List current patterns
bmgrep ignore list

# Add patterns
bmgrep ignore add "archive/**" "**/draft-*.md"

# Remove patterns (triggers re-index of previously ignored files)
bmgrep ignore remove "archive/**"

# Show the ignore file path
bmgrep ignore path
```

## How it works

### Indexing

When a collection is created, bmgrep:

1. Recursively scans the root directory for `.md` files.
2. Applies `.bmgrepignore` patterns to filter candidates.
3. Reads each file, computing SHA-256 hash and metadata (mtime, size, line count).
4. Cleans the Markdown for FTS5 indexing (see below).
5. Inserts both raw content (for excerpt display) and cleaned content (for ranking) into SQLite.
6. FTS5 triggers automatically maintain the inverted index.

### Pre-search reconciliation

Before every search, bmgrep performs a fast reconciliation against the filesystem:

- **New files** are indexed.
- **Changed files** are re-indexed (fast path: mtime+size check, then hash comparison if changed).
- **Deleted files** are removed from the index.
- **Newly ignored files** are removed from the index.

All mutations happen in a single SQLite transaction — the index is never left in a partially-updated state.

### Markdown cleaning

FTS5's `unicode61` tokenizer already strips most Markdown syntax (`#`, `*`, `>`, backticks, brackets). Cleaning targets the high-impact noise sources that the tokenizer cannot handle:

| Source | Problem | Action |
|---|---|---|
| URLs in links | Duplicate content terms + noise tokens (`https`, `docs`, `com`) inflating TF and document length | Strip URL, keep link text |
| Image references | Same as links | Strip URL, keep alt text |
| YAML frontmatter | Key-value noise tokens | Strip entirely |
| Code fence markers | Language identifiers (`python`, `typescript`) as low-value tokens | Strip markers, **keep code content** |
| HTML tags | Tag names (`div`, `span`, `href`) as noise tokens | Strip tags, keep text content |
| Reference link definitions | URL noise | Strip entirely |

Code block content is intentionally preserved — code examples contain identifiers, function names, and API method names that agents plausibly search for.

### IDF-weighted passage scoring

When extracting excerpt windows in sample mode, bmgrep weights each query term by its IDF (Inverse Document Frequency) using the BM25 formula:

```
IDF(term) = log((N - df + 0.5) / (df + 0.5) + 1)
```

Where `N` is total documents and `df` is the number of documents containing the term. This means a window containing a rare, highly discriminating term scores higher than a window with multiple occurrences of a common term — aligning passage ranking with the same statistical principle BM25 uses for document ranking.

Windows are selected greedily: the highest-scoring window is chosen first, then any overlapping windows are excluded from consideration, and the process repeats. Final output presents windows in document order.

## Output format

### Design principle

Every token in bmgrep output maps to a concrete agent decision or tool call. Line numbers use `cat -n` conventions (right-justified, tab-delimited) for consistency with other file inspection tools agents already parse. Paths are absolute `realpath` values that feed directly into Read tool calls without transformation.

### Whitespace rules

- No blank line between path and first sample.
- No blank line between samples within a document.
- One blank line between documents (highest-level structural boundary).

## Data locations

| Data | Default path | Override |
|---|---|---|
| Config | `~/.config/bmgrep/config.yaml` | `--config` flag, `$BMGREP_CONFIG`, or `$XDG_CONFIG_HOME` |
| Database | `~/.local/share/bmgrep/bmgrep.db` | `--db` flag, `$BMGREP_DB`, or `$XDG_DATA_HOME` |
| Ignore file | `<collection_root>/.bmgrepignore` | — |

## Development

### Prerequisites

- Go 1.25+

### Run tests

```bash
go test ./...
```

### Test coverage

| Package | Tests | Coverage areas |
|---|---|---|
| `internal/search` | 19 | Query normalization, tokenization, FTS operator safety, IDF-weighted window selection, non-overlap enforcement, document-order output, coverage tiebreaker, output formatting, singular/plural, comma formatting |
| `internal/ingest` | 17 | Markdown cleaning (frontmatter, fences, tilde fences, nested fences, links, images, HTML, reference defs), ignore file read/append/remove, reconcile lifecycle (add/update/delete/ignore/line-number fidelity) |
| `internal/paths` | 7 | Tilde expansion, absolute/relative paths, XDG config/data fallback |
| `internal/config` | 5 | Load missing file, save/load round-trip, resolution precedence (explicit > env > default) |

### Project structure

```
cmd/bmgrep/          Entry point
internal/
  cli/               Cobra command tree (root, collection, ignore)
  config/            YAML config load/save with path resolution
  ingest/            Filesystem scanning, Markdown cleaning, reconciliation
  paths/             Path expansion and XDG-aware default locations
  search/            Query normalization, sliding window sampler, output formatting
  store/             SQLite schema, FTS5 index, ranked/sample queries, IDF weights
reference/           Design rationale and implementation guidance documents
```

## License

TBD
