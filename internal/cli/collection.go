package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moonstream-labs/bmgrep/internal/ingest"
	"github.com/moonstream-labs/bmgrep/internal/paths"
	"github.com/moonstream-labs/bmgrep/internal/store"
)

func newCollectionCmd(app *App) *cobra.Command {
	var flagJSON bool

	collectionCmd := &cobra.Command{
		Use:   "collection",
		Short: "Manage indexed markdown collections",
		Long: `Collection commands control which markdown sources are indexed
and which collection is searched by default.

Collections are source sets. Start with collection create --path for an
initial directory source, then use collection add to curate additional
directory/file sources from anywhere on disk.`,
	}

	collectionCmd.AddCommand(
		newCollectionListCmd(app, &flagJSON),
		newCollectionCreateCmd(app),
		newCollectionAddSourceCmd(app),
		newCollectionSourcesCmd(app, &flagJSON),
		newCollectionRemoveSourceCmd(app),
		newCollectionSetCmd(app),
		newCollectionRenameCmd(app),
		newCollectionDeleteCmd(app),
	)

	collectionCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "output machine-readable JSON")

	return collectionCmd
}

func newCollectionListCmd(app *App, jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all collections",
		Example: strings.TrimSpace(`
  bmgrep collection list
  bmgrep collection list --json
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			collections, err := app.Store.ListCollections()
			if err != nil {
				return err
			}

			if *jsonFlag {
				payload := collectionListJSON{
					Collections: make([]collectionSummaryJSON, 0, len(collections)),
				}
				for _, collection := range collections {
					payload.Collections = append(payload.Collections, collectionSummaryJSON{
						Name:          collection.Name,
						RootPath:      collection.RootPath,
						DocumentCount: collection.Documents,
						IsDefault:     collection.IsDefault,
					})
				}
				return writeJSON(cmd.OutOrStdout(), payload)
			}

			if len(collections) == 0 {
				fmt.Println("No collections found.")
				return nil
			}

			for _, c := range collections {
				marker := " "
				if c.IsDefault {
					marker = "*"
				}
				fmt.Printf("%s %s (%d docs)\n", marker, c.Name, c.Documents)
				fmt.Printf("  path: %s\n", c.RootPath)
			}
			return nil
		},
	}
}

func newCollectionCreateCmd(app *App) *cobra.Command {
	var flagPath string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create and index a new collection",
		Long: `Create a collection with an initial directory source at --path,
ensure .bmignore exists, and index all non-ignored .md files.`,
		Example: strings.TrimSpace(`
  bmgrep collection create docs --path /home/user/reference/docs
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("collection name cannot be empty")
			}
			if strings.TrimSpace(flagPath) == "" {
				return fmt.Errorf("--path is required")
			}

			root, info, err := resolveSourcePath(flagPath)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return fmt.Errorf("--path must be a directory")
			}

			ignorePath, err := ingest.EnsureIgnoreFile(root)
			if err != nil {
				return err
			}

			collection, err := app.Store.CreateCollection(name, root, ignorePath)
			if err != nil {
				return err
			}

			stats, err := ingest.ReconcileCollection(app.Store, collection)
			if err != nil {
				return err
			}

			fmt.Printf("Created collection %q\n", collection.Name)
			fmt.Printf("root: %s\n", collection.RootPath)
			fmt.Printf("ignore: %s\n", ingest.DirectoryIgnoreFilePath(collection.RootPath))
			fmt.Printf("indexed: +%d ~%d -%d\n", stats.Added, stats.Updated, stats.Deleted)

			if _, err := app.Store.GetDefaultCollection(); err != nil {
				if !errors.Is(err, store.ErrNoDefaultCollection) {
					return err
				}
				if err := app.Store.SetDefaultCollectionByName(collection.Name); err != nil {
					return err
				}
				fmt.Printf("default collection set to %q\n", collection.Name)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&flagPath, "path", "", "initial directory source for markdown files (required)")
	return cmd
}

func newCollectionSetCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "set <name>",
		Short: "Set default collection for bmgrep queries",
		Example: strings.TrimSpace(`
  bmgrep collection set docs
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := app.Store.SetDefaultCollectionByName(name); err != nil {
				return err
			}

			fmt.Printf("Default collection set to %q\n", name)
			return nil
		},
	}
}

func newCollectionAddSourceCmd(app *App) *cobra.Command {
	var (
		flagCollection string
		flagDir        string
		flagFile       string
	)

	cmd := &cobra.Command{
		Use:   "add [collection]",
		Short: "Add a file or directory source to a collection",
		Long: `Add a source to a collection. When collection is omitted, bmgrep uses
BMGREP_COLLECTION, then the persistent default collection.
Exactly one of --dir or --file is required.`,
		Example: strings.TrimSpace(`
  bmgrep collection add --dir ~/docs/reference
  bmgrep collection add --file ~/notes/agent-patterns.md
  bmgrep collection add docs-v2 --dir ~/tmp/handpicked-md
  bmgrep collection add --collection docs-v2 --file ~/work/spec.md
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit := strings.TrimSpace(flagCollection)
			if len(args) == 1 {
				if explicit != "" {
					return fmt.Errorf("collection can be provided either as argument or --collection, not both")
				}
				explicit = strings.TrimSpace(args[0])
			}

			collection, err := resolveCollectionTarget(app, explicit)
			if err != nil {
				return err
			}

			hasDir := strings.TrimSpace(flagDir) != ""
			hasFile := strings.TrimSpace(flagFile) != ""
			if hasDir == hasFile {
				return fmt.Errorf("exactly one of --dir or --file must be provided")
			}

			var source store.CollectionSource
			if hasDir {
				dirPath, info, err := resolveSourcePath(flagDir)
				if err != nil {
					return err
				}
				if !info.IsDir() {
					return fmt.Errorf("--dir must point to a directory")
				}

				ignorePath, err := ingest.EnsureIgnoreFile(dirPath)
				if err != nil {
					return err
				}

				source, err = app.Store.AddCollectionSource(collection.ID, store.SourceTypeDirectory, dirPath, ignorePath)
				if err != nil {
					return err
				}
			} else {
				filePath, info, err := resolveSourcePath(flagFile)
				if err != nil {
					return err
				}
				if info.IsDir() {
					return fmt.Errorf("--file must point to a markdown file, not a directory")
				}
				if strings.ToLower(filepath.Ext(filePath)) != ".md" {
					return fmt.Errorf("--file must have .md extension")
				}

				source, err = app.Store.AddCollectionSource(collection.ID, store.SourceTypeFile, filePath, "")
				if err != nil {
					return err
				}
			}

			stats, err := ingest.ReconcileCollection(app.Store, collection)
			if err != nil {
				return err
			}

			fmt.Printf("Added %s source to collection %q\n", source.SourceType, collection.Name)
			fmt.Printf("source[%d]: %s\n", source.ID, source.SourcePath)
			if source.SourceType == store.SourceTypeDirectory {
				fmt.Printf("ignore: %s\n", ingest.DirectoryIgnoreFilePath(source.SourcePath))
			} else if strings.TrimSpace(source.IgnoreFilePath) != "" {
				fmt.Printf("ignore: %s\n", source.IgnoreFilePath)
			}
			fmt.Printf("reindexed: +%d ~%d -%d\n", stats.Added, stats.Updated, stats.Deleted)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagCollection, "collection", "", "target collection name (defaults to BMGREP_COLLECTION or persistent default collection)")
	cmd.Flags().StringVar(&flagDir, "dir", "", "directory source to add (.md files scanned recursively)")
	cmd.Flags().StringVar(&flagFile, "file", "", "single markdown file source to add")
	return cmd
}

func newCollectionSourcesCmd(app *App, jsonFlag *bool) *cobra.Command {
	var flagCollection string

	cmd := &cobra.Command{
		Use:   "sources [collection]",
		Short: "List sources configured for a collection",
		Example: strings.TrimSpace(`
  bmgrep collection sources
  bmgrep collection sources docs-v2
  bmgrep collection sources --json
  bmgrep collection sources docs-v2 --json
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit := strings.TrimSpace(flagCollection)
			if len(args) == 1 {
				if explicit != "" {
					return fmt.Errorf("collection can be provided either as argument or --collection, not both")
				}
				explicit = strings.TrimSpace(args[0])
			}

			collection, err := resolveCollectionTarget(app, explicit)
			if err != nil {
				return err
			}

			sources, err := app.Store.ListCollectionSources(collection.ID)
			if err != nil {
				return err
			}

			if *jsonFlag {
				payload := collectionSourcesJSON{
					Collection: collection.Name,
					Sources:    make([]collectionSourceJSON, 0, len(sources)),
				}
				for _, source := range sources {
					payload.Sources = append(payload.Sources, collectionSourceJSON{
						ID:         source.ID,
						Type:       source.SourceType,
						Path:       source.SourcePath,
						IgnoreFile: source.IgnoreFilePath,
						Enabled:    source.Enabled,
					})
				}
				return writeJSON(cmd.OutOrStdout(), payload)
			}

			if len(sources) == 0 {
				fmt.Printf("Collection %q has no configured sources.\n", collection.Name)
				return nil
			}

			fmt.Printf("Collection %q sources:\n", collection.Name)
			for _, source := range sources {
				state := "enabled"
				if !source.Enabled {
					state = "disabled"
				}
				fmt.Printf("  [%d] %s (%s)\n", source.ID, source.SourcePath, source.SourceType)
				if source.SourceType == store.SourceTypeDirectory {
					fmt.Printf("       ignore: %s\n", ingest.DirectoryIgnoreFilePath(source.SourcePath))
				} else if source.IgnoreFilePath != "" {
					fmt.Printf("       ignore: %s\n", source.IgnoreFilePath)
				}
				fmt.Printf("       state: %s\n", state)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&flagCollection, "collection", "", "target collection name (defaults to BMGREP_COLLECTION or persistent default collection)")
	return cmd
}

func newCollectionRemoveSourceCmd(app *App) *cobra.Command {
	var flagCollection string

	cmd := &cobra.Command{
		Use:   "remove-source <id-or-path> [collection]",
		Short: "Remove a configured source from a collection",
		Example: strings.TrimSpace(`
  bmgrep collection remove-source 3
  bmgrep collection remove-source ~/notes/agent-patterns.md
  bmgrep collection remove-source 3 docs-v2
`),
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit := strings.TrimSpace(flagCollection)
			if len(args) == 2 {
				if explicit != "" {
					return fmt.Errorf("collection can be provided either as argument or --collection, not both")
				}
				explicit = strings.TrimSpace(args[1])
			}

			collection, err := resolveCollectionTarget(app, explicit)
			if err != nil {
				return err
			}

			target := strings.TrimSpace(args[0])
			if target == "" {
				return fmt.Errorf("source id/path cannot be empty")
			}

			var removed store.CollectionSource
			if id, err := strconv.ParseInt(target, 10, 64); err == nil {
				removed, err = app.Store.RemoveCollectionSourceByID(collection.ID, id)
				if err != nil {
					return err
				}
			} else {
				path, _, err := resolveSourcePathBestEffort(target)
				if err != nil {
					return err
				}
				removed, err = app.Store.RemoveCollectionSourceByPath(collection.ID, path)
				if err != nil {
					return err
				}
			}

			stats, err := ingest.ReconcileCollection(app.Store, collection)
			if err != nil {
				return err
			}

			fmt.Printf("Removed source[%d] from collection %q\n", removed.ID, collection.Name)
			fmt.Printf("source: %s (%s)\n", removed.SourcePath, removed.SourceType)
			fmt.Printf("reindexed: +%d ~%d -%d\n", stats.Added, stats.Updated, stats.Deleted)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagCollection, "collection", "", "target collection name (defaults to BMGREP_COLLECTION or persistent default collection)")
	return cmd
}

func newCollectionRenameCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename an existing collection",
		Example: strings.TrimSpace(`
  bmgrep collection rename docs docs-v2
`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldName := strings.TrimSpace(args[0])
			newName := strings.TrimSpace(args[1])
			if oldName == "" || newName == "" {
				return fmt.Errorf("collection names cannot be empty")
			}

			if err := app.Store.RenameCollection(oldName, newName); err != nil {
				return err
			}

			fmt.Printf("Renamed collection %q -> %q\n", oldName, newName)
			return nil
		},
	}
}

func newCollectionDeleteCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a collection and all indexed documents in it",
		Example: strings.TrimSpace(`
  bmgrep collection delete docs
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("collection name cannot be empty")
			}

			if err := app.Store.DeleteCollection(name); err != nil {
				return err
			}

			fmt.Printf("Deleted collection %q\n", name)
			return nil
		},
	}
}

func resolveCollectionTarget(app *App, explicit string) (store.Collection, error) {
	return app.resolveCollection(explicit)
}

func resolveSourcePath(input string) (string, os.FileInfo, error) {
	expanded, err := paths.ExpandPath(strings.TrimSpace(input))
	if err != nil {
		return "", nil, fmt.Errorf("resolve source path: %w", err)
	}
	info, err := os.Stat(expanded)
	if err != nil {
		return "", nil, fmt.Errorf("stat source path: %w", err)
	}
	resolved, err := ingest.ResolveSourcePath(expanded)
	if err != nil {
		return "", nil, err
	}
	return resolved, info, nil
}

func resolveSourcePathBestEffort(input string) (string, os.FileInfo, error) {
	expanded, err := paths.ExpandPath(strings.TrimSpace(input))
	if err != nil {
		return "", nil, fmt.Errorf("resolve source path: %w", err)
	}
	resolved, err := ingest.ResolveSourcePath(expanded)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return resolved, nil, nil
		}
		return "", nil, fmt.Errorf("stat source path: %w", err)
	}
	return resolved, info, nil
}
