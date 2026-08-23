package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/tehuelche/scv-go-tools/v3/wrappers"
	"go.mongodb.org/mongo-driver/bson"
)

// Create inserts an entity and returns its id.
//
// The document is stored as jsonb. Entities are tagged for BSON rather than
// encoding/json, so the marshalling goes through Extended JSON: it honours
// those tags, which is what keeps a filter written against a field name
// matching the column it was stored under, and it keeps dates and numbers
// typed instead of flattening them into strings.
func (r *PostgresRepository) Create(ctx context.Context, entity interface{}) (string, error) {
	document, err := toDocument(entity)
	if err != nil {
		return "", err
	}

	// The id is assigned by the database when the entity does not carry one,
	// which is how the Mongo repository behaves.
	delete(document, "_id")

	payload, err := bson.MarshalExtJSON(document, false, false)
	if err != nil {
		return "", err
	}

	var id string
	query := fmt.Sprintf("INSERT INTO %s (doc) VALUES ($1) RETURNING id", r.table())
	if err := r.DB.QueryRowContext(ctx, query, payload).Scan(&id); err != nil {
		return "", err
	}

	return id, nil
}

// Get returns the entities matching the filter.
//
// An empty result is a NonExistentErr rather than an empty slice, which is the
// contract the callers were written against.
func (r *PostgresRepository) Get(ctx context.Context, filter map[string]interface{}, skip, take *int) ([]interface{}, error) {
	where, args, err := containmentFilter(filter)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf("SELECT doc FROM %s%s ORDER BY id", r.table(), where)
	if take != nil && *take > 0 {
		args = append(args, *take)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if skip != nil && *skip > 0 {
		args = append(args, *skip)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []interface{}
	for rows.Next() {
		entry, err := r.scanEntity(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(result) < 1 {
		return nil, wrappers.NewNonExistentErr(sql.ErrNoRows)
	}

	return result, nil
}

// GetByID returns the entity with the given id.
func (r *PostgresRepository) GetByID(ctx context.Context, ID string) (interface{}, error) {
	query := fmt.Sprintf("SELECT doc FROM %s WHERE id = $1", r.table())

	row := r.DB.QueryRowContext(ctx, query, ID)

	var payload []byte
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, wrappers.NewNonExistentErr(err)
		}
		return nil, err
	}

	return r.decode(payload)
}

// Update replaces the document of the entity with the given id.
//
// It replaces rather than merges, which is what $set with a whole entity does
// on the Mongo side.
func (r *PostgresRepository) Update(ctx context.Context, ID string, entity interface{}) error {
	document, err := toDocument(entity)
	if err != nil {
		return err
	}

	// The id lives in its own column; leaving it in the document would let an
	// update rewrite the key it was addressed by.
	delete(document, "_id")

	payload, err := bson.MarshalExtJSON(document, false, false)
	if err != nil {
		return err
	}

	query := fmt.Sprintf("UPDATE %s SET doc = $1 WHERE id = $2", r.table())
	result, err := r.DB.ExecContext(ctx, query, payload, ID)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected < 1 {
		return wrappers.NewNonExistentErr(sql.ErrNoRows)
	}

	return nil
}

// Delete removes the entity with the given id.
func (r *PostgresRepository) Delete(ctx context.Context, ID string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE id = $1", r.table())

	result, err := r.DB.ExecContext(ctx, query, ID)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected < 1 {
		return wrappers.NewNonExistentErr(sql.ErrNoRows)
	}

	return nil
}

// table returns the quoted table name.
func (r *PostgresRepository) table() string {
	return quoteIdentifier(r.Table)
}

// scanEntity reads one row into a new instance of the repository's target type.
func (r *PostgresRepository) scanEntity(rows *sql.Rows) (interface{}, error) {
	var payload []byte
	if err := rows.Scan(&payload); err != nil {
		return nil, err
	}
	return r.decode(payload)
}

// decode turns a stored document back into the repository's target type.
func (r *PostgresRepository) decode(payload []byte) (interface{}, error) {
	entry := reflect.New(reflect.TypeOf(r.Target)).Interface()
	if err := bson.UnmarshalExtJSON(payload, false, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// toDocument turns an entity into a map keyed by its stored field names.
func toDocument(entity interface{}) (map[string]interface{}, error) {
	payload, err := bson.MarshalExtJSON(entity, false, false)
	if err != nil {
		return nil, err
	}

	var document map[string]interface{}
	if err := bson.UnmarshalExtJSON(payload, false, &document); err != nil {
		return nil, err
	}

	return document, nil
}

// containmentFilter builds the WHERE clause for a filter.
//
// The filter is matched with the jsonb containment operator, so a filter on a
// field name means the same thing it meant against a document store: the
// document contains this value at this key.
func containmentFilter(filter map[string]interface{}) (string, []interface{}, error) {
	if len(filter) == 0 {
		return "", nil, nil
	}

	payload, err := bson.MarshalExtJSON(filter, false, false)
	if err != nil {
		return "", nil, err
	}

	return " WHERE doc @> $1", []interface{}{payload}, nil
}

// quoteIdentifier quotes an identifier so a table name cannot be read as SQL.
func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
