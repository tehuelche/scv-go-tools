package infrastructure_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/tehuelche/scv-go-tools/v3/infrastructure"
	"github.com/tehuelche/scv-go-tools/v3/wrappers"
)

// sampleEntity stands in for a stored entity: BSON tags, a nested struct and a
// slice, which is what the real ones look like.
type sampleEntity struct {
	ID        string        `bson:"_id,omitempty"`
	Name      string        `bson:"name"`
	Status    int           `bson:"status"`
	Price     float64       `bson:"price"`
	Active    bool          `bson:"active"`
	CreatedAt time.Time     `bson:"created_at"`
	Origin    sampleAddress `bson:"origin"`
	Tags      []string      `bson:"tags,omitempty"`
}

type sampleAddress struct {
	Lat float64 `bson:"lat"`
	Lng float64 `bson:"lng"`
}

const createSampleTable = `
CREATE TABLE IF NOT EXISTS repository_samples (
	id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	doc    jsonb NOT NULL,
	status int GENERATED ALWAYS AS ((doc->>'status')::int) STORED
);
CREATE INDEX IF NOT EXISTS repository_samples_doc ON repository_samples USING gin (doc);
`

// newTestRepository connects to the PostgreSQL the integration environment
// runs. Without it the tests skip rather than fail: the unit suite has to pass
// on a machine with no database.
func newTestRepository(t *testing.T) *infrastructure.PostgresRepository {
	t.Helper()

	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN not set")
	}

	db, err := infrastructure.ConnectPostgresDB(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}

	if _, err := db.Exec(createSampleTable); err != nil {
		t.Fatalf("creating the table: %v", err)
	}
	if _, err := db.Exec("TRUNCATE repository_samples"); err != nil {
		t.Fatalf("truncating: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	return &infrastructure.PostgresRepository{
		DB:     db,
		Table:  "repository_samples",
		Target: sampleEntity{},
	}
}

func sample() sampleEntity {
	return sampleEntity{
		Name:      "a trip",
		Status:    5,
		Price:     12.34,
		Active:    true,
		CreatedAt: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC),
		Origin:    sampleAddress{Lat: 4.7, Lng: -74.05},
		Tags:      []string{"one", "two"},
	}
}

func TestPostgresCreateAndGetByID(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	id, err := repo.Create(ctx, sample())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" {
		t.Fatal("Create returned an empty id")
	}

	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	entity, ok := got.(*sampleEntity)
	if !ok {
		t.Fatalf("GetByID returned %T, want *sampleEntity", got)
	}

	want := sample()
	if entity.Name != want.Name || entity.Status != want.Status || entity.Price != want.Price {
		t.Errorf("scalars did not round-trip: %+v", entity)
	}
	if entity.Active != want.Active {
		t.Errorf("Active = %v, want %v", entity.Active, want.Active)
	}
	if !entity.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", entity.CreatedAt, want.CreatedAt)
	}
	if entity.Origin != want.Origin {
		t.Errorf("nested struct did not round-trip: %+v", entity.Origin)
	}
	if len(entity.Tags) != 2 || entity.Tags[0] != "one" {
		t.Errorf("slice did not round-trip: %v", entity.Tags)
	}
}

func TestPostgresGetByIDNotFound(t *testing.T) {
	repo := newTestRepository(t)

	// The callers branch on this, so a missing row has to arrive as the same
	// error the Mongo repository returns.
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, wrappers.NonExistentErr) {
		t.Fatalf("err = %v, want NonExistentErr", err)
	}
}

func TestPostgresGetByFilter(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	first := sample()
	first.Name = "first"
	if _, err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create: %v", err)
	}

	second := sample()
	second.Name = "second"
	second.Status = 9
	if _, err := repo.Create(ctx, second); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.Get(ctx, map[string]interface{}{"status": 9}, nil, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("len = %d, want 1", len(found))
	}
	if found[0].(*sampleEntity).Name != "second" {
		t.Errorf("got %q, want second", found[0].(*sampleEntity).Name)
	}
}

func TestPostgresGetOnNestedField(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, sample()); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Containment reaches into the document, which is what a filter on a
	// nested field meant against the document store.
	found, err := repo.Get(ctx, map[string]interface{}{
		"origin": map[string]interface{}{"lat": 4.7},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("len = %d, want 1", len(found))
	}
}

func TestPostgresGetEmptyIsNonExistent(t *testing.T) {
	repo := newTestRepository(t)

	_, err := repo.Get(context.Background(), map[string]interface{}{"status": 404}, nil, nil)
	if !errors.Is(err, wrappers.NonExistentErr) {
		t.Fatalf("err = %v, want NonExistentErr", err)
	}
}

func TestPostgresGetAllWithNoFilter(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := repo.Create(ctx, sample()); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	found, err := repo.Get(ctx, map[string]interface{}{}, nil, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(found) != 3 {
		t.Errorf("len = %d, want 3", len(found))
	}
}

func TestPostgresGetSkipAndTake(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := repo.Create(ctx, sample()); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	take := 2
	found, err := repo.Get(ctx, nil, nil, &take)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(found) != 2 {
		t.Errorf("take: len = %d, want 2", len(found))
	}

	skip := 3
	rest, err := repo.Get(ctx, nil, &skip, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(rest) != 2 {
		t.Errorf("skip: len = %d, want 2", len(rest))
	}
}

func TestPostgresUpdate(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	id, err := repo.Create(ctx, sample())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	changed := sample()
	changed.Name = "renamed"
	changed.Status = 7
	if err := repo.Update(ctx, id, changed); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	entity := got.(*sampleEntity)
	if entity.Name != "renamed" || entity.Status != 7 {
		t.Errorf("update did not stick: %+v", entity)
	}
}

func TestPostgresUpdateMissingIsNonExistent(t *testing.T) {
	repo := newTestRepository(t)

	err := repo.Update(context.Background(), "00000000-0000-0000-0000-000000000000", sample())
	if !errors.Is(err, wrappers.NonExistentErr) {
		t.Fatalf("err = %v, want NonExistentErr", err)
	}
}

func TestPostgresDelete(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	id, err := repo.Create(ctx, sample())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := repo.GetByID(ctx, id); !errors.Is(err, wrappers.NonExistentErr) {
		t.Fatalf("after Delete, GetByID err = %v, want NonExistentErr", err)
	}
}

func TestPostgresDeleteMissingIsNonExistent(t *testing.T) {
	repo := newTestRepository(t)

	err := repo.Delete(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, wrappers.NonExistentErr) {
		t.Fatalf("err = %v, want NonExistentErr", err)
	}
}

// TestPostgresCreateDoesNotStoreTheID keeps the id in its column and out of the
// document, so an update cannot rewrite the key it was addressed by.
func TestPostgresCreateDoesNotStoreTheID(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	entity := sample()
	entity.ID = "654c2c92ec153122642d49ad"

	id, err := repo.Create(ctx, entity)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var raw string
	row := repo.DB.QueryRowContext(ctx, `SELECT doc::text FROM repository_samples WHERE id = $1`, id)
	if err := row.Scan(&raw); err != nil {
		t.Fatalf("reading the document: %v", err)
	}

	if contains(raw, `"_id"`) {
		t.Errorf("the document still carries an _id: %s", raw)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// TestPostgresReadsBackTheID covers what a round trip is for: an entity read
// without its id is one nothing can be done with afterwards — no update, no
// delete, and no reference from anything else. The id lives in its own column,
// so reading has to put it back.
func TestPostgresReadsBackTheID(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	id, err := repo.Create(ctx, sample())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if entity := got.(*sampleEntity); entity.ID != id {
		t.Errorf("GetByID returned ID %q, want %q", entity.ID, id)
	}

	found, err := repo.Get(ctx, nil, nil, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("len = %d, want 1", len(found))
	}
	if entity := found[0].(*sampleEntity); entity.ID != id {
		t.Errorf("Get returned ID %q, want %q", entity.ID, id)
	}
}

// TestPostgresUpdateAfterReadKeepsTheRow is the failure the previous test
// guards against, end to end: read an entity, change it, write it back. With
// the id missing from the read, the caller has nothing to address the update
// with.
func TestPostgresUpdateAfterReadKeepsTheRow(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	id, err := repo.Create(ctx, sample())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	entity := got.(*sampleEntity)
	entity.Name = "renamed"

	if err := repo.Update(ctx, entity.ID, *entity); err != nil {
		t.Fatalf("Update with the id that came back: %v", err)
	}

	back, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if back.(*sampleEntity).Name != "renamed" {
		t.Errorf("the update did not stick: %+v", back)
	}
}
