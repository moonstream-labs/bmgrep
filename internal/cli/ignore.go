package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moonstream-labs/bmgrep/internal/ingest"
)

func newIgnoreCmd(app *App) *cobra.Command {
	ignoreCmd := &cobra.Command{
		Use:   "ignore",
		Short: "Manage .bmgrepignore patterns for the active collection",
		Long: `Ignore patterns use .gitignore-style syntax and are stored in the
active collection's primary directory source .bmgrepignore file.

Active collection resolution:
  BMGREP_COLLECTION -> persistent default collection in the database.`,
		Example: strings.TrimSpace(`
  bmgrep ignore list
  bmgrep ignore add "archive/**" "**/draft-*.md"
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
	return &cobra.Command{
		Use:   "list",
		Short: "List current ignore patterns",
		Example: strings.TrimSpace(`
  bmgrep ignore list
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			collection, err := app.resolveCollection("")
			if err != nil {
				return err
			}
			source, err := app.Store.PrimaryDirectorySource(collection.ID)
			if err != nil {
				return err
			}
			if _, err := ingest.EnsureIgnoreFile(source.SourcePath); err != nil {
				return err
			}

			patterns, err := ingest.ReadIgnorePatterns(source.IgnoreFilePath)
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
}

func newIgnorePathCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the active .bmgrepignore path",
		Example: strings.TrimSpace(`
  bmgrep ignore path
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			collection, err := app.resolveCollection("")
			if err != nil {
				return err
			}
			source, err := app.Store.PrimaryDirectorySource(collection.ID)
			if err != nil {
				return err
			}
			if _, err := ingest.EnsureIgnoreFile(source.SourcePath); err != nil {
				return err
			}
			fmt.Println(source.IgnoreFilePath)
			return nil
		},
	}
}

func newIgnoreAddCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "add <pattern...>",
		Short: "Append ignore patterns",
		Example: strings.TrimSpace(`
  bmgrep ignore add "archive/**"
  bmgrep ignore add "**/draft-*.md" "**/tmp/**"
`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return addIgnorePatterns(app, args)
		},
	}
}

func newIgnoreRemoveCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <pattern...>",
		Short: "Remove ignore patterns by exact line match",
		Example: strings.TrimSpace(`
  bmgrep ignore remove "archive/**"
  bmgrep ignore remove "**/draft-*.md" "**/tmp/**"
`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			collection, err := app.resolveCollection("")
			if err != nil {
				return err
			}
			source, err := app.Store.PrimaryDirectorySource(collection.ID)
			if err != nil {
				return err
			}
			if _, err := ingest.EnsureIgnoreFile(source.SourcePath); err != nil {
				return err
			}

			if err := ingest.RemoveIgnorePatterns(source.IgnoreFilePath, args); err != nil {
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
}

func addIgnorePatterns(app *App, patterns []string) error {
	collection, err := app.resolveCollection("")
	if err != nil {
		return err
	}
	source, err := app.Store.PrimaryDirectorySource(collection.ID)
	if err != nil {
		return err
	}
	if _, err := ingest.EnsureIgnoreFile(source.SourcePath); err != nil {
		return err
	}

	if err := ingest.AppendIgnorePatterns(source.IgnoreFilePath, patterns); err != nil {
		return err
	}

	stats, err := ingest.ReconcileCollection(app.Store, collection)
	if err != nil {
		return err
	}

	fmt.Printf("Added %d pattern(s)\n", len(patterns))
	fmt.Printf("reindexed: +%d ~%d -%d\n", stats.Added, stats.Updated, stats.Deleted)
	return nil
}
