// Package canvas implements the creator-facing canvas surface (09 号票):
// multi-canvas CRUD and whole-graph JSON persistence with a version-number
// optimistic lock. Nodes and edges are never rows — the graph lives in one
// JSON column and travels as a whole; auto-save debounces on the client and
// the version check catches lost updates (two tabs editing one canvas).
package canvas

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound reports a canvas id that has no row.
var ErrNotFound = errors.New("canvas: not found")

// ErrVersionConflict reports a SaveGraph whose expected version no longer
// matches the stored row: another writer saved in between.
var ErrVersionConflict = errors.New("canvas: version conflict")

// Canvas is one persisted canvas. Graph is the whole-graph JSON document
// (vue-flow elements: nodes, edges). The server never interprets its shape,
// so the editor schema can evolve without a migration; the MySQL JSON column
// validates and normalizes it, so round-trips are document-equal, not
// byte-equal.
type Canvas struct {
	ID        int64
	Name      string
	Graph     []byte
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store persists canvases. SaveGraph is the optimistic-lock seam: it stores
// the graph only when the stored version still equals expectedVersion,
// answering ErrVersionConflict otherwise.
type Store interface {
	List(ctx context.Context) ([]Canvas, error)
	Get(ctx context.Context, id int64) (Canvas, error)
	Create(ctx context.Context, name string, graph []byte) (Canvas, error)
	Rename(ctx context.Context, id int64, name string) (Canvas, error)
	Delete(ctx context.Context, id int64) error
	SaveGraph(ctx context.Context, id int64, graph []byte, expectedVersion int64) (Canvas, error)
}
