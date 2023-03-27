package infrastructure

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
)

// ConnectMysqlDB opens a connection to the MysqlDB and ensures that the db is reachable
func ConnectMysqlDB(ctx context.Context, dsn string) (*sql.DB, error) {
	db, _ := sql.Open("mysql", dsn)
	return db, pingMySql(ctx, db)
}

func pingMySql(ctx context.Context, db *sql.DB) (err error) {
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

// MigrateMysqlDB runs all migrations found in the given directory against the db
func MigrateMysqlDB(db *sql.DB, migrationsDir string) error {
	goose.SetTableName("goose_db_version")
	return goose.Up(db, migrationsDir)
}

// MysqlRepository struct of a mysql repository
// Needs a specific implementation of the Repository interface for every entity
type MysqlRepository struct {
	DB *sql.DB
}
