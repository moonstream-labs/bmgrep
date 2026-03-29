# bmgrep - Output Design Guide

---

## Design Principle

Every token in `bmgrep` output must either directly feed the agent's next action or inform a decision about what that action should be. If a piece of output doesn't map to a concrete decision or tool call, it doesn't belong in the output.

Terminal agents consume tool output under constant context-window pressure. Each line, each whitespace character, each decorative separator is a token that displaces actual reasoning capacity. The output format is not a cosmetic choice — it's a resource allocation decision.

---

## Optimized Output Formats

### Sample Mode (default)

```
$ bmgrep "authentication middleware" --limit 2 --lines 4 --samples 2

results: 2 of 8

[1] /home/moonstream/reference/docs/express-auth.md
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

[2] /home/moonstream/reference/docs/security-overview.md
73-76:
    73	Authentication middleware should be registered before any
    74	route handlers that serve protected resources. Ordering
    75	matters — middleware registered after a route handler
    76	will not intercept requests to that route.
```

### Rank Mode (`--rank`)

```
$ bmgrep "authentication middleware" --rank 5

results: 5 of 12

[1] /home/moonstream/reference/docs/express-auth.md (127 lines, 6 matches)
[2] /home/moonstream/reference/docs/security-overview.md (843 lines, 3 matches)
[3] /home/moonstream/reference/docs/session-management.md (234 lines, 2 matches)
[4] /home/moonstream/reference/docs/api-reference.md (1,509 lines, 2 matches)
[5] /home/moonstream/reference/docs/migration-guide-v3.md (412 lines, 1 match)
```

---

## Element-by-Element Rationale

### `results: n of y`

```
results: 2 of 8
```

This line informs a workflow decision, not a content decision. It tells the agent about **query quality**, not document quality.

- `results: 2 of 2` on a 50-document corpus → the query is highly specific, results are almost certainly on-target.
- `results: 5 of 25` → query terms are broadly distributed, consider refining.
- `results: 0 of 0` → unambiguous signal to reformulate with different terms.

Without this, the agent has no way to gauge whether the results it's seeing represent a precise hit or the top of a long, undifferentiated tail. It directly informs the decision: "should I act on these results, or should I search again with different terms?"

### Rank Index and Path

```
[1] /home/moonstream/reference/docs/express-auth.md
```

The `realpath` is the single most actionable element in the entire output. It feeds directly and without transformation into the agent's next Read tool call. No path resolution, no string extraction, no mental mapping from a short name to a real location.

The rank index `[1]` communicates ordinal position, and ordinal position is the only BM25 signal the agent needs. Raw BM25 scores should never appear in the output — they aren't normalized, they aren't comparable across queries, and the agent can't act on `3.847` in any way that the ordinal ranking doesn't already communicate. Exposing scores invites either misinterpretation (treating the number as a confidence measure) or wasted tokens that the agent ignores.

### Line Range Headers

```
345-348:
   345	The authentication middleware intercepts incoming requests
...
```

The range header `345-348:` exists so the agent can immediately construct a targeted Read call (e.g., "read lines 340-360 of express-auth.md") without scanning the sample content and counting. This becomes increasingly valuable as `--lines` increases. At `--lines 3`, the agent can eyeball the range. At `--lines 12`, the range header saves it from counting twelve lines to determine the span.

The range header also enables a pattern where the agent skims range headers first and sample content second. If the agent sees `345-348:` and `512-515:`, it already knows the matching content is in two separated clusters — that's a structural signal about the document before the agent reads a single line of content.

### `cat -n` Line Format

```
   345	The authentication middleware intercepts incoming requests
```

The numbered line format follows `cat -n` conventions: right-justified line numbers in a fixed-width field, followed by a tab, followed by the line content. This is a deliberate choice rooted in tool-output consistency.

Terminal agents already parse `cat -n` output constantly — it's the format they encounter from Read tool calls, Grep results, and direct file inspection. Using the same convention in `bmgrep` means the agent applies the same parsing logic it already uses everywhere else. There is no format-switching overhead, no edge cases introduced by a novel delimiter, and no risk of an agent misinterpreting a line because the format deviates from what it expects.

The token cost of the padding whitespace is real but modest, and is outweighed by the consistency benefit. A tool whose output looks and parses identically to every other file-inspection tool in the agent's environment reduces the total surface area of formatting conventions the agent needs to handle. In practice, agents are more reliable when tool outputs are uniform than when each tool optimizes its format independently.

### Whitespace Economy

The optimized format eliminates two classes of whitespace present in the initial design while preserving `cat -n` conventions within sample content:

**No blank line between path and first sample.** The transition from a path line (starts with `[`) to a range header (starts with a digit) is already unambiguous. A blank line improves human scanability but adds a token that the agent doesn't need for parsing.

**No blank line between samples within a document.** The range header on its own line is the separator. A preceding blank line is redundant — the new `N-M:` line unambiguously signals the start of a new sample.

**Blank line between documents is preserved.** This is the one separator that earns its token cost. The transition between documents is the highest-level structural boundary in the output, and a single blank line before each `[n]` index line makes it reliably parseable even in edge cases where a sample's last line might contain bracket characters.

### Rank Mode Metadata

```
[1] /home/moonstream/reference/docs/express-auth.md (127 lines, 6 matches)
```

When the agent is in `--rank` mode, it's triaging — deciding which documents to investigate further. Two metadata signals directly inform that decision:

**Line count** tells the agent the context-window cost of reading the file. A 127-line focused reference is a very different commitment than a 1,509-line API reference. If the agent has limited context budget, it might skip the 1,509-line document in favor of a shorter one, or it might use a follow-up sample query to find the specific relevant section in the longer document before committing to a full read.

**Match count** tells the agent whether the document is _about_ the query topic or merely _mentions_ it. `6 matches` means the query terms are distributed throughout the document — it's likely a focused reference on the topic. `1 match` means a single passage mentions the query terms in passing — the document is about something else. This is the difference between "I should read this entire document" and "I should pull a sample to see if that one mention is useful."

These two signals together let the agent make an informed cost/benefit decision: "document 1 is short and densely relevant — read it. Document 4 is long with sparse matches — sample it first. Document 5 has a single match in a medium-length doc — probably skip it unless nothing else pans out."

---

## Decision Scenarios

### Scenario 1: Precise Query, Clear Top Result

```
$ bmgrep "createReadStream backpressure" --limit 3 --lines 4 --samples 2

results: 1 of 1

[1] /home/moonstream/reference/docs/node-streams.md
89-92:
    89	When the writable destination cannot keep pace with the readable
    90	source, createReadStream automatically signals backpressure by
    91	pausing the read operation until the downstream buffer drains
    92	below the high-water mark.
204-207:
   204	const stream = fs.createReadStream(filepath, {
   205	  highWaterMark: 64 * 1024,
   206	  encoding: 'utf8',
   207	});
```

`results: 1 of 1` tells the agent this is the only document containing both terms. There's no need to look further. Two samples show both a prose explanation and a code example — the agent can see that this document covers the topic conceptually and practically. It should immediately read the file, likely starting around line 89.

### Scenario 2: Broad Query, Triage Required

```
$ bmgrep "configuration" --rank 5

results: 5 of 19

[1] /home/moonstream/reference/docs/config-reference.md (312 lines, 14 matches)
[2] /home/moonstream/reference/docs/getting-started.md (89 lines, 5 matches)
[3] /home/moonstream/reference/docs/deployment.md (567 lines, 4 matches)
[4] /home/moonstream/reference/docs/plugin-system.md (201 lines, 2 matches)
[5] /home/moonstream/reference/docs/changelog.md (1,203 lines, 2 matches)
```

`results: 5 of 19` signals that "configuration" is too broad — it appears across most of the corpus. The agent should immediately recognize this and reformulate with more specific terms rather than reading any of these results. If the agent was looking for database configuration specifically, `bmgrep "database connection pool" --rank 3` would be a much better follow-up than opening `config-reference.md` and scanning 312 lines.

However, the metadata still provides useful triage signals if the agent does need to act on these results. Document 1 is a dedicated configuration reference (14 matches in 312 lines — high density). Document 5 is a changelog that mentions configuration twice in 1,203 lines — almost certainly not what the agent wants.

### Scenario 3: Targeted Follow-Up After Rank Triage

```
$ bmgrep "database connection pool" --rank 3

results: 3 of 5

[1] /home/moonstream/reference/docs/config-reference.md (312 lines, 4 matches)
[2] /home/moonstream/reference/docs/deployment.md (567 lines, 3 matches)
[3] /home/moonstream/reference/docs/troubleshooting.md (445 lines, 1 match)
```

The agent refined from "configuration" to "database connection pool" and the result set sharpened from 19 matching documents to 5, with 3 shown. Now it knows where to look. The next move depends on context-window budget. It might read `config-reference.md` directly (312 lines is manageable), or sample `deployment.md` first since 567 lines is a larger commitment:

```
$ bmgrep "database connection pool" --limit 1 --lines 5 --samples 2

results: 1 of 5

[1] /home/moonstream/reference/docs/config-reference.md
145-149:
   145	database:
   146	  pool:
   147	    min: 5
   148	    max: 20
   149	    acquireTimeout: 30000
203-207:
   203	The connection pool settings control how many simultaneous
   204	database connections the application maintains. Setting max
   205	too high can exhaust database server resources; setting it
   206	too low causes request queuing under load. The default of
   207	10 is suitable for most single-instance deployments.
```

Two samples — one showing configuration syntax, one explaining the semantics. The agent now has enough context to decide whether to read the full document or whether these two passages already provide what it needs.

### Scenario 4: No Results, Reformulation Signal

```
$ bmgrep "env vars dotenv" --limit 3 --lines 3 --samples 1

results: 0 of 0
```

Zero results. The agent used abbreviated, informal terms (`env vars`, `dotenv`) that don't appear in the documentation. Recall that BM25 has no synonym awareness — `env` is a different token than `environment`, `vars` is different from `variables`. The agent should reformulate using the canonical terms the documentation likely uses:

```
$ bmgrep "environment variables" --limit 3 --lines 3 --samples 1

results: 2 of 4

[1] /home/moonstream/reference/docs/config-reference.md
34-36:
    34	Environment variables take precedence over values defined
    35	in configuration files. All environment variables use the
    36	prefix APP_ followed by the uppercase setting name.
```

The reformulated query with full, unabbreviated terms finds the relevant documentation immediately.

### Scenario 5: Single Sample Sufficiency

```
$ bmgrep "CORS preflight OPTIONS" --limit 2 --lines 5 --samples 1

results: 2 of 3

[1] /home/moonstream/reference/docs/security-overview.md
301-305:
   301	CORS preflight requests (HTTP OPTIONS) are handled by the
   302	corsMiddleware before reaching route handlers. The middleware
   303	responds with appropriate Access-Control-Allow-* headers
   304	based on the origin whitelist defined in config.cors.origins.
   305	Preflight responses are cached for 86400 seconds by default.

[2] /home/moonstream/reference/docs/api-reference.md
78-82:
    78	OPTIONS requests to any /api/* endpoint return CORS headers
    79	without authentication. This is necessary because browsers
    80	send preflight requests without credentials, and rejecting
    81	them would block legitimate cross-origin requests from
    82	permitted origins.
```

With `--samples 1`, each document contributes its single most term-dense passage. This is the right flag choice when the agent wants a fast relevance check across multiple documents rather than a deep look into any single one. The agent can see that document 1 covers configuration, document 2 covers the authentication interaction — and it can decide which angle it needs without having read either file in full.