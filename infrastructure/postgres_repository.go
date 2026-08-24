package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/lib/pq"
	"github.com/tehuelche/scv-go-tools/v3/wrappers"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
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

	// An entity that carries an id keeps it. Callers generate one and hand it
	// out before the record exists — a trip is written to Firestore under the
	// id the app knows it by, and the apps poll for it there — so assigning a
	// different one leaves a record nothing can find again.
	//
	// This is what the Mongo repository does: an _id on the entity is the id of
	// the document. Only an entity without one gets a key from the database.
	id, carried := documentID(document)
	delete(document, "_id")

	payload, err := bson.MarshalExtJSON(document, false, false)
	if err != nil {
		return "", err
	}

	if carried {
		query := fmt.Sprintf("INSERT INTO %s (id, doc) VALUES ($1, $2) RETURNING id", r.table())
		if err := r.DB.QueryRowContext(ctx, query, id, payload).Scan(&id); err != nil {
			return "", translateWriteErr(err)
		}
		return id, nil
	}

	query := fmt.Sprintf("INSERT INTO %s (doc) VALUES ($1) RETURNING id", r.table())
	if err := r.DB.QueryRowContext(ctx, query, payload).Scan(&id); err != nil {
		return "", translateWriteErr(err)
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

	query := fmt.Sprintf("SELECT id, doc FROM %s%s ORDER BY id", r.table(), where)
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
	query := fmt.Sprintf("SELECT id, doc FROM %s WHERE id = $1", r.table())

	row := r.DB.QueryRowContext(ctx, query, ID)

	var id string
	var payload []byte
	if err := row.Scan(&id, &payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, wrappers.NewNonExistentErr(err)
		}
		return nil, err
	}

	return r.decode(id, payload)
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
	var id string
	var payload []byte
	if err := rows.Scan(&id, &payload); err != nil {
		return nil, err
	}
	return r.decode(id, payload)
}

// decode turns a stored row back into the repository's target type.
//
// The id is put back into the document before decoding. It is kept in its own
// column so an update cannot rewrite the key it was addressed by, but an entity
// read without its id is one nothing can be done with afterwards: no update, no
// delete, and no reference from anything else.
func (r *PostgresRepository) decode(id string, payload []byte) (interface{}, error) {
	var document map[string]interface{}
	if err := bson.UnmarshalExtJSON(payload, false, &document); err != nil {
		return nil, err
	}
	document["_id"] = id

	restored, err := bson.MarshalExtJSON(document, false, false)
	if err != nil {
		return nil, err
	}

	entry := reflect.New(reflect.TypeOf(r.Target)).Interface()
	if err := bson.UnmarshalExtJSON(restored, false, entry); err != nil {
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
// A plain value is matched with the jsonb containment operator, so a filter on
// a field name means what it meant against a document store: the document
// contains this value at this key.
//
// A value that is itself a map of operators — {"$nin": [...]} and the like — is
// translated instead. The generic repository contract allows those, and
// matching one by containment would look for a document whose field is
// literally that object: no error, no rows, and nothing to say why.
func containmentFilter(filter map[string]interface{}) (string, []interface{}, error) {
	if len(filter) == 0 {
		return "", nil, nil
	}

	conditions := []string{}
	args := []interface{}{}
	plain := map[string]interface{}{}

	// Sorted so the same filter always produces the same statement, which keeps
	// the query plan cache useful.
	fields := make([]string, 0, len(filter))
	for field := range filter {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	for _, field := range fields {
		operators, ok := operatorMap(filter[field])
		if !ok {
			plain[field] = filter[field]
			continue
		}

		condition, operatorArgs, err := operatorCondition(field, operators, len(args))
		if err != nil {
			return "", nil, err
		}

		conditions = append(conditions, condition)
		args = append(args, operatorArgs...)
	}

	if len(plain) > 0 {
		payload, err := bson.MarshalExtJSON(plain, false, false)
		if err != nil {
			return "", nil, err
		}
		args = append(args, payload)
		conditions = append(conditions, fmt.Sprintf("doc @> $%d", len(args)))
	}

	if len(conditions) == 0 {
		return "", nil, nil
	}

	return " WHERE " + strings.Join(conditions, " AND "), args, nil
}

// operatorMap reports whether a filter value is a set of operators rather than
// a value to match.
func operatorMap(value interface{}) (map[string]interface{}, bool) {
	operators, ok := value.(map[string]interface{})
	if !ok || len(operators) == 0 {
		return nil, false
	}

	for key := range operators {
		if !strings.HasPrefix(key, "$") {
			return nil, false
		}
	}

	return operators, true
}

// operatorCondition turns one field's operators into SQL.
//
// An operator with no translation is an error rather than something skipped:
// silently dropping a condition returns rows the caller asked to exclude, which
// is worse than failing.
func operatorCondition(field string, operators map[string]interface{}, offset int) (string, []interface{}, error) {
	parts := make([]string, 0, len(operators))
	args := make([]interface{}, 0, len(operators))

	names := make([]string, 0, len(operators))
	for name := range operators {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		value := operators[name]
		position := offset + len(args) + 1

		switch name {
		case "$in", "$nin":
			payload, err := bson.MarshalExtJSON(map[string]interface{}{"v": value}, false, false)
			if err != nil {
				return "", nil, err
			}
			args = append(args, payload)

			// Membership is asked of the value as jsonb, so it holds for
			// numbers and strings alike without the field needing a cast.
			test := fmt.Sprintf("(doc->%s) IN (SELECT jsonb_array_elements(($%d::jsonb)->'v'))",
				quoteLiteral(field), position)
			if name == "$nin" {
				test = "NOT " + test
			}
			parts = append(parts, test)

		case "$ne":
			payload, err := bson.MarshalExtJSON(map[string]interface{}{"v": value}, false, false)
			if err != nil {
				return "", nil, err
			}
			args = append(args, payload)
			parts = append(parts, fmt.Sprintf("(doc->%s) IS DISTINCT FROM (($%d::jsonb)->'v')",
				quoteLiteral(field), position))

		case "$exists":
			present, ok := value.(bool)
			if !ok {
				return "", nil, fmt.Errorf("$exists on %s needs a boolean", field)
			}
			test := fmt.Sprintf("doc ? %s", quoteLiteral(field))
			if !present {
				test = "NOT " + test
			}
			parts = append(parts, test)

		case "$gt", "$gte", "$lt", "$lte":
			payload, err := bson.MarshalExtJSON(map[string]interface{}{"v": value}, false, false)
			if err != nil {
				return "", nil, err
			}
			args = append(args, payload)
			parts = append(parts, fmt.Sprintf("(doc->%s) %s (($%d::jsonb)->'v')",
				quoteLiteral(field), comparison(name), position))

		default:
			return "", nil, fmt.Errorf("filter operator %s on %s has no translation", name, field)
		}
	}

	return "(" + strings.Join(parts, " AND ") + ")", args, nil
}

func comparison(operator string) string {
	switch operator {
	case "$gt":
		return ">"
	case "$gte":
		return ">="
	case "$lt":
		return "<"
	default:
		return "<="
	}
}

// quoteLiteral quotes a field name for use inside a statement.
func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// quoteIdentifier quotes an identifier so a table name cannot be read as SQL.
func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// documentID reads the id an entity carries, if it carries one.
//
// An empty value counts as none: entities tag their id omitempty, and a zero id
// means the caller left it to the database rather than asking for that key.
func documentID(document map[string]interface{}) (string, bool) {
	raw, ok := document["_id"]
	if !ok || raw == nil {
		return "", false
	}

	// The id arrives as an ObjectID when the entity types it that way, and as a
	// string when it does not — PostgreSQL keys rows by text and takes either.
	switch value := raw.(type) {
	case primitive.ObjectID:
		if value.IsZero() {
			return "", false
		}
		return value.Hex(), true

	case string:
		if value == "" {
			return "", false
		}
		return value, true

	default:
		return "", false
	}
}

// translateWriteErr says what a rejected write means in terms the callers share
// with the other adapters.
//
// PostgreSQL reports a unique violation as SQLSTATE 23505 and MongoDB reports
// the same situation as E11000. A caller that has to know both is a caller
// tied to whichever one it happens to run on today.
func translateWriteErr(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return wrappers.NewAlreadyExistsErr(err)
	}

	return err
}

// uniqueViolation is the SQLSTATE PostgreSQL reports when a unique index or
// primary key rejects a write.
const uniqueViolation = "23505"
