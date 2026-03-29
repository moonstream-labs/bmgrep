# bmgrep Implementation Plan

This document is the execution blueprint for building `bmgrep`, a local-first BM25 search CLI for Markdown reference documentation.

## 1. Product Goals

- Provide near-instant, agent-optimized search over curated local docs.
- Use only local dependencies: SQLite + FTS5, no network services.
- Keep the command surface small, discoverable, and predictable.
- Preserve actionable output:
  - ranked document paths
  - `cat -n` style line-numbered excerpts
  - sample windows by term density

## 2. Hard Requirements

- Binary name: `bmgrep`
- Search scope: always the persistent default collection.
- Supported indexed extensions in v1: `.md` only.
- Query interpretation: plain text terms only (not raw FTS query syntax).
- `--rank <n>` mode is mutually exclusive with sample-mode flags.
- No raw BM25 scores in output.
- `.bmgrepignore` lives at the collection root and uses `.gitignore`-style patterns.

## 3. Command Surface

### 3.1 Root Search Command

```bash
bmgrep "query terms"
```

Sample mode flags:

- `-n, --limit <n>`: number of ranked documents to return.
- `-l, --lines <n>`: lines per excerpt window.
- `-s, --samples <n>`: non-overlapping excerpt windows per document.

Rank mode:

- `--rank <n>`: index-only rank output with metadata, no excerpts.

Validation:

- If `--rank` is set, `--limit`, `--lines`, and `--samples` are not allowed.

### 3.2 Collection Commands

- `bmgrep collection list`
- `bmgrep collection create <name> --path <dir>`
- `bmgrep collection set <name>`
- `bmgrep collection rename <old> <new>`
- `bmgrep collection delete <name>`

### 3.3 Ignore Commands

- `bmgrep ignore list`
- `bmgrep ignore add <pattern...>`
- `bmgrep ignore remove <pattern...>`
- `bmgrep ignore path`

All ignore commands operate on the current default collection.

## 4. Data Model (SQLite)

### 4.1 Database Location

- Default path: `~/.local/share/bmgrep/bmgrep.db`

### 4.2 Config Location

- Default path: `~/.config/bmgrep/config.yaml`
- Stores `default_collection`.

### 4.3 Tables

`collections`

- `id INTEGER PRIMARY KEY`
- `name TEXT NOT NULL UNIQUE`
- `root_path TEXT NOT NULL`
- `ignore_file_path TEXT NOT NULL`
- `created_at TEXT NOT NULL`
- `updated_at TEXT NOT NULL`

`documents`

- `id INTEGER PRIMARY KEY`
- `collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE`
- `path TEXT NOT NULL UNIQUE` (absolute real path)
- `rel_path TEXT NOT NULL`
- `file_hash TEXT NOT NULL`
- `mtime_ns INTEGER NOT NULL`
- `size_bytes INTEGER NOT NULL`
- `line_count INTEGER NOT NULL`
- `raw_content TEXT NOT NULL`
- `clean_content TEXT NOT NULL`
- `updated_at TEXT NOT NULL`

Indexes:

- `idx_documents_collection_id`
- `idx_documents_collection_rel_path` unique on `(collection_id, rel_path)`

### 4.4 FTS5

`docs_fts`:

- `CREATE VIRTUAL TABLE docs_fts USING fts5(clean_content, content='documents', content_rowid='id', tokenize='unicode61');`

Triggers maintain index on insert/update/delete.

`docs_vocab`:

- `CREATE VIRTUAL TABLE docs_vocab USING fts5vocab(docs_fts, 'instance');`

Used for token hit counts in rank mode.

## 5. Ingestion and Reconciliation

### 5.1 Collection Creation

1. Validate and normalize root path.
2. Create collection row and `.bmgrepignore` file if missing.
3. Scan recursively for `.md` files.
4. Apply ignore matcher.
5. Read files, compute hash/metadata, clean markdown, insert documents.
6. FTS index updates automatically via triggers.

### 5.2 Pre-Search Reconcile

For default collection only:

1. Scan current filesystem view under `root_path` for `.md`.
2. Apply `.bmgrepignore` patterns.
3. Upsert new or changed documents.
4. Remove missing documents.
5. Remove newly ignored documents.

Change detection strategy:

- Fast path: compare `(mtime_ns, size_bytes)`.
- If changed, compute hash and update only when content hash differs.

## 6. Markdown Cleaning Rules

High-impact cleanup only:

- Strip YAML frontmatter at file start.
- Strip fenced code marker lines and fence language identifiers.
- Keep fenced code content.
- Convert links to link text (`[text](url)` -> `text`).
- Remove image URL target and keep alt text when present.
- Strip HTML tags while preserving text content.
- Remove reference link definitions (`[id]: ...`).

Notes:

- Query-time excerpting uses `raw_content` for exact line-number fidelity.
- FTS ranking uses `clean_content` to reduce index noise.

## 7. Search Pipeline

### 7.1 Query Normalization

1. Lowercase query.
2. Tokenize by `unicode61`-like rules: split on non letter/digit.
3. Deduplicate terms while preserving order.
4. Build FTS query string from normalized terms.

### 7.2 Stage 1: Document Ranking (SQLite)

- MATCH query against `docs_fts` joined to `documents` filtered by collection id.
- Ordered by `bm25(docs_fts)` ascending.
- Return top N and total candidate count.

### 7.3 Rank Mode Output

For each ranked document:

- path
- line count
- total token hits for query terms (from `docs_vocab`, index-only)

No source file reads in rank mode.

### 7.4 Sample Mode Output

For each ranked document:

1. Tokenize each line from `raw_content`.
2. Build per-line match counts against query terms.
3. Slide a window of `--lines` across line scores.
4. Select top `--samples` non-overlapping windows by:
   - higher total hits
   - then higher distinct-term coverage
   - then earlier start line
5. Render excerpts in required format.

## 8. Output Contract

### 8.1 Shared Header

```text
results: <shown> of <total>
```

### 8.2 Sample Mode

```text
[1] /absolute/path/file.md
345-348:
   345	...
   346	...
```

Formatting rules:

- No blank line between path and first sample.
- No blank line between samples in same document.
- One blank line between documents.

### 8.3 Rank Mode

```text
[1] /absolute/path/file.md (127 lines, 6 matches)
```

## 9. Discoverability and Help Quality

`bmgrep --help` must include:

- concise purpose statement
- mode model (`--rank` triage vs sample preview)
- query construction guidelines for BM25
- common examples and anti-patterns

Every command includes:

- clear `Short` and `Long`
- `Example` block with practical calls
- explicit defaults and mode constraints

## 10. Validation and Test Strategy

### 10.1 Unit Tests

- query tokenizer and normalization
- markdown cleaning edge cases
- sample window selection and non-overlap behavior
- output formatting contract
- ignore file parser behavior

### 10.2 Integration Tests

- create collection, set default, run search
- reconcile detects add/update/delete/ignore changes
- rank mode returns index-only metadata
- sample mode line numbers match source content

### 10.3 Manual Smoke Tests

- run `bmgrep --help`, command-specific `--help`
- run representative rank and sample queries
- verify output against style guide examples

## 11. Phased Execution

1. Bootstrap repository docs + git-essential files.
2. Scaffold Go module and CLI command tree.
3. Implement SQLite schema + migrations + config.
4. Implement collection lifecycle and ingestion.
5. Implement ignore management and reconcile.
6. Implement search ranking and sample extraction.
7. Implement output formatting and help polish.
8. Add tests and run full test suite.
9. Run end-to-end manual validation and finalize.
