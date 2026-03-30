// Package cli wires bmgrep's command surface.
package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moonstream-labs/bmgrep/internal/config"
	"github.com/moonstream-labs/bmgrep/internal/dbprofile"
	"github.com/moonstream-labs/bmgrep/internal/ingest"
	"github.com/moonstream-labs/bmgrep/internal/search"
	"github.com/moonstream-labs/bmgrep/internal/store"
)

const (
	defaultLimit   = 5
	defaultLines   = 4
	defaultSamples = 1
)

// App holds runtime dependencies resolved from config and flags.
type App struct {
	ConfigPath string
	DBPath     string
	ConfigFrom string
	DBFrom     string
	Workspace  string
	Config     *config.Config
	Store      *store.Store
}

// Execute builds the root command and runs it.
func Execute() error {
	app := &App{}

	var flagConfig string
	var flagDB string

	var flagLimit int
	var flagLines int
	var flagSamples int
	var flagRank int

	root := &cobra.Command{
		Use:   "bmgrep [query terms]",
		Short: "Fast BM25 search over local Markdown collections",
		Long: `bmgrep is a local-first, SQLite-backed BM25 search tool for agents.

Modes:
  Sample (default)  Ranked files with line-numbered excerpt windows.
  Rank (--rank N)   Index-only triage: path, line count, match count.
  --rank cannot be combined with --limit, --lines, or --samples.

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
  Runtime config/database paths are resolved by precedence:
  flags -> env vars -> workspace profile -> workspace files ->
  global profile -> global defaults.
  Run bmgrep db current to inspect active resolution.

Before every search, bmgrep reconciles the default collection to ingest
new/changed files and remove deleted/ignored ones.`,
		Example: strings.TrimSpace(`
  # Sample mode: excerpts
  bmgrep "authentication middleware" -n 2 -l 4 -s 2

  # Rank mode: fast triage
  bmgrep "authentication middleware" --rank 5

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

			resolved, err := dbprofile.ResolvePaths(flagConfig, flagDB)
			if err != nil {
				return err
			}

			cfg, err := config.Load(resolved.ConfigPath)
			if err != nil {
				return err
			}

			st, err := store.Open(resolved.DBPath)
			if err != nil {
				return err
			}

			app.ConfigPath = resolved.ConfigPath
			app.DBPath = resolved.DBPath
			app.ConfigFrom = resolved.ConfigSource
			app.DBFrom = resolved.DBSource
			app.Workspace = resolved.Workspace
			app.Config = cfg
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

			queryTerms, ftsQuery := search.NormalizePlainQuery(strings.Join(args, " "))
			if len(queryTerms) == 0 {
				return fmt.Errorf("query contains no searchable terms")
			}

			collection, err := app.requireDefaultCollection()
			if err != nil {
				return err
			}

			if _, err := ingest.ReconcileCollection(app.Store, collection); err != nil {
				return fmt.Errorf("reconcile collection %q: %w", collection.Name, err)
			}

			if rankExplicit {
				docs, total, err := app.Store.SearchRankedDocs(collection.ID, ftsQuery, flagRank)
				if err != nil {
					return err
				}
				fmt.Print(search.FormatRankOutput(docs, total))
				return nil
			}

			docs, total, err := app.Store.SearchSampleDocs(collection.ID, ftsQuery, flagLimit)
			if err != nil {
				return err
			}

			weights, err := app.Store.TermIDFWeights(collection.ID, queryTerms)
			if err != nil {
				return fmt.Errorf("compute IDF weights: %w", err)
			}

			results := make([]search.SampleResult, 0, len(docs))
			for _, d := range docs {
				windows := search.ExtractTopWindows(d.RawContent, queryTerms, weights, flagLines, flagSamples)
				if len(windows) == 0 {
					continue
				}
				results = append(results, search.SampleResult{Path: d.Path, Windows: windows})
			}

			fmt.Print(search.FormatSampleOutput(results, total))
			return nil
		},
	}

	root.PersistentFlags().StringVar(&flagConfig, "config", "", "config path override (highest precedence; inspect with bmgrep db current)")
	root.PersistentFlags().StringVar(&flagDB, "db", "", "database path override (highest precedence; inspect with bmgrep db current)")

	root.Flags().IntVarP(&flagLimit, "limit", "n", defaultLimit, "number of ranked documents in sample mode")
	root.Flags().IntVarP(&flagLines, "lines", "l", defaultLines, "excerpt lines per sample window")
	root.Flags().IntVarP(&flagSamples, "samples", "s", defaultSamples, "non-overlapping sample windows per result")
	root.Flags().IntVar(&flagRank, "rank", 0, "rank mode: show top N documents without excerpts")

	root.AddCommand(newCollectionCmd(app), newIgnoreCmd(app), newDBCmd(app, &flagConfig, &flagDB))

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

func (a *App) requireDefaultCollection() (store.Collection, error) {
	if a.Config == nil {
		return store.Collection{}, errors.New("runtime config not loaded")
	}
	if strings.TrimSpace(a.Config.DefaultCollection) == "" {
		return store.Collection{}, fmt.Errorf("no default collection set; run: bmgrep collection set <name>")
	}
	return a.Store.GetCollectionByName(a.Config.DefaultCollection)
}
