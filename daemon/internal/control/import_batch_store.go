package control

import (
	"fmt"
	"sync"
	"time"

	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
)

const importBatchTTL = 30 * time.Minute

type ImportBatchStore struct {
	mu      sync.Mutex
	batches map[string]*importBatch
}

type importBatch struct {
	totalBatches int
	chunks       [][]profiledoc.Node
	createdAt    time.Time
	updatedAt    time.Time
}

type ImportBatchState struct {
	BatchID         string
	TotalBatches    int
	ReceivedBatches int
	ReceivedNodes   int
	Ready           bool
}

func NewImportBatchStore() *ImportBatchStore {
	return &ImportBatchStore{batches: make(map[string]*importBatch)}
}

func (s *ImportBatchStore) Add(batchID string, totalBatches int, nodes []profiledoc.Node, now time.Time) (ImportBatchState, error) {
	if s == nil {
		return ImportBatchState{}, fmt.Errorf("import batch store is not configured")
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked(now)
	batch := s.batches[batchID]
	if batch == nil {
		batch = &importBatch{totalBatches: totalBatches, createdAt: now}
		s.batches[batchID] = batch
	} else if batch.totalBatches != totalBatches {
		return ImportBatchState{}, fmt.Errorf("batch totalBatches mismatch: got %d, expected %d", totalBatches, batch.totalBatches)
	}
	if len(batch.chunks) >= batch.totalBatches {
		return ImportBatchState{}, fmt.Errorf("batch already has %d/%d chunks", len(batch.chunks), batch.totalBatches)
	}
	batch.chunks = append(batch.chunks, append([]profiledoc.Node(nil), nodes...))
	batch.updatedAt = now
	return importBatchState(batchID, batch), nil
}

func (s *ImportBatchStore) Commit(batchID string, now time.Time) ([]profiledoc.Node, ImportBatchState, error) {
	if s == nil {
		return nil, ImportBatchState{}, fmt.Errorf("import batch store is not configured")
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked(now)
	batch := s.batches[batchID]
	if batch == nil {
		return nil, ImportBatchState{}, fmt.Errorf("batch not found")
	}
	state := importBatchState(batchID, batch)
	if !state.Ready {
		return nil, state, fmt.Errorf("batch is incomplete: received %d/%d", state.ReceivedBatches, state.TotalBatches)
	}
	nodes := make([]profiledoc.Node, 0, state.ReceivedNodes)
	for _, chunk := range batch.chunks {
		nodes = append(nodes, chunk...)
	}
	delete(s.batches, batchID)
	return nodes, state, nil
}

func (s *ImportBatchStore) cleanupLocked(now time.Time) {
	for batchID, batch := range s.batches {
		lastTouch := batch.updatedAt
		if lastTouch.IsZero() {
			lastTouch = batch.createdAt
		}
		if now.Sub(lastTouch) > importBatchTTL {
			delete(s.batches, batchID)
		}
	}
}

func importBatchState(batchID string, batch *importBatch) ImportBatchState {
	state := ImportBatchState{
		BatchID:         batchID,
		TotalBatches:    batch.totalBatches,
		ReceivedBatches: len(batch.chunks),
	}
	for _, chunk := range batch.chunks {
		state.ReceivedNodes += len(chunk)
	}
	state.Ready = state.ReceivedBatches == state.TotalBatches
	return state
}
