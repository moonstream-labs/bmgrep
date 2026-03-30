package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moonstream-labs/bmgrep/internal/paths"
	"github.com/moonstream-labs/bmgrep/internal/store"
)

func newDBCmd(app *App, flagDB *string) *cobra.Command {
	dbCmd := &cobra.Command{
		Use:   "db",
		Short: "Manage bmgrep database location and health",
		Long: `Database commands support workspace-local usage without profile indirection.

Resolution order:
  --db flag -> BMGREP_DB -> nearest .bmgrep/bmgrep.db -> global default.

Workspace-local state lives in <workspace>/.bmgrep/bmgrep.db.`,
	}

	dbCmd.AddCommand(
		newDBInitCmd(),
		newDBCurrentCmd(flagDB),
		newDBDoctorCmd(flagDB),
	)

	return dbCmd
}

func newDBInitCmd() *cobra.Command {
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

func newDBCurrentCmd(flagDB *string) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show active database path and resolution source",
		Example: strings.TrimSpace(`
  bmgrep db current
  bmgrep db current --db /tmp/bmgrep.db
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveDBRuntimePath(strings.TrimSpace(*flagDB))
			if err != nil {
				return err
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

func newDBDoctorCmd(flagDB *string) *cobra.Command {
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
