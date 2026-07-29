package cmd

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/spf13/cobra"
)

const migrationsDir = "migrations"

var migrateCmd = &cobra.Command{
	Use:   "migrate [up|down|status]",
	Short: "Run database migrations (goose)",
	Long: "Apply, roll back, or inspect database migrations. Migrations are " +
		"embedded from the migrations/ directory. Requires DATABASE_URL.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			return fmt.Errorf("DATABASE_URL is not set")
		}

		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = db.Close() }()

		goose.SetBaseFS(migrationsFS)
		if err := goose.SetDialect("postgres"); err != nil {
			return fmt.Errorf("set dialect: %w", err)
		}

		switch args[0] {
		case "up":
			return goose.Up(db, migrationsDir)
		case "down":
			return goose.Down(db, migrationsDir)
		case "status":
			return goose.Status(db, migrationsDir)
		default:
			return fmt.Errorf("unknown subcommand %q (use up, down, or status)", args[0])
		}
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}
