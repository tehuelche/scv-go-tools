package infrastructure

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tehuelche/scv-go-tools/v3/mocks"
)

// TestConnectMysqlDB_InvalidDSN checks that ConnectMysqlDB returns an error when an invalid DSN is provided
func TestConnectMysqlDB_InvalidDSN(t *testing.T) {
	// Arrange
	expectedError := "missing \"=\" after \"invalid-dsn\" in connection info string\""

	// Act
	_, err := ConnectMysqlDB(context.Background(), "invalid-dsn")

	// Assert
	assert.Equal(t, expectedError, err.Error())
}

// TestPingMySql_Ok checks that pingMySql does not return an error when a valid db is received
func TestPingMySql_Ok(t *testing.T) {
	// Arrange
	_, db := mocks.NewSqlDB(t)

	// Act
	err := pingMySql(context.Background(), db)

	// Assert
	assert.Nil(t, err)
}

// TestMigrateMysqlDB_NotValidDirectory checks that MigrateMysqlDB retuns an error when the given directory does not exist
func TestMigrateMysqlDB_NotValidDirectory(t *testing.T) {
	// Arrange
	_, db := mocks.NewSqlDB(t)
	expectedError := "invalid-directory directory does not exist"

	// Act
	err := MigrateMysqlDB(db, "invalid-directory")

	// Assert
	assert.Equal(t, expectedError, err.Error())
}
