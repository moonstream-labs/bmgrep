// Package cli wires bmgrep's command surface.
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moonstream-labs/bmgrep/internal/ingest"
	"github.com/moonstream-labs/bmgrep/internal/search"
	"github.com/moonstream-labs/bmgrep/internal/store"
)

const (
	defaultLimit   = 5
	defaultLines   = 4
	defaultSamples = 1
)

// App holds runtime dependencies resolved from flags and environment.
type App struct {
	DBPath    string
	DBFrom    string
	Workspace string
	Store     *store.Store
}

// Execute builds the root command and runs it.
func Execute() error {
	app := &App{}

	var flagDB string
	var flagCollection string

	var flagLimit int
	var flagLines int
	var flagSamples int
	var flagRank int
	var flagMatch string
	var flagMeta bool

	root := &cobra.Command{
		Use:   "bmgrep [query terms]",
		Short: "Fast BM25 search over local Markdown collections",
		Long: `bmgrep is a local-first, SQLite-backed BM25 search tool for agents.

Modes:
  Sample (default)  Ranked files with line-numbered excerpt windows.
  Rank (--rank N)   Index-only triage: path, line count, match count
                    (title-weighted BM25 when frontmatter title exists).
  --rank cannot be combined with --limit, --lines, or --samples.
  --meta            Surface title/description/references from YAML frontmatter
                    (sample mode shows title/references only).
                    source_url is intentionally omitted.

Term matching:
  --match all   Strict all-term match (FTS5 AND).
  --match any   Any-term match (FTS5 OR).
  --match auto  Try all-term first; if 0 results on a multi-term query,
                 retry as any-term and mark output with fallback notice.
                 Fallback marker line:
                   match: any-term fallback (auto; no all-term matches)
                 In rank mode, effective any-term matching also shows term
                 coverage per result (for example: 1/2 terms).

Query construction (BM25 is term-matching, not semantic search):
  - Use the vocabulary of the documents, not the vocabulary of the task.
    Good:  bmgrep "authentication middleware configuration"
    Bad:   bmgrep "how to set up auth"
  - Keep queries to 2-4 specific, high-discrimination terms.
    Nouns and domain terms work best. Avoid verbs, prepositions, filler.
  - Prefer canonical/unabbreviated terms. BM25 has no synonym awareness:
    "environment variables" matches; "env vars" does not.
  - Do not wrap queries in question form — "what", "is", "the", "for"
    carry zero discriminating power.
  - If results are poor, reformulate with different terms rather than
    adding more terms to the same query.

Workflow patterns:
  1. --rank for triage: identify which documents are relevant.
  2. Sample mode to preview passages before committing to a full read.
  3. Narrow first, then broaden. Start with the most specific query.

Path resolution:
  Runtime database paths are resolved by precedence:
  --db flag -> BMGREP_DB -> nearest .bmgrep/bmgrep.db -> global default.
  Run bmgrep db current to inspect active resolution.

Collection resolution:
  Query target collection is resolved by precedence:
  --collection -> BMGREP_COLLECTION -> persistent default in the database.

Before every search, bmgrep reconciles the active target collection to ingest
new/changed files and remove deleted/ignored ones.`,
		Example: strings.TrimSpace(`
  # Sample mode: excerpts
  bmgrep "authentication middleware" -n 2 -l 4 -s 2

  # Rank mode: fast triage
  bmgrep "authentication middleware" --rank 5

  # Include frontmatter metadata in output
  bmgrep "authentication middleware" --rank 5 --meta
  bmgrep "authentication middleware" -n 2 -l 4 -s 1 --meta

  # Match modes
  bmgrep "SkillsBench decomposition" --rank 5 --match all
  bmgrep "SkillsBench decomposition" --rank 5 --match any
  bmgrep "SkillsBench decomposition" --rank 5 --match auto

  # Non-persistent collection override for this query
  bmgrep "authentication middleware" --collection docs-v2 --rank 5

  # Create and activate a collection
  bmgrep collection create docs --path /home/user/reference/docs
  bmgrep collection set docs

  # Initialize workspace-local state and inspect active runtime paths
  bmgrep db init
  bmgrep db current
`),
		Args: cobra.ArbitraryArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if !needsRuntime(cmd) {
				return nil
			}

			resolved, err := resolveDBRuntimePath(flagDB)
			if err != nil {
				return err
			}

			st, err := store.Open(resolved.DBPath)
			if err != nil {
				return err
			}

			app.DBPath = resolved.DBPath
			app.DBFrom = resolved.DBSource
			app.Workspace = resolved.Workspace
			app.Store = st
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if app.Store != nil {
				return app.Store.Close()
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}

			rankExplicit := cmd.Flags().Changed("rank")

			if rankExplicit {
				if flagRank < 1 {
					return fmt.Errorf("--rank must be >= 1")
				}
				if cmd.Flags().Changed("limit") || cmd.Flags().Changed("lines") || cmd.Flags().Changed("samples") {
					return fmt.Errorf("--rank is mutually exclusive with --limit, --lines, and --samples")
				}
			} else {
				if flagLimit <= 0 {
					return fmt.Errorf("--limit must be >= 1")
				}
				if flagLines <= 0 {
					return fmt.Errorf("--lines must be >= 1")
				}
				if flagSamples <= 0 {
					return fmt.Errorf("--samples must be >= 1")
				}
			}

			matchMode, err := search.ParseMatchMode(flagMatch)
			if err != nil {
				return err
			}

			queryTerms, _ := search.NormalizePlainQuery(strings.Join(args, " "))
			if len(queryTerms) == 0 {
				return fmt.Errorf("query contains no searchable terms")
			}

			collection, err := app.resolveCollection(flagCollection)
			if err != nil {
				return err
			}

			if _, err := ingest.ReconcileCollection(app.Store, collection); err != nil {
				return fmt.Errorf("reconcile collection %q: %w", collection.Name, err)
			}

			if rankExplicit {
				autoFallback := false
				effectiveMode := matchMode

				ftsQuery := search.BuildFTSQuery(queryTerms, matchMode)
				showCoverage := matchMode == search.MatchAny && len(queryTerms) >= 2
				docs, total, err := app.Store.SearchRankedDocsWithTerms(collection.ID, ftsQuery, queryTerms, flagRank, showCoverage)
				if err != nil {
					return err
				}

				if matchMode == search.MatchAuto {
					effectiveMode = search.MatchAll
					if total == 0 && len(queryTerms) >= 2 {
						autoFallback = true
						effectiveMode = search.MatchAny
						ftsQuery = search.BuildFTSQuery(queryTerms, search.MatchAny)
						docs, total, err = app.Store.SearchRankedDocsWithTerms(collection.ID, ftsQuery, queryTerms, flagRank, true)
						if err != nil {
							return err
						}
					}
				}

				showCoverage = effectiveMode == search.MatchAny && len(queryTerms) >= 2

				var metaByPath map[string]search.DocMeta
				if flagMeta && len(docs) > 0 {
					docIDs := make([]int64, 0, len(docs))
					for _, d := range docs {
						docIDs = append(docIDs, d.ID)
					}

					rawByID, err := app.Store.GetRawContentByDocIDs(collection.ID, docIDs)
					if err != nil {
						return fmt.Errorf("load rank metadata: %w", err)
					}

					metaByPath = make(map[string]search.DocMeta, len(docs))
					for _, d := range docs {
						raw, ok := rawByID[d.ID]
						if !ok {
							continue
						}
						meta := search.ExtractFrontmatter(raw)
						if meta.Title == "" && meta.Description == "" && meta.References <= 0 {
							continue
						}
						metaByPath[d.Path] = meta
					}
				}

				out := search.FormatRankOutputWithOptions(docs, total, search.RankOutputOptions{
					Match:            search.MatchInfo{AutoFallback: autoFallback},
					ShowTermCoverage: showCoverage,
					QueryTermCount:   len(queryTerms),
					ShowMeta:         flagMeta,
					MetaByPath:       metaByPath,
				})
				fmt.Print(out)
				return nil
			}

			autoFallback := false
			ftsQuery := search.BuildFTSQuery(queryTerms, matchMode)
			docs, total, err := app.Store.SearchSampleDocs(collection.ID, ftsQuery, flagLimit)
			if err != nil {
				return err
			}

			if matchMode == search.MatchAuto && total == 0 && len(queryTerms) >= 2 {
				autoFallback = true
				ftsQuery = search.BuildFTSQuery(queryTerms, search.MatchAny)
				docs, total, err = app.Store.SearchSampleDocs(collection.ID, ftsQuery, flagLimit)
				if err != nil {
					return err
				}
			}

			weights, err := app.Store.TermIDFWeights(collection.ID, queryTerms)
			if err != nil {
				return fmt.Errorf("compute IDF weights: %w", err)
			}

			results := make([]search.SampleResult, 0, len(docs))
			var metaByPath map[string]search.DocMeta
			if flagMeta {
				metaByPath = make(map[string]search.DocMeta, len(docs))
			}
			for _, d := range docs {
				var meta search.DocMeta
				if flagMeta {
					meta = search.ExtractFrontmatter(d.RawContent)
				}

				windows := search.ExtractTopWindows(d.RawContent, queryTerms, weights, flagLines, flagSamples)
				if len(windows) == 0 {
					if flagMeta && (meta.Title != "" || meta.References > 0) {
						results = append(results, search.SampleResult{Path: d.Path, Windows: windows})
						metaByPath[d.Path] = meta
					}
					continue
				}
				results = append(results, search.SampleResult{Path: d.Path, Windows: windows})
				if flagMeta {
					if meta.Title != "" || meta.References > 0 {
						metaByPath[d.Path] = meta
					}
				}
			}

			fmt.Print(search.FormatSampleOutputWithOptions(results, total, search.SampleOutputOptions{
				Match:      search.MatchInfo{AutoFallback: autoFallback},
				ShowMeta:   flagMeta,
				MetaByPath: metaByPath,
			}))
			return nil
		},
	}

	root.PersistentFlags().StringVar(&flagDB, "db", "", "database path override (highest precedence; inspect with bmgrep db current)")

	root.Flags().IntVarP(&flagLimit, "limit", "n", defaultLimit, "number of ranked documents in sample mode")
	root.Flags().IntVarP(&flagLines, "lines", "l", defaultLines, "excerpt lines per sample window")
	root.Flags().IntVarP(&flagSamples, "samples", "s", defaultSamples, "non-overlapping sample windows per result")
	root.Flags().IntVar(&flagRank, "rank", 0, "rank mode: show top N documents without excerpts")
	root.Flags().StringVar(&flagMatch, "match", string(search.MatchAuto), "term matching: all (AND), any (OR), or auto (AND first, OR fallback on zero multi-term hits; prints fallback marker)")
	root.Flags().BoolVar(&flagMeta, "meta", false, "show frontmatter metadata (rank: title+description+references, sample: title+references; source_url omitted)")
	root.Flags().StringVar(&flagCollection, "collection", "", "query collection override (non-persistent; also supports BMGREP_COLLECTION)")

	root.AddCommand(newCollectionCmd(app), newIgnoreCmd(app), newDBCmd(app, &flagDB))

	return root.Execute()
}

func needsRuntime(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if cmd.Name() == "help" || cmd.Name() == "completion" {
		return false
	}
	if cmd.Name() == "db" || (cmd.Parent() != nil && cmd.Parent().Name() == "db") {
		return false
	}
	if cmd.RunE == nil && cmd.Run == nil {
		return false
	}
	return true
}

func (a *App) resolveCollection(explicit string) (store.Collection, error) {
	if a.Store == nil {
		return store.Collection{}, errors.New("runtime store not loaded")
	}

	if name := strings.TrimSpace(explicit); name != "" {
		return a.Store.GetCollectionByName(name)
	}

	if envName := strings.TrimSpace(os.Getenv("BMGREP_COLLECTION")); envName != "" {
		return a.Store.GetCollectionByName(envName)
	}

	collection, err := a.Store.GetDefaultCollection()
	if err != nil {
		if errors.Is(err, store.ErrNoDefaultCollection) {
			return store.Collection{}, fmt.Errorf("no default collection set; run: bmgrep collection set <name> or pass --collection")
		}
		return store.Collection{}, err
	}

	return collection, nil
}
