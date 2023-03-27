package infrastructure

import (
	"context"
	"database/sql"
	"time"

	"github.com/pressly/goose/v3"
	_ "github.com/sijms/go-ora"
)

// go_ora "github.com/sijms/go-ora"

// ConnectOracleDB opens a connection to the OracleDB and ensures that the db is reachable
func ConnectOracleDB(ctx context.Context, dsn string) (*sql.DB, error) {
	db, _ := sql.Open("oracle", dsn)
	return db, pingOracle(ctx, db)
}

func pingOracle(ctx context.Context, db *sql.DB) (err error) {
	// wait until db is ready
	for start := time.Now(); time.Since(start) < (5 * time.Second); {
		err = db.PingContext(ctx)
		if err == nil {
			break
		}

		time.Sleep(1 * time.Second)
	}
	return err
}

// MigrateOracleDB runs all migrations found in the given directory against the db
func MigrateOracleDB(db *sql.DB, migrationsDir string) error {
	goose.SetTableName("goose_db_version")
	return goose.Up(db, migrationsDir)
}

// OracleRepository struct of a oracle repository
// Needs a specific implementation of the Repository interface for every entity
type OracleRepository struct {
	DB *sql.DB
}
