# bmgrep - Ranking, Matching, & Sampling

---

## Document Ranking: Pure FTS5

When the agent runs any `bmgrep` query, the first thing that happens is a MATCH query against the FTS5 virtual table, ordered by `bm25()`. This is the document-level ranking — it determines which files appear in the results and in what order. FTS5 handles this entirely through its inverted index. It never reads the source files. It looks up which documents contain the query terms, computes BM25 scores from the index statistics (term frequency per document, document frequency across the corpus, document length, average document length), and returns ranked document IDs. This is the only operation that touches SQLite.

This ranking is what `--rank` mode exposes directly. The ordered list of documents, their paths, their line counts — all of that comes from the index and whatever metadata columns you store alongside the indexed content. The "matches" count in rank mode is the total token hits for the query terms in that document, also pulled from the index. No source files are read. This is why `--rank` is fast — it's a pure index lookup.

## Sample Extraction: Post-Retrieval Windowing

When the agent runs a query _without_ `--rank` — the default sample mode — the pipeline has a second stage that FTS5 has no involvement in. After FTS5 returns the top N documents (controlled by `--limit`), you take those document IDs, look up their file paths, and read the actual source files from disk. Everything that happens from this point forward is your code operating on raw file content, not on anything FTS5 provides.

For each source file, you tokenize every line using the same logic as `unicode61` (lowercase, split on non-alphanumeric characters), check each token against the set of query terms, and build a per-line score array. Then you slide a window of `--lines` size across the document, summing the per-line scores for each window position. The highest-scoring windows become the samples. You select the top `--samples` non-overlapping windows by score, and those are what appear in the output with their `cat -n` formatted content and range headers.

This means the sample ranking within a document can disagree with the document ranking across documents, and that's fine — they're answering different questions. BM25 is answering "which documents are most relevant to this query across the entire corpus?" The windowing algorithm is answering "within this specific document, where are the query terms most concentrated?"

## Why They're Separate

The separation isn't an implementation shortcut — it reflects a genuine difference in what the two systems are good at.

BM25 is a statistical model designed for corpus-level discrimination. Its power comes from IDF — weighting terms by how rare they are across all documents. A term that appears in 2 out of 50 documents gets a high weight; one that appears in 40 out of 50 gets almost none. This is exactly what you want for deciding which _documents_ matter. But BM25 has no concept of _where_ within a document the terms appear. It treats a document as a bag of terms — position is invisible to it.

The windowing algorithm is the opposite. It operates on a single document with full positional awareness. It knows that lines 345-348 have three query terms clustered together while lines 600-603 have one scattered mention. BM25 can't distinguish these — both contribute equally to the document's BM25 score. But for the agent, the difference between a dense cluster and a scattered mention is the difference between "here's the relevant passage" and "this line happens to use the word."

## Where the Match Count Fits

This is the piece that ties the two systems together in `--rank` mode. The total token hits count you show in rank output — `(127 lines, 6 matches)` — is a crude proxy for what the windowing algorithm would find if you ran it. It's saying "the query terms appear 6 times total in this document," which correlates with (but doesn't guarantee) the presence of dense, useful passages.

If you wanted to make rank mode's match count reflect actual passage density — non-overlapping windows above a threshold, as I originally described — you'd have to run the windowing algorithm against each ranked document's source file even in rank mode. That makes `--rank` do file I/O, which costs you the speed advantage of a pure index operation. For a curated corpus of Markdown files the latency would still be negligible in absolute terms, but it's a design purity question: is `--rank` a fast index-only operation, or is it a full pipeline that just skips the output formatting of samples?

Total token hits from the index is the pragmatic choice. It's free (the data is already in the index), it gives the agent a useful discrimination signal, and it keeps `--rank` as a pure index operation with predictable, near-instant performance regardless of document size.