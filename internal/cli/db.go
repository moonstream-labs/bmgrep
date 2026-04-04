package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moonstream-labs/bmgrep/internal/paths"
	"github.com/moonstream-labs/bmgrep/internal/store"
)

func newDBCmd(app *App, flagDB *string) *cobra.Command {
	var flagJSON bool

	dbCmd := &cobra.Command{
		Use:   "db",
		Short: "Manage bmgrep database location and health",
		Long: `Database commands support workspace-local usage without profile indirection.

Resolution order:
  --db flag -> BMGREP_DB -> nearest .bmgrep/bmgrep.db -> global default.

Workspace-local state lives in <workspace>/.bmgrep/bmgrep.db.`,
	}

	dbCmd.AddCommand(
		newDBInitCmd(app),
		newDBCurrentCmd(app, flagDB, &flagJSON),
		newDBSourcesCmd(app, flagDB, &flagJSON),
		newDBDoctorCmd(app, flagDB),
	)

	dbCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "output machine-readable JSON")

	return dbCmd
}

func newDBInitCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize a workspace-local bmgrep database",
		Example: strings.TrimSpace(`
  bmgrep db init
  bmgrep db init ~/work/my-project
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			root, err := paths.ExpandPath(target)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(root, 0o755); err != nil {
				return fmt.Errorf("create workspace root: %w", err)
			}

			workspaceDir := workspaceDirPath(root)
			if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
				return fmt.Errorf("create workspace directory: %w", err)
			}

			dbPath := workspaceDBPath(root)
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			if err := st.Close(); err != nil {
				return err
			}

			fmt.Printf("Initialized workspace at %s\n", root)
			fmt.Printf("db: %s\n", dbPath)
			return nil
		},
	}
}

func newDBCurrentCmd(app *App, flagDB *string, jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show active database path and resolution source",
		Example: strings.TrimSpace(`
  bmgrep db current
  bmgrep db current --db /tmp/bmgrep.db
  bmgrep db current --json
  bmgrep db current --db /tmp/bmgrep.db --json
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveDBRuntimePath(strings.TrimSpace(*flagDB))
			if err != nil {
				return err
			}

			if *jsonFlag {
				cwd, _ := os.Getwd()
				return writeJSON(cmd.OutOrStdout(), dbCurrentJSON{
					CWD:       cwd,
					DBPath:    resolved.DBPath,
					DBSource:  resolved.DBSource,
					Workspace: resolved.Workspace,
				})
			}

			fmt.Printf("db: %s\n", resolved.DBPath)
			fmt.Printf("db source: %s\n", resolved.DBSource)
			if resolved.Workspace != "" {
				fmt.Printf("workspace: %s\n", resolved.Workspace)
			} else {
				fmt.Println("workspace: <none>")
			}
			return nil
		},
	}
}

func newDBSourcesCmd(app *App, flagDB *string, jsonFlag *bool) *cobra.Command {
	var flagCollection string
	var flagType string
	var flagEnabled bool
	var flagDisabled bool
	var flagPathPrefix string
	var flagSort string
	var flagDesc bool
	var flagWithStats bool

	cmd := &cobra.Command{
		Use:   "sources",
		Short: "List configured sources in the active database",
		Example: strings.TrimSpace(`
  bmgrep db sources
  bmgrep db sources --with-stats
  bmgrep db sources --collection docs --type dir --sort updated --desc
  bmgrep db sources --path-prefix ./reference/docs --with-stats --json
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagEnabled && flagDisabled {
				return fmt.Errorf("--enabled and --disabled are mutually exclusive")
			}

			resolved, err := resolveDBRuntimePath(strings.TrimSpace(*flagDB))
			if err != nil {
				return err
			}

			st, err := store.Open(resolved.DBPath)
			if err != nil {
				return fmt.Errorf("database open failed: %w", err)
			}
			defer st.Close()

			pathPrefix := strings.TrimSpace(flagPathPrefix)
			if pathPrefix != "" {
				expanded, err := paths.ExpandPath(pathPrefix)
				if err != nil {
					return err
				}
				pathPrefix = filepath.Clean(expanded)
			}

			query := store.DBSourceQuery{
				CollectionName: strings.TrimSpace(flagCollection),
				SourceType:     strings.TrimSpace(flagType),
				EnabledOnly:    flagEnabled,
				DisabledOnly:   flagDisabled,
				PathPrefix:     pathPrefix,
				SortBy:         strings.TrimSpace(flagSort),
				Desc:           flagDesc,
				IncludeStats:   flagWithStats,
			}

			sources, err := st.ListDBSources(query)
			if err != nil {
				return err
			}

			if *jsonFlag {
				cwd, _ := os.Getwd()
				payload := dbSourcesJSON{
					CWD:       cwd,
					DBPath:    resolved.DBPath,
					DBSource:  resolved.DBSource,
					Workspace: resolved.Workspace,
					Filters: dbSourcesFilterJSON{
						Collection:   query.CollectionName,
						Type:         query.SourceType,
						EnabledOnly:  query.EnabledOnly,
						DisabledOnly: query.DisabledOnly,
						PathPrefix:   query.PathPrefix,
						Sort:         normalizedDBSourceSort(query.SortBy),
						Desc:         query.Desc,
						WithStats:    query.IncludeStats,
					},
					Sources: make([]dbSourceEntryJSON, 0, len(sources)),
				}

				for _, source := range sources {
					entry := dbSourceEntryJSON{
						SourceID:     source.SourceID,
						CollectionID: source.CollectionID,
						Collection:   source.CollectionName,
						Type:         source.SourceType,
						Path:         source.SourcePath,
						IgnoreFile:   source.IgnoreFilePath,
						Enabled:      source.Enabled,
						AddedAt:      source.CreatedAt,
						UpdatedAt:    source.UpdatedAt,
					}
					if query.IncludeStats {
						indexedDocs := source.IndexedDocs
						entry.IndexedDocs = &indexedDocs
						entry.LatestIndexedAt = source.LatestIndexedAt
					}
					payload.Sources = append(payload.Sources, entry)
				}

				return writeJSON(cmd.OutOrStdout(), payload)
			}

			fmt.Printf("db: %s (%s)\n", resolved.DBPath, resolved.DBSource)
			if resolved.Workspace != "" {
				fmt.Printf("workspace: %s\n", resolved.Workspace)
			}
			fmt.Printf("sources: %d\n", len(sources))
			if len(sources) == 0 {
				return nil
			}
			fmt.Println()

			for i, source := range sources {
				state := "enabled"
				if !source.Enabled {
					state = "disabled"
				}

				fmt.Printf("[%d] %s source[%d] (%s, %s)\n", i+1, source.CollectionName, source.SourceID, source.SourceType, state)
				fmt.Printf("    path: %s\n", source.SourcePath)
				if strings.TrimSpace(source.IgnoreFilePath) != "" {
					fmt.Printf("    ignore: %s\n", source.IgnoreFilePath)
				}
				fmt.Printf("    added: %s\n", source.CreatedAt)
				fmt.Printf("    updated: %s\n", source.UpdatedAt)
				if query.IncludeStats {
					fmt.Printf("    indexed docs: %d\n", source.IndexedDocs)
					if source.LatestIndexedAt != "" {
						fmt.Printf("    latest indexed: %s\n", source.LatestIndexedAt)
					} else {
						fmt.Println("    latest indexed: <none>")
					}
				}

				if i < len(sources)-1 {
					fmt.Println()
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&flagCollection, "collection", "", "filter by collection name")
	cmd.Flags().StringVar(&flagType, "type", "", "filter by source type (dir|file)")
	cmd.Flags().BoolVar(&flagEnabled, "enabled", false, "show only enabled sources")
	cmd.Flags().BoolVar(&flagDisabled, "disabled", false, "show only disabled sources")
	cmd.Flags().StringVar(&flagPathPrefix, "path-prefix", "", "filter sources under this absolute/relative path prefix")
	cmd.Flags().StringVar(&flagSort, "sort", "added", "sort by added, updated, collection, or path")
	cmd.Flags().BoolVar(&flagDesc, "desc", true, "sort descending")
	cmd.Flags().BoolVar(&flagWithStats, "with-stats", false, "include per-source indexed document stats")

	return cmd
}

func normalizedDBSourceSort(sortBy string) string {
	s := strings.ToLower(strings.TrimSpace(sortBy))
	if s == "" {
		return "added"
	}
	return s
}

func newDBDoctorCmd(app *App, flagDB *string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Validate active database resolution and basic health",
		Example: strings.TrimSpace(`
  bmgrep db doctor
  bmgrep db doctor --db /tmp/bmgrep.db
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveDBRuntimePath(strings.TrimSpace(*flagDB))
			if err != nil {
				return err
			}

			fmt.Printf("db: %s (%s)\n", resolved.DBPath, resolved.DBSource)
			if resolved.Workspace != "" {
				fmt.Printf("workspace: %s\n", resolved.Workspace)
			}

			st, err := store.Open(resolved.DBPath)
			if err != nil {
				return fmt.Errorf("database open failed: %w", err)
			}
			defer st.Close()

			collections, err := st.ListCollections()
			if err != nil {
				return fmt.Errorf("database query failed: %w", err)
			}

			fmt.Printf("database check: ok (%d collections)\n", len(collections))
			return nil
		},
	}
}
