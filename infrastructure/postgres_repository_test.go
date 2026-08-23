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
	id     text PRIMARY KEY DEFAULT gen_random_uuid()::text,
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

// TestPostgresFilterWithNotIn covers the operator that started this: the trip
// service asks for a driver's trips excluding the cancelled statuses, and
// matching {"$nin": [...]} by containment looked for a document whose field is
// literally that object — no error, no rows, and nothing to say why.
func TestPostgresFilterWithNotIn(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	for _, status := range []int{1, 5, 9} {
		entity := sample()
		entity.Status = status
		if _, err := repo.Create(ctx, entity); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	found, err := repo.Get(ctx, map[string]interface{}{
		"status": map[string]interface{}{"$nin": []int{9}},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(found) != 2 {
		t.Fatalf("len = %d, want 2", len(found))
	}
	for _, entry := range found {
		if entry.(*sampleEntity).Status == 9 {
			t.Error("a status the filter excluded came back")
		}
	}
}

func TestPostgresFilterWithIn(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	for _, status := range []int{1, 5, 9} {
		entity := sample()
		entity.Status = status
		if _, err := repo.Create(ctx, entity); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	found, err := repo.Get(ctx, map[string]interface{}{
		"status": map[string]interface{}{"$in": []int{1, 9}},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(found) != 2 {
		t.Errorf("len = %d, want 2", len(found))
	}
}

// TestPostgresFilterMixesOperatorsAndValues is the shape the trip service uses:
// one field matched by value, another by operator.
func TestPostgresFilterMixesOperatorsAndValues(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	wanted := sample()
	wanted.Name = "wanted"
	wanted.Status = 5
	if _, err := repo.Create(ctx, wanted); err != nil {
		t.Fatalf("Create: %v", err)
	}

	excludedByStatus := sample()
	excludedByStatus.Name = "wanted"
	excludedByStatus.Status = 9
	if _, err := repo.Create(ctx, excludedByStatus); err != nil {
		t.Fatalf("Create: %v", err)
	}

	excludedByName := sample()
	excludedByName.Name = "other"
	excludedByName.Status = 5
	if _, err := repo.Create(ctx, excludedByName); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.Get(ctx, map[string]interface{}{
		"name":   "wanted",
		"status": map[string]interface{}{"$nin": []int{9}},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(found) != 1 {
		t.Fatalf("len = %d, want 1", len(found))
	}
	if entity := found[0].(*sampleEntity); entity.Name != "wanted" || entity.Status != 5 {
		t.Errorf("got %+v", entity)
	}
}

func TestPostgresFilterWithComparison(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	for _, status := range []int{1, 5, 9} {
		entity := sample()
		entity.Status = status
		if _, err := repo.Create(ctx, entity); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	found, err := repo.Get(ctx, map[string]interface{}{
		"status": map[string]interface{}{"$gte": 5},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(found) != 2 {
		t.Errorf("len = %d, want 2", len(found))
	}
}

func TestPostgresFilterWithExists(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	withTags := sample()
	if _, err := repo.Create(ctx, withTags); err != nil {
		t.Fatalf("Create: %v", err)
	}

	withoutTags := sample()
	withoutTags.Tags = nil
	if _, err := repo.Create(ctx, withoutTags); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.Get(ctx, map[string]interface{}{
		"tags": map[string]interface{}{"$exists": true},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("len = %d, want 1", len(found))
	}
}

// TestPostgresRejectsUnknownOperator keeps an untranslated operator from being
// dropped. Skipping a condition returns rows the caller asked to exclude, which
// is worse than failing.
func TestPostgresRejectsUnknownOperator(t *testing.T) {
	repo := newTestRepository(t)

	_, err := repo.Get(context.Background(), map[string]interface{}{
		"name": map[string]interface{}{"$regex": "^a"},
	}, nil, nil)

	if err == nil {
		t.Fatal("expected an untranslated operator to fail")
	}
}

// TestPostgresCreateKeepsTheIDTheEntityCarries is the bug this covers: callers
// generate an id and hand it out before the record exists. A trip is written to
// Firestore under the id the app knows it by, and the apps poll for it there,
// so assigning a different one leaves a record nothing can find again — the
// trip was created, matched and paid for, and every lookup afterwards missed.
func TestPostgresCreateKeepsTheIDTheEntityCarries(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	entity := sample()
	entity.ID = "654c2c92ec153122642d49ad"

	id, err := repo.Create(ctx, entity)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if id != entity.ID {
		t.Fatalf("Create returned %q, want the id the entity carried, %q", id, entity.ID)
	}

	got, err := repo.GetByID(ctx, entity.ID)
	if err != nil {
		t.Fatalf("GetByID with the id the caller knows: %v", err)
	}
	if got.(*sampleEntity).ID != entity.ID {
		t.Errorf("read back %q, want %q", got.(*sampleEntity).ID, entity.ID)
	}
}

// TestPostgresCreateAssignsAnIDWhenTheEntityHasNone keeps the other half: an
// entity that leaves the id empty gets one from the database.
func TestPostgresCreateAssignsAnIDWhenTheEntityHasNone(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	id, err := repo.Create(ctx, sample())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if id == "" {
		t.Fatal("Create returned an empty id")
	}

	if _, err := repo.GetByID(ctx, id); err != nil {
		t.Fatalf("GetByID with the assigned id: %v", err)
	}
}
