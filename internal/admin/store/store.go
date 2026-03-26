package store

import (
"context"
"database/sql"
_ "embed"
"fmt"
"math/rand"
"time"

"github.com/StevenBuglione/open-cli/internal/admin/domain"
_ "github.com/lib/pq"
)

//go:embed schema.sql
var schema string

// Store provides persistence for admin control-plane state
type Store struct {
db *sql.DB
}

// New creates a new Store with the given database connection
func New(db *sql.DB) *Store {
return &Store{db: db}
}

// InitSchema initializes the database schema
func (s *Store) InitSchema(ctx context.Context) error {
_, err := s.db.ExecContext(ctx, schema)
return err
}

// CreateSource creates a new source and returns its ID
func (s *Store) CreateSource(ctx context.Context, input domain.CreateSourceInput) (string, error) {
id := newID("src")
now := time.Now()
_, err := s.db.ExecContext(ctx, `
INSERT INTO admin_sources (id, kind, display_name, status, created_at, updated_at)
VALUES ($1, $2, $3, 'draft', $4, $5)
`, id, input.Kind, input.DisplayName, now, now)
return id, err
}

// newID generates a new ID with the given prefix
func newID(prefix string) string {
const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
b := make([]byte, 16)
for i := range b {
b[i] = charset[rand.Intn(len(charset))]
}
return fmt.Sprintf("%s_%s", prefix, string(b))
}

func init() {
rand.Seed(time.Now().UnixNano())
}
