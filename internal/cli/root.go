// Package cli wires bmgrep's command surface.
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moonstream-labs/bmgrep/internal/config"
	"github.com/moonstream-labs/bmgrep/internal/ingest"
	"github.com/moonstream-labs/bmgrep/internal/paths"
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
		Long: `bmgrep is a local-first, SQLite-backed documentation search tool for agents.

It supports two modes:
  1) Sample mode (default): ranked files + line-numbered excerpt windows.
  2) Rank mode (--rank): index-only triage output with line and match counts.

Query guidance for BM25:
  - Use specific nouns and canonical terms from docs.
  - Keep queries short (2-4 terms) for strongest discrimination.
  - Reformulate terms when results are weak; avoid long natural-language questions.

Before every search, bmgrep performs a fast reconcile for the default collection
to ingest new/changed files and remove deleted/ignored files.

Mode rules:
  - Sample mode uses: --limit/-n, --lines/-l, --samples/-s
  - Rank mode uses: --rank <n>
  - --rank cannot be combined with sample-mode flags.`,
		Example: strings.TrimSpace(`
  # Sample mode: excerpts
  bmgrep "authentication middleware" -n 2 -l 4 -s 2

  # Rank mode: fast triage
  bmgrep "authentication middleware" --rank 5

  # Create and activate a collection
  bmgrep collection create docs --path /home/user/reference/docs
  bmgrep collection set docs
`),
		Args: cobra.ArbitraryArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if !needsRuntime(cmd) {
				return nil
			}

			cfgPath, err := config.ResolvePath(flagConfig)
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			dbPath, err := resolveDBPath(flagDB)
			if err != nil {
				return err
			}
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}

			app.ConfigPath = cfgPath
			app.DBPath = dbPath
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

			if flagRank < 0 {
				return fmt.Errorf("--rank must be >= 0")
			}

			if flagRank > 0 {
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

			if flagRank > 0 {
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

			results := make([]search.SampleResult, 0, len(docs))
			for _, d := range docs {
				windows := search.ExtractTopWindows(d.RawContent, queryTerms, flagLines, flagSamples)
				if len(windows) == 0 {
					continue
				}
				results = append(results, search.SampleResult{Path: d.Path, Windows: windows})
			}

			fmt.Print(search.FormatSampleOutput(results, total))
			return nil
		},
	}

	root.PersistentFlags().StringVar(&flagConfig, "config", "", "path to config file (default: ~/.config/bmgrep/config.yaml)")
	root.PersistentFlags().StringVar(&flagDB, "db", "", "path to SQLite database (default: ~/.local/share/bmgrep/bmgrep.db)")

	root.Flags().IntVarP(&flagLimit, "limit", "n", defaultLimit, "number of ranked documents in sample mode")
	root.Flags().IntVarP(&flagLines, "lines", "l", defaultLines, "excerpt lines per sample window")
	root.Flags().IntVarP(&flagSamples, "samples", "s", defaultSamples, "non-overlapping sample windows per result")
	root.Flags().IntVar(&flagRank, "rank", 0, "rank mode: show top N documents without excerpts")

	root.AddCommand(newCollectionCmd(app), newIgnoreCmd(app))

	return root.Execute()
}

func needsRuntime(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if cmd.Name() == "help" || cmd.Name() == "completion" {
		return false
	}
	if cmd.RunE == nil && cmd.Run == nil {
		return false
	}
	return true
}

func resolveDBPath(flag string) (string, error) {
	if strings.TrimSpace(flag) != "" {
		return expandAndAbs(flag)
	}
	if env := strings.TrimSpace(os.Getenv("BMGREP_DB")); env != "" {
		return expandAndAbs(env)
	}
	p, err := paths.DefaultDBPath()
	if err != nil {
		return "", err
	}
	return p, nil
}

func expandAndAbs(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return filepath.Abs(path)
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
