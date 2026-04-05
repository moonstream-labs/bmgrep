package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moonstream-labs/bmgrep/internal/ingest"
	"github.com/moonstream-labs/bmgrep/internal/store"
)

func newIgnoreCmd(app *App) *cobra.Command {
	ignoreCmd := &cobra.Command{
		Use:   "ignore",
		Short: "Manage .bmignore patterns for the active collection",
		Long: `Ignore patterns use .gitignore-style syntax and are stored in the
.bmignore file of the targeted directory source. By default, patterns
target the primary (first) directory source. Use --source to target
a specific directory source by path.

The collection must have at least one directory source. File sources
do not support ignore patterns.

Active collection resolution:
  BMGREP_COLLECTION -> persistent default collection in the database.`,
		Example: strings.TrimSpace(`
  bmgrep ignore list
  bmgrep ignore list --source ~/agents/docs/nuxt
  bmgrep ignore add "archive/**" "**/draft-*.md"
  bmgrep ignore add --source ~/agents/docs/nuxt "drafts/**"
`),
	}

	ignoreCmd.AddCommand(
		newIgnoreListCmd(app),
		newIgnorePathCmd(app),
		newIgnoreAddCmd(app),
		newIgnoreRemoveCmd(app),
	)

	return ignoreCmd
}

func newIgnoreListCmd(app *App) *cobra.Command {
	var flagSource string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List current ignore patterns",
		Example: strings.TrimSpace(`
  bmgrep ignore list
  bmgrep ignore list --source ~/agents/docs/nuxt
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			collection, err := app.resolveCollection("")
			if err != nil {
				return err
			}
			ignorePath, err := resolveIgnoreFilePath(app, collection.ID, flagSource)
			if err != nil {
				return err
			}

			patterns, err := ingest.ReadIgnorePatterns(ignorePath)
			if err != nil {
				return err
			}
			if len(patterns) == 0 {
				fmt.Println("No ignore patterns.")
				return nil
			}

			for _, p := range patterns {
				fmt.Println(p)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&flagSource, "source", "", "directory source path for ignore targeting")
	return cmd
}

func newIgnorePathCmd(app *App) *cobra.Command {
	var flagSource string

	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print the active .bmignore path",
		Example: strings.TrimSpace(`
  bmgrep ignore path
  bmgrep ignore path --source ~/agents/docs/nuxt
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			collection, err := app.resolveCollection("")
			if err != nil {
				return err
			}
			ignorePath, err := resolveIgnoreFilePath(app, collection.ID, flagSource)
			if err != nil {
				return err
			}
			fmt.Println(app.displayPath(ignorePath))
			return nil
		},
	}

	cmd.Flags().StringVar(&flagSource, "source", "", "directory source path for ignore targeting")
	return cmd
}

func newIgnoreAddCmd(app *App) *cobra.Command {
	var flagSource string

	cmd := &cobra.Command{
		Use:   "add <pattern...>",
		Short: "Append ignore patterns",
		Example: strings.TrimSpace(`
  bmgrep ignore add "archive/**"
  bmgrep ignore add "**/draft-*.md" "**/tmp/**"
  bmgrep ignore add --source ~/agents/docs/nuxt "drafts/**"
`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			collection, err := app.resolveCollection("")
			if err != nil {
				return err
			}
			ignorePath, err := resolveIgnoreFilePath(app, collection.ID, flagSource)
			if err != nil {
				return err
			}

			if err := ingest.AppendIgnorePatterns(ignorePath, args); err != nil {
				return err
			}

			stats, err := ingest.ReconcileCollection(app.Store, collection)
			if err != nil {
				return err
			}

			fmt.Printf("Added %d pattern(s)\n", len(args))
			fmt.Printf("reindexed: +%d ~%d -%d\n", stats.Added, stats.Updated, stats.Deleted)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagSource, "source", "", "directory source path for ignore targeting")
	return cmd
}

func newIgnoreRemoveCmd(app *App) *cobra.Command {
	var flagSource string

	cmd := &cobra.Command{
		Use:   "remove <pattern...>",
		Short: "Remove ignore patterns by exact line match",
		Example: strings.TrimSpace(`
  bmgrep ignore remove "archive/**"
  bmgrep ignore remove "**/draft-*.md" "**/tmp/**"
  bmgrep ignore remove --source ~/agents/docs/nuxt "drafts/**"
`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			collection, err := app.resolveCollection("")
			if err != nil {
				return err
			}
			ignorePath, err := resolveIgnoreFilePath(app, collection.ID, flagSource)
			if err != nil {
				return err
			}

			if err := ingest.RemoveIgnorePatterns(ignorePath, args); err != nil {
				return err
			}

			stats, err := ingest.ReconcileCollection(app.Store, collection)
			if err != nil {
				return err
			}

			fmt.Printf("Removed %d pattern(s)\n", len(args))
			fmt.Printf("reindexed: +%d ~%d -%d\n", stats.Added, stats.Updated, stats.Deleted)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagSource, "source", "", "directory source path for ignore targeting")
	return cmd
}

func resolveIgnoreFilePath(app *App, collectionID int64, sourcePath string) (string, error) {
	var source store.CollectionSource
	var err error

	if strings.TrimSpace(sourcePath) != "" {
		resolved, _, resolveErr := resolveSourcePathBestEffort(sourcePath)
		if resolveErr != nil {
			return "", resolveErr
		}
		source, err = app.Store.GetCollectionSourceByPath(collectionID, resolved)
		if err != nil {
			return "", err
		}
		if source.SourceType != store.SourceTypeDirectory {
			return "", fmt.Errorf("source %q is a file source; ignore patterns apply only to directory sources", resolved)
		}
	} else {
		source, err = app.Store.PrimaryDirectorySource(collectionID)
		if err != nil {
			return "", err
		}
	}

	ignorePath, err := ingest.EnsureIgnoreFile(source.SourcePath)
	if err != nil {
		return "", err
	}
	return ignorePath, nil
}
