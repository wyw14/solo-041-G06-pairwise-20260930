package discharge

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

type Permit struct {
	ID             uuid.UUID    `json:"id"`
	LineID         model.LineID `json:"line_id"`
	BatchID        uuid.UUID    `json:"batch_id"`
	SampleID       uuid.UUID    `json:"sample_id"`
	SampleRevision uint64       `json:"sample_revision"`
	Active         bool         `json:"active"`
	IssuedAt       time.Time    `json:"issued_at"`
	RevokedAt      *time.Time   `json:"revoked_at,omitempty"`
}

type PermitBook struct {
	mu      sync.RWMutex
	byBatch map[uuid.UUID]Permit
	valves  *ValveController
}

func NewPermitBook(valves *ValveController) (*PermitBook, error) {
	if valves == nil {
		return nil, errors.New("permit book requires valve controller")
	}
	return &PermitBook{byBatch: make(map[uuid.UUID]Permit), valves: valves}, nil
}

func (b *PermitBook) ApplyResult(result model.LabResult, now time.Time) (Permit, error) {
	if !result.Valid || result.SampleID == uuid.Nil || result.BatchID == uuid.Nil || result.Revision == 0 {
		return Permit{}, errors.New("permit result is invalid")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	current, exists := b.byBatch[result.BatchID]
	if exists && current.SampleID == result.SampleID && result.Revision <= current.SampleRevision {
		return current, errors.New("permit result revision is stale")
	}
	if !result.Qualified() {
		if exists && current.Active {
			stamp := now.UTC()
			current.Active = false
			current.SampleRevision = result.Revision
			current.RevokedAt = &stamp
			b.byBatch[result.BatchID] = current
			if err := b.valves.Hold(result.LineID, now); err != nil {
				return Permit{}, err
			}
			return current, nil
		}
		return Permit{}, errors.New("result is not qualified for discharge")
	}
	permit := Permit{
		ID: uuid.New(), LineID: result.LineID, BatchID: result.BatchID,
		SampleID: result.SampleID, SampleRevision: result.Revision, Active: true, IssuedAt: now.UTC(),
	}
	if err := b.valves.Open(result.LineID, permit.ID, now); err != nil {
		return Permit{}, err
	}
	b.byBatch[result.BatchID] = permit
	return permit, nil
}

// InvalidateSample 因样本丢失或撤销而吊销由该样本支撑的出水许可，并保持阀门关闭。
// 若批次许可是由另一条仍有效样本支撑的，则不受牵连。批次本就没有匹配的有效许可时为空操作。
func (b *PermitBook) InvalidateSample(batchID, sampleID uuid.UUID, now time.Time) (Permit, bool, error) {
	b.mu.Lock()
	permit, exists := b.byBatch[batchID]
	if !exists || !permit.Active || permit.SampleID != sampleID {
		b.mu.Unlock()
		return Permit{}, false, nil
	}
	lineID := permit.LineID
	stamp := now.UTC()
	permit.Active = false
	permit.RevokedAt = &stamp
	b.byBatch[batchID] = permit
	if err := b.valves.Hold(lineID, now); err != nil {
		b.mu.Unlock()
		return Permit{}, false, err
	}
	b.mu.Unlock()
	return permit, true, nil
}

func (b *PermitBook) Current(batchID uuid.UUID) (Permit, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	permit, ok := b.byBatch[batchID]
	return permit, ok
}

func (b *PermitBook) All() []Permit {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]Permit, 0, len(b.byBatch))
	for _, permit := range b.byBatch {
		result = append(result, permit)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].IssuedAt.After(result[j].IssuedAt) })
	return result
}
