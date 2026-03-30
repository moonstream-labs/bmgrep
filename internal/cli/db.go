package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moonstream-labs/bmgrep/internal/config"
	"github.com/moonstream-labs/bmgrep/internal/dbprofile"
	"github.com/moonstream-labs/bmgrep/internal/paths"
	"github.com/moonstream-labs/bmgrep/internal/store"
)

func newDBCmd(app *App, flagConfig, flagDB *string) *cobra.Command {
	dbCmd := &cobra.Command{
		Use:   "db",
		Short: "Manage workspace and global bmgrep databases",
		Long: `Database commands control workspace-local and global database profiles.
By default, database/profile operations target the nearest workspace (.bmgrep).
Use --global to manage profiles in ~/.config/bmgrep/databases.yaml.`,
	}

	dbCmd.AddCommand(
		newDBInitCmd(),
		newDBCurrentCmd(flagConfig, flagDB),
		newDBListCmd(),
		newDBRegisterCmd(),
		newDBUseCmd(),
		newDBUnregisterCmd(),
		newDBDoctorCmd(flagConfig, flagDB),
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

			workspaceDir := dbprofile.WorkspacePath(root)
			if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
				return fmt.Errorf("create workspace directory: %w", err)
			}

			dbPath := dbprofile.WorkspaceDBPath(root)
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			if err := st.Close(); err != nil {
				return err
			}

			configPath := dbprofile.WorkspaceConfigPath(root)
			if _, err := os.Stat(configPath); err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("stat workspace config: %w", err)
				}
				if err := config.Save(configPath, &config.Config{}); err != nil {
					return err
				}
			}

			regPath := dbprofile.LocalRegistryPath(root)
			reg, err := dbprofile.LoadRegistry(regPath)
			if err != nil {
				return err
			}

			name := filepath.Base(root)
			entry, err := dbprofile.RegisterDatabase(reg, dbprofile.DatabaseEntry{
				Name:          name,
				DBPath:        dbPath,
				ConfigPath:    configPath,
				WorkspaceRoot: root,
				Scope:         dbprofile.ScopeLocal,
			})
			if err != nil {
				return err
			}
			if _, err := dbprofile.UseDatabase(reg, entry.Name); err != nil {
				return err
			}
			if err := dbprofile.SaveRegistry(regPath, reg); err != nil {
				return err
			}

			fmt.Printf("Initialized workspace at %s\n", root)
			fmt.Printf("db: %s\n", dbPath)
			fmt.Printf("config: %s\n", configPath)
			fmt.Printf("registry: %s\n", regPath)
			fmt.Printf("active profile: %s\n", entry.Name)
			return nil
		},
	}
}

func newDBCurrentCmd(flagConfig, flagDB *string) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the active runtime db and config resolution",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := dbprofile.ResolvePaths(strings.TrimSpace(*flagConfig), strings.TrimSpace(*flagDB))
			if err != nil {
				return err
			}

			fmt.Printf("db: %s\n", resolved.DBPath)
			fmt.Printf("db source: %s\n", resolved.DBSource)
			fmt.Printf("config: %s\n", resolved.ConfigPath)
			fmt.Printf("config source: %s\n", resolved.ConfigSource)
			if resolved.Workspace != "" {
				fmt.Printf("workspace: %s\n", resolved.Workspace)
			} else {
				fmt.Println("workspace: <none>")
			}
			return nil
		},
	}
}

func newDBListCmd() *cobra.Command {
	var flagGlobal bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered databases",
		Example: strings.TrimSpace(`
  bmgrep db list
  bmgrep db list --global
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			regPath, scopeLabel, workspace, err := registryTarget(flagGlobal)
			if err != nil {
				return err
			}

			reg, err := dbprofile.LoadRegistry(regPath)
			if err != nil {
				return err
			}
			if len(reg.Databases) == 0 {
				fmt.Printf("No %s database profiles found.\n", scopeLabel)
				if !flagGlobal {
					fmt.Println("Tip: run `bmgrep db init` to create a workspace profile.")
				}
				return nil
			}

			fmt.Printf("%s database profiles:\n", titleCase(scopeLabel))
			if workspace != "" {
				fmt.Printf("workspace: %s\n", workspace)
			}
			for _, entry := range reg.Databases {
				marker := " "
				if (entry.Name != "" && reg.Active == entry.Name) || reg.Active == entry.DBPath {
					marker = "*"
				}
				name := entry.Name
				if name == "" {
					name = "<unnamed>"
				}
				fmt.Printf("%s %s\n", marker, name)
				fmt.Printf("  db: %s\n", entry.DBPath)
				if entry.ConfigPath != "" {
					fmt.Printf("  config: %s\n", entry.ConfigPath)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&flagGlobal, "global", "g", false, "list global profiles instead of workspace-local profiles")
	return cmd
}

func newDBRegisterCmd() *cobra.Command {
	var flagName string
	var flagConfig string
	var flagGlobal bool

	cmd := &cobra.Command{
		Use:   "register <db-path>",
		Short: "Register a database profile",
		Example: strings.TrimSpace(`
  bmgrep db register ./.bmgrep/bmgrep.db --name project-a
  bmgrep db register ~/.local/share/bmgrep/other.db --global --name shared
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := paths.ExpandPath(args[0])
			if err != nil {
				return err
			}

			var cfgPath string
			if strings.TrimSpace(flagConfig) != "" {
				cfgPath, err = paths.ExpandPath(flagConfig)
				if err != nil {
					return err
				}
			}

			regPath, scopeLabel, workspace, err := registryTarget(flagGlobal)
			if err != nil {
				return err
			}
			reg, err := dbprofile.LoadRegistry(regPath)
			if err != nil {
				return err
			}

			scope := dbprofile.ScopeLocal
			if flagGlobal {
				scope = dbprofile.ScopeGlobal
			}
			entry, err := dbprofile.RegisterDatabase(reg, dbprofile.DatabaseEntry{
				Name:          strings.TrimSpace(flagName),
				DBPath:        dbPath,
				ConfigPath:    cfgPath,
				WorkspaceRoot: workspace,
				Scope:         scope,
			})
			if err != nil {
				return err
			}

			if err := dbprofile.SaveRegistry(regPath, reg); err != nil {
				return err
			}

			name := entry.Name
			if name == "" {
				name = "<unnamed>"
			}
			fmt.Printf("Registered %s profile %q\n", scopeLabel, name)
			fmt.Printf("db: %s\n", entry.DBPath)
			if entry.ConfigPath != "" {
				fmt.Printf("config: %s\n", entry.ConfigPath)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&flagName, "name", "", "profile name (optional)")
	cmd.Flags().StringVar(&flagConfig, "config", "", "config path to associate with profile")
	cmd.Flags().BoolVarP(&flagGlobal, "global", "g", false, "register in global profile registry")
	return cmd
}

func newDBUseCmd() *cobra.Command {
	var flagGlobal bool

	cmd := &cobra.Command{
		Use:   "use <name-or-path>",
		Short: "Set the active database profile",
		Example: strings.TrimSpace(`
  bmgrep db use project-a
  bmgrep db use ~/.local/share/bmgrep/other.db --global
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := strings.TrimSpace(args[0])
			regPath, scopeLabel, _, err := registryTarget(flagGlobal)
			if err != nil {
				return err
			}
			reg, err := dbprofile.LoadRegistry(regPath)
			if err != nil {
				return err
			}

			entry, err := dbprofile.UseDatabase(reg, target)
			if err != nil {
				return err
			}
			if err := dbprofile.SaveRegistry(regPath, reg); err != nil {
				return err
			}

			name := entry.Name
			if name == "" {
				name = entry.DBPath
			}
			fmt.Printf("Active %s profile set to %q\n", scopeLabel, name)
			fmt.Printf("db: %s\n", entry.DBPath)
			if entry.ConfigPath != "" {
				fmt.Printf("config: %s\n", entry.ConfigPath)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&flagGlobal, "global", "g", false, "select active profile in global registry")
	return cmd
}

func newDBUnregisterCmd() *cobra.Command {
	var flagGlobal bool

	cmd := &cobra.Command{
		Use:   "unregister <name-or-path>",
		Short: "Remove a registered database profile",
		Example: strings.TrimSpace(`
  bmgrep db unregister project-a
  bmgrep db unregister ~/.local/share/bmgrep/other.db --global
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := strings.TrimSpace(args[0])
			regPath, scopeLabel, _, err := registryTarget(flagGlobal)
			if err != nil {
				return err
			}
			reg, err := dbprofile.LoadRegistry(regPath)
			if err != nil {
				return err
			}

			entry, ok, err := dbprofile.UnregisterDatabase(reg, target)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("database profile %q not found", target)
			}

			if err := dbprofile.SaveRegistry(regPath, reg); err != nil {
				return err
			}

			name := entry.Name
			if name == "" {
				name = entry.DBPath
			}
			fmt.Printf("Unregistered %s profile %q\n", scopeLabel, name)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&flagGlobal, "global", "g", false, "unregister from global profile registry")
	return cmd
}

func newDBDoctorCmd(flagConfig, flagDB *string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Validate active db/config resolution and database health",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := dbprofile.ResolvePaths(strings.TrimSpace(*flagConfig), strings.TrimSpace(*flagDB))
			if err != nil {
				return err
			}

			fmt.Printf("db: %s (%s)\n", resolved.DBPath, resolved.DBSource)
			fmt.Printf("config: %s (%s)\n", resolved.ConfigPath, resolved.ConfigSource)
			if resolved.Workspace != "" {
				fmt.Printf("workspace: %s\n", resolved.Workspace)
			}

			if _, err := config.Load(resolved.ConfigPath); err != nil {
				return fmt.Errorf("config load failed: %w", err)
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

func registryTarget(global bool) (path, scopeLabel, workspace string, err error) {
	if global {
		p, err := dbprofile.GlobalRegistryPath()
		if err != nil {
			return "", "", "", err
		}
		return p, "global", "", nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", "", "", fmt.Errorf("get working directory: %w", err)
	}
	root, found, err := dbprofile.FindWorkspaceRoot(cwd)
	if err != nil {
		return "", "", "", err
	}
	if !found {
		return "", "", "", fmt.Errorf("no workspace found from %s; run `bmgrep db init` or use --global", cwd)
	}
	return dbprofile.LocalRegistryPath(root), "workspace", root, nil
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
