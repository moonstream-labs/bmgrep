# Agent Workflow Observations — Claude Opus Session

**Date:** 2026-04-04
**Context:** Extended session setting up a multi-source bmgrep workspace
for a monorepo project (1,225 docs across 8 trawl-sourced documentation
sets). Used bmgrep throughout for search validation and query testing.

## What works well

**Rank/sample/read progression.** The three-step workflow is effective
for context gathering. `--rank` for fast triage costs minimal tokens,
and the line count + match count heuristic provides a reliable signal
for whether to sample or read directly. A 57-line file with 17 matches
is obviously worth reading whole; a 455-line file with 57 matches
warrants a sample first.

**Term coverage on fallback queries.** The `3/4 terms` indicator on
auto-fallback results is a strong signal for whether a partial match
is worth pursuing. It lets me skip low-coverage noise without reading.

**Auto-reconciliation.** Never having to think about re-indexing after
docs change is the right design. Files were added and removed from the
collection directory throughout the session, and every subsequent query
just worked.

**Match mode defaults.** `--match auto` does the right thing — precise
when it can be, graceful fallback when it can't. Never needed to reach
for `--match all` or `--match any` explicitly, which is a sign the
default is well-calibrated.

**Relative path display.** The `./reference/docs/...` paths in results
are immediately actionable — I can pass them directly to a Read tool
without path manipulation.

## Friction points

**No collection introspection.** When unfamiliar with what's indexed,
I have to guess at vocabulary. There's no way to ask "what's in this
collection?" without going to the filesystem. A `collection stats`
command showing subdirectory breakdown, document counts per subtree,
and possibly top terms would help construct better first queries.

**No path-scoped queries.** In a single-source collection spanning
multiple documentation sets (e.g., cloudflare-workers + nuxt + mise),
I can't restrict a search to a subtree. If I know my answer is in the
Cloudflare Workers docs, I'm still searching across all 1,225 docs.
The IDF landscape is shared, which can push Nuxt results ahead of CF
results for terms like "middleware" that are common in both. Something
like `bmgrep "middleware" --path cloudflare-workers/` would let me keep
single-collection simplicity while getting focused IDF when needed.

**--meta context cost.** The backlink counts from `--meta` are useful
for identifying foundational pages, but the extra output lines per
result add up. In practice I rarely use it because the context cost
outweighs the signal for most queries. Not sure what the fix is —
maybe a compact inline format like `[1] ./path (168 lines, 25 matches, 14↗)`.

## Collection design feedback

The single top-level `--path` pointing at a parent directory
(`reference/docs/`) with auto-discovery of subdirectories is the right
default. It means adding a new documentation source is just `cp -r`
into the directory — no bmgrep commands needed. This was validated
repeatedly during the session when adding mise, obsidian-help, and
openpanel docs.

The tension is between this simplicity and the IDF implications of a
large mixed-domain collection. Path-scoped queries would resolve this
without requiring the operator to split into multiple collections.
