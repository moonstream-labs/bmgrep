package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moonstream-labs/bmgrep/internal/config"
	"github.com/moonstream-labs/bmgrep/internal/ingest"
	"github.com/moonstream-labs/bmgrep/internal/paths"
)

func newCollectionCmd(app *App) *cobra.Command {
	collectionCmd := &cobra.Command{
		Use:   "collection",
		Short: "Manage indexed markdown collections",
		Long: `Collection commands control which documentation roots are indexed
and which collection is searched by default.`,
	}

	collectionCmd.AddCommand(
		newCollectionListCmd(app),
		newCollectionCreateCmd(app),
		newCollectionSetCmd(app),
		newCollectionRenameCmd(app),
		newCollectionDeleteCmd(app),
	)

	return collectionCmd
}

func newCollectionListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all collections",
		Example: strings.TrimSpace(`
  bmgrep collection list
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			collections, err := app.Store.ListCollections()
			if err != nil {
				return err
			}
			if len(collections) == 0 {
				fmt.Println("No collections found.")
				return nil
			}

			for _, c := range collections {
				marker := " "
				if app.Config.DefaultCollection == c.Name {
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
		Long: `Create a collection rooted at --path, ensure .bmgrepignore exists,
and index all non-ignored .md files.`,
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

			root, err := paths.ExpandPath(flagPath)
			if err != nil {
				return fmt.Errorf("resolve --path: %w", err)
			}
			if info, err := os.Stat(root); err != nil {
				return fmt.Errorf("stat --path: %w", err)
			} else if !info.IsDir() {
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
			fmt.Printf("ignore: %s\n", collection.IgnoreFilePath)
			fmt.Printf("indexed: +%d ~%d -%d\n", stats.Added, stats.Updated, stats.Deleted)

			if strings.TrimSpace(app.Config.DefaultCollection) == "" {
				app.Config.DefaultCollection = collection.Name
				if err := config.Save(app.ConfigPath, app.Config); err != nil {
					return err
				}
				fmt.Printf("default collection set to %q\n", collection.Name)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&flagPath, "path", "", "root directory for markdown files (required)")
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
			if _, err := app.Store.GetCollectionByName(name); err != nil {
				return err
			}

			app.Config.DefaultCollection = name
			if err := config.Save(app.ConfigPath, app.Config); err != nil {
				return err
			}

			fmt.Printf("Default collection set to %q\n", name)
			return nil
		},
	}
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

			if app.Config.DefaultCollection == oldName {
				app.Config.DefaultCollection = newName
				if err := config.Save(app.ConfigPath, app.Config); err != nil {
					return err
				}
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

			if app.Config.DefaultCollection == name {
				app.Config.DefaultCollection = ""
				if err := config.Save(app.ConfigPath, app.Config); err != nil {
					return err
				}
			}

			fmt.Printf("Deleted collection %q\n", name)
			return nil
		},
	}
}
