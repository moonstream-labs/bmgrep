# bmgrep - Agent Guidelines

---

This is really about bridging the gap between how an LLM agent naturally "thinks" about searching and how BM25 actually scores results. There are a few conceptual mismatches that, if left unaddressed, will consistently produce suboptimal queries.

## The Central Concept to Convey

BM25 is a term-matching algorithm, not a semantic search engine. This distinction sounds obvious, but it's the root cause of nearly every bad query an agent will construct. An agent's instinct is to describe what it's looking for — to form a natural language question or a conceptual paraphrase. BM25 doesn't understand concepts. It counts term occurrences, weights them by rarity across the corpus, and normalizes for document length. The agent needs to internalize that its job when constructing a query is to **predict which specific words appear in the document it wants to find**, not to describe what it wants to learn.

This reframe is the single most impactful thing you can convey. Everything else flows from it.

## Query Construction Guidelines

**Use the vocabulary of the documents, not the vocabulary of the task.** If the agent is trying to figure out how to configure authentication middleware, the query `bmgrep "how to set up auth"` is weak — `how`, `to`, `set`, `up` are extremely common terms that will appear in nearly every document (high document frequency, near-zero IDF, effectively ignored by BM25). The query `bmgrep "authentication middleware configuration"` is much stronger because those three terms are specific, likely to co-occur in exactly the kind of document the agent needs, and rare enough across the corpus to have meaningful IDF weight. The guiding principle: favor nouns and domain-specific terms; avoid verbs, prepositions, articles, and filler.

**Keep queries short and precise — typically 2 to 4 terms.** BM25 scores each term independently and sums the contributions. Adding more terms doesn't make the query "more specific" the way it would with semantic search — it adds more independent scoring signals. Every additional term that isn't actually present in the target document dilutes the score contribution of the terms that are present, because the document gets no boost from the missing terms while other documents that happen to contain the extra term might score higher. Three well-chosen terms almost always outperform six loosely related ones.

**Think in terms of discriminating power.** Not all terms contribute equally. A term that appears in 2 out of 50 documents contributes far more to ranking than one that appears in 40 out of 50. The agent should prioritize terms that are likely to be _distinctive_ to the documents it wants. API names, configuration keys, specific error identifiers, library names, named concepts — these are high-IDF terms that do the real work. Generic terms like `function`, `example`, `using`, `error`, `file` are probably in half the corpus and contribute almost nothing to discrimination.

**Prefer the canonical/exact term over synonyms or abbreviations.** BM25 has no synonym awareness. If the documentation says `environment variables`, querying `env vars` will match neither — `env` and `vars` are different tokens than `environment` and `variables`. The agent should use the terminology it expects the documentation to use. If unsure, err toward the formal/unabbreviated form, since reference documentation tends to use full terms.

**Do not wrap queries in question form.** `bmgrep "what is the syntax for pattern matching"` wastes four of its eight tokens on terms (`what`, `is`, `the`, `for`) that carry zero discriminating power. `bmgrep "syntax pattern matching"` is the same query with all the noise stripped out.

## Flag Usage Guidelines

This is about teaching the agent to match its flag choices to its intent at a given point in a workflow. The two modes — ranked list via `--rank` and sampled excerpts via the default — serve different purposes, and the agent should learn when each is appropriate.

**Use `--rank` for orientation and triage.** When the agent is early in a task and needs to understand what's available — which documents in the corpus are relevant to the problem space — `--rank` is the right tool. It's fast, low-noise, and gives the agent a map of the corpus relative to its query. A typical pattern: `bmgrep "streaming response handlers" --rank 5` to see which documents are most relevant, then follow up with a Read tool call on the top result. The agent shouldn't be pulling samples when it just needs to know _which file to open_.

**Use the default sample mode when the agent needs to assess content before committing to a full read.** Samples serve as a preview mechanism — they let the agent see whether a document's relevant content is actually useful before spending context window on reading the whole file. This is the mode to use when the agent has already identified likely-relevant documents (perhaps from a prior `--rank` call) and wants to verify relevance or locate the specific section it needs.

**`--lines` should scale with expected content density.** For prose documentation, 3 to 5 lines is usually enough to establish whether a passage is relevant. For code-heavy documents where a useful example might span a function definition, 8 to 12 lines might be more appropriate. The agent should think about what kind of content it expects to find and size accordingly. Smaller values are almost always preferable — the point is triage, not extraction.

**`--samples` should reflect how much of each document the agent needs to preview.** A single sample (`--samples 1`) tells the agent about the single densest passage. Multiple samples reveal whether the query terms are concentrated in one section or spread across the document. If the agent is trying to determine whether a document is a focused reference on the topic versus one that merely mentions it in passing, `--samples 2` or `--samples 3` is informative — a document with three strong-matching passages is a different signal than one with a single passing mention.

**`--limit` controls breadth of search.** For precise, well-constructed queries, `--limit 2` or `--limit 3` is usually sufficient — the top results will be strongly relevant and the dropoff is steep. For broader or more exploratory queries where the agent isn't sure which documents might be useful, `--limit 5` casts a wider net. Going much beyond that is rarely productive; if the top 5 results don't contain what the agent needs, the query itself likely needs to be reformulated rather than the result set expanded.

## Workflow Patterns Worth Encoding

Beyond individual query and flag guidance, there are a few higher-level patterns that are worth making explicit.

**Narrow, then broaden — don't start broad.** The agent should start with its most specific query and only generalize if results are insufficient. `bmgrep "WebSocket heartbeat timeout" --rank 3` before `bmgrep "WebSocket connection" --rank 5`. The specific query, if it hits, gives a much more precise result. Starting broad wastes a tool call on results the agent then has to sift through.

**Reformulate rather than expanding.** If a query returns poor results, the agent's instinct might be to add more terms. The better strategy is usually to replace terms — try different vocabulary that might appear in the target document. `bmgrep "rate limiting"` returning nothing useful should prompt `bmgrep "throttle requests"` or `bmgrep "request quota"`, not `bmgrep "rate limiting API requests configuration"`.

**Use `--rank` results to inform sample queries.** If `--rank` reveals that relevant documents exist, the agent can follow up with a targeted sample query on those same terms, using `--limit` and `--samples` to get a closer look before deciding whether to do a full read. This two-step pattern — rank for triage, sample for preview, read for full context — is the most context-efficient workflow.

## What to Actually Put in the Skill File

All of the above, but compressed ruthlessly. Agents work best with terse, example-driven instructions. The conceptual explanation matters for you as the designer, but the skill file should be mostly patterns and anti-patterns with brief rationale. Show a bad query, show the better version, state the principle in one sentence. The agent doesn't need to understand IDF mathematically — it needs to internalize "use specific nouns the document likely contains, not descriptions of what you want to learn."
