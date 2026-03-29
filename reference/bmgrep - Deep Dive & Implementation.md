# bmgrep - Deep Dive & Implementation

---

## How FTS5 Tokenization Actually Handles Markdown

The default FTS5 tokenizer (`unicode61`) splits tokens on any character that isn't alphanumeric (broadly: Unicode letter/number categories), folds case, and discards the separators. This means characters like `#`, `*`, `>`, `` ` ``, `[`, `]`, `(`, `)`, `|`, and `-` are all treated as token boundaries and thrown away during indexing. So `## Configuration Options` tokenizes to the three terms `configuration`, `options` — the hashes never enter the index. Similarly, `**important**` becomes `important`, `> blockquote text` becomes `blockquote` and `text`, and `` `some_func` `` becomes `some` and `func` (since the underscore is also a separator by default, which is worth noting).

This means that for the majority of Markdown's structural syntax — headers, emphasis, bold, blockquotes, horizontal rules, list markers — the tokenizer is effectively already doing the stripping for you. Pre-processing those elements before ingestion won't change which terms end up in the index or how they score. Stripping `##` from `## My Header` before inserting it is functionally identical to inserting it as-is, because the tokenizer produces the same token stream either way.

So where does pre-processing actually matter? The cases where Markdown syntax introduces _real noise into the token stream_ — meaning extra terms that the tokenizer can't distinguish from content.

## Where Pre-Processing Genuinely Helps

**URLs are the biggest offender.** A Markdown link like `[configuration guide](https://docs.example.com/v2/setup/configuration-guide)` tokenizes into: `configuration`, `guide`, `https`, `docs`, `example`, `com`, `v2`, `setup`, `configuration`, `guide`. You've now doubled the term frequency of `configuration` and `guide` in that document, and introduced noise terms like `https`, `docs`, `com`, `v2`, and `setup` that have nothing to do with the document's actual content. Across a corpus where many documents contain links to similar domains, those URL-derived tokens (`docs`, `com`, `github`, `io`) will appear in many documents, which depresses their IDF — that part is mostly harmless. But the duplicate content terms inflating TF, and the extra tokens inflating document length (which BM25's length normalization penalizes), do distort ranking. Strip the URL portion of links and image references, keep the link text. This is the single highest-impact pre-processing step.

**Image references** follow the same logic. `![alt text](path/to/image.png)` should be reduced to just the alt text, or dropped entirely if the alt text is non-descriptive.

**HTML tags**, if present in your Markdown, inject tag names (`div`, `span`, `table`, `href`, `class`) as tokens. Strip them, keep their text content.

**Metadata/frontmatter blocks** (YAML between `---` fences) can introduce key-value noise. If your documents have frontmatter, strip it or index it separately.

## Code Blocks: The Interesting Case

This is where it gets genuinely tricky, and the right answer depends on your agents' usage patterns. Fenced code blocks present two distinct considerations.

First, the **language identifier** (` ```python `, ` ```typescript `, etc.): the word `python` or `typescript` becomes a token. If many documents have Python examples, `python` will appear frequently across the corpus, lowering its IDF and making it a weak discriminator. This is mild noise — stripping the language identifier is easy and slightly beneficial, but not transformative.

Second, the **code content itself**: this is where you need to make a judgment call. Code blocks contain identifiers, function names, API method names, and configuration keys that a coding agent might very plausibly search for. If an agent queries `bmgrep "createReadStream"` and your Node.js reference doc has that in a code example, you want that match. Stripping code blocks entirely would eliminate those matches. On the other hand, dense code blocks add a lot of tokens (every variable name, keyword, punctuation-delimited fragment) that inflate document length and can dilute BM25 scores for the surrounding prose content.

My recommendation here: **keep code block content, strip the fence markers and language identifiers.** The value of making code examples searchable outweighs the noise cost for a coding agent use case. If you find that code-heavy documents are getting unfairly penalized by length normalization, you can tune the `b` parameter in BM25 (FTS5 lets you configure this via `bm25(fts_table, ...)` — the default `b=0.75` is a moderate length penalty; reducing it toward `0` diminishes the penalty for longer documents).

## Inline Code and Lists

**Inline backtick code** (`` `someFunction` ``): the backticks are stripped by the tokenizer, and the content is tokenized normally. `some` and `function` become separate tokens (or `somefunction` stays as one token if there's no separator — it depends on the casing and characters). No pre-processing needed here; the tokenizer handles it cleanly.

**Ordered and unordered lists**: the markers (`-`, `*`, `1.`, `2.`) are stripped by the tokenizer. The content is tokenized as normal prose. No action needed.

## The Deeper BM25 Consideration

Beyond token-level noise, there's a subtler issue worth thinking about: **what constitutes a "document" in your FTS5 table.** BM25's IDF component is computed across all documents in the table, and its length normalization is relative to the average document length. If you're ingesting entire Markdown files as single rows, a 2,000-line reference doc and a 50-line API quickstart guide are both "documents" — and the BM25 length normalization will heavily penalize the long one, even if the query terms are densely concentrated in a specific section.

You might already be handling this given your sample-extraction approach, but it's worth considering whether you should segment documents into chunks (by heading, by section) at ingestion time rather than indexing whole files. This gives BM25's statistics a more level playing field and naturally improves the precision of your excerpt extraction. You'd store the source file path and line offset alongside each chunk so your output can still reference the original file.

## Practical Summary

The pre-processing that actually moves the needle, in rough priority order: strip URLs from links and images (keep link text), strip HTML tags (keep text content), strip frontmatter, strip code fence markers and language identifiers (keep code content), and strip reference-style link definitions. Everything else — header markers, emphasis, blockquotes, list markers — the tokenizer already handles for you, so stripping them is cosmetic rather than functional.

---

## Implementing Ranked Samples by Term Density

FTS5 doesn't provide anything that directly solves this problem. It has two auxiliary functions that get close — `snippet()` and `highlight()` — but neither is sufficient on its own, and understanding why clarifies what you actually need to build.

## What FTS5 Gives You (and Why It's Not Enough)

`snippet()` is the obvious candidate. It extracts a short fragment surrounding matching terms and is designed for exactly the "show a preview" use case. But it has hard limitations that make it unsuitable here. It returns a single snippet per column per row — you can't get multiple ranked passages from one document. Its internal algorithm for choosing _which_ passage to show is opaque and not configurable. The size is controlled by a token count, not a line count, which doesn't map to your `--lines` output model. And there's no way to ask it for the second-best or third-best passage. For a simple "show one preview" feature it's fine, but for ranked multi-sample extraction it's a dead end.

`highlight()` is more useful as a building block. It wraps every matching term in the document with delimiter strings you specify. So if your query is `authentication middleware` and the document contains those terms, you get back the full text with every occurrence of `authentication` and `middleware` wrapped in markers like `«authentication»`. This effectively gives you a map of where every match occurs in the document text. But it gives you the _entire_ document with annotations — it doesn't do any windowing, density calculation, or ranking of passages. It's a match locator, not a passage ranker.

So the passage ranking is yours to build. The good news is that the algorithm is straightforward and the performance characteristics are favorable for your use case, since you're only running it against the top N documents that FTS5 has already identified.

## The Sliding Window Approach

The core algorithm is a fixed-size sliding window over the lines of each ranked document, scored by query term density within the window. Here's the conceptual structure.

After FTS5 returns ranked documents, you retrieve the source text for each. You have two options for locating match positions: parse the output of `highlight()`, or re-tokenize the source text yourself against the query terms. I'd recommend the latter. The reason is that `highlight()` returns the _indexed_ text (whatever you inserted into the FTS table), which after your pre-processing may differ from the original file on disk — and your output needs to show the original file content with accurate line numbers. Working directly from the source file keeps everything aligned.

Re-tokenizing to match `unicode61` behavior is simple: lowercase the text, split on any character that isn't a Unicode letter or digit, and you have the same token stream that FTS5 produced. Build a set of the query terms (processed the same way), then for each line in the source file, count how many tokens from that line appear in the query term set. This gives you a per-line match count.

Now slide a window of `--lines` size across the document. For each window position, sum the per-line match counts. This is your window's density score. You want the top `--samples` non-overlapping windows by density score.

The non-overlapping constraint is important. Without it, if a document has a single dense cluster of matches, your top three windows will all be slight offsets of the same passage, which is useless to the agent. The standard approach: find the highest-scoring window, record it, then exclude any window that overlaps with it from further consideration, find the next highest, and repeat. This greedy selection is simple and works well in practice.

## Scoring Nuances Worth Considering

A raw count of matching terms per window works, but there are a couple of refinements that can meaningfully improve passage quality.

**Weight terms by IDF, not just count.** If the agent queries `bmgrep "WebSocket authentication middleware"`, a window containing two occurrences of `websocket` and one of `middleware` should score differently than a window containing two occurrences of `middleware` and one of `websocket` — if `websocket` is rarer in the corpus, the first window is more informative. You already have access to IDF-like statistics through FTS5: the `fts5vocab` virtual table gives you document frequency for every term in the index. At startup or query time, you can look up the document frequency of each query term and use the inverse as a weight. The window score then becomes the sum of IDF weights of all matching tokens within the window, rather than a raw count. This aligns your passage ranking with the same principle BM25 uses for document ranking.

**Consider distinct term coverage as a tiebreaker.** Between two windows with equal density scores, the one that contains all three query terms is more useful than the one that contains twelve occurrences of a single term. A simple approach: primary sort by weighted density score, secondary sort by the number of distinct query terms present in the window.

## A Practical Concern: Line-Number Accuracy

Since your output format shows `cat -n` style line numbers, the mapping between the window and the source file needs to be exact. This is straightforward if you work directly from the source file (split on newlines, enumerate from 1), but there's a subtle trap if you try to use `highlight()` output instead: the markers injected by `highlight()` could shift apparent line boundaries if a marker wraps text that spans a newline, or if your pre-processing collapsed whitespace. Working from the source file directly avoids this entirely.

The sequence is: FTS5 gives you ranked document IDs, you use the ID to look up the file path (from a metadata column in your table or a separate mapping), you read the file from disk, you run the windowing algorithm over the raw file content, and you output windows with accurate line numbers. The FTS5 index is used purely for document ranking — passage extraction is a post-retrieval operation against the original files.

## Implementation Structure

The cleanest separation is three stages in the query pipeline:

**Stage 1 — Document ranking.** Pure FTS5. Run the MATCH query, order by `bm25()`, limit to whatever `--limit` or `--rank` specifies. This is the only stage that touches SQLite.

**Stage 2 — Term location.** For each ranked document (if not `--rank` mode), read the source file, tokenize each line, and build a per-line match score array. This is pure string processing, no database involvement.

**Stage 3 — Window extraction.** Slide the window, score, select top non-overlapping windows, format output. Pure computation over the per-line score array from stage 2.

This separation keeps the responsibilities clean, makes each stage independently testable, and means you can swap out the windowing strategy without touching the retrieval logic. It also means stage 1 can be extremely fast (FTS5 queries over a modest corpus return in single-digit milliseconds), and stages 2 and 3 add only the cost of reading and scanning the top N files — which for a curated local corpus of Markdown files is negligible.

## The `fts5vocab` Detail

If you go with IDF-weighted window scoring, the `fts5vocab` table is how you get the statistics. When you create your FTS5 table, you can create an associated vocab table:

```sql
CREATE VIRTUAL TABLE docs_fts USING fts5(content, path UNINDEXED);
CREATE VIRTUAL TABLE docs_vocab USING fts5vocab(docs_fts, row);
```

Then at query time, for each query term, you look up its document frequency:

```sql
SELECT doc FROM docs_vocab WHERE term = ?;
```

The `doc` column gives you the number of documents containing that term. Compute IDF as `log((N - df + 0.5) / (df + 0.5) + 1)` (the standard BM25 IDF formula, where N is total document count and df is the document frequency), and use that as the weight for that term in your window scoring. This is cheap — a handful of index lookups — and gives you corpus-aware passage ranking that mirrors BM25's own logic.