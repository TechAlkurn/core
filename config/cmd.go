package config

import (
	"os"

	"github.com/spf13/cobra"
)

func InitCmd() {
	rootCmd := &cobra.Command{Use: "app", SilenceUsage: true}

	// `migrate` — schema-only dump, written to ../db/schema.sql for
	// version-controlled schema snapshots. Preserves the original CLI
	// contract (used by developer tooling).
	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Dump the database schema (no data) to ../db/schema.sql",
		Run: func(cmd *cobra.Command, args []string) {
			path, err := DumpDatabase(BackupOptions{
				Dir:        "../db",
				SchemaOnly: true,
				FileName:   "schema.sql",
			})
			if err != nil {
				Log.Println("migrate:", err)
				os.Exit(1)
			}
			Log.Println("schema written to", path)
		},
	}

	// `backup` — full data + schema dump, timestamped, written under
	// DIR_DB/backups (or ./backups if DIR_DB isn't set).
	// This is the command that produces a restore-ready snapshot.
	backupCmd := &cobra.Command{
		Use:   "backup",
		Short: "Full mysqldump (schema + data) to DIR_DB/backups",
		Run: func(cmd *cobra.Command, args []string) {
			path, err := DumpDatabase(BackupOptions{})
			if err != nil {
				Log.Println("backup:", err)
				os.Exit(1)
			}
			Log.Println("backup written to", path)
		},
	}

	// `restore <file.sql>` — imports a dump into the configured database.
	// Always takes a pre-restore snapshot first (fails closed if that
	// snapshot errors, so nothing overwrites without a rollback path).
	restoreCmd := &cobra.Command{
		Use:   "restore <file.sql>",
		Short: "Restore the database from a mysqldump .sql file",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			path, err := RestoreDatabase(RestoreOptions{
				File:         args[0],
				BackupBefore: true,
			})
			if err != nil {
				Log.Println("restore:", err)
				os.Exit(1)
			}
			Log.Println("restored from", path)
		},
	}

	generateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate models",
		Run: func(cmd *cobra.Command, args []string) {
			Log.Println("Executing command 2")
		},
	}

	rootCmd.AddCommand(migrateCmd, backupCmd, restoreCmd, generateCmd)
	if err := rootCmd.Execute(); err != nil {
		Log.Println(err)
		os.Exit(1)
	}
}
