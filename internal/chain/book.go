package chain

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

// Book 保存所有人工采样送检链条，并串行化每一次交接。
type Book struct {
	mu    sync.RWMutex
	tasks map[uuid.UUID]model.SamplingTask
}

func NewBook() *Book {
	return &Book{tasks: make(map[uuid.UUID]model.SamplingTask)}
}

// Create 登记一条采样任务。
func (b *Book) Create(task model.SamplingTask) (model.SamplingTask, error) {
	if task.ID == uuid.Nil {
		return model.SamplingTask{}, errors.New("sampling task id is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.tasks[task.ID]; exists {
		return model.SamplingTask{}, errors.New("sampling task already exists")
	}
	for _, existing := range b.tasks {
		if existing.SampleID == task.SampleID {
			return model.SamplingTask{}, errors.New("sample already has a sampling chain")
		}
	}
	b.tasks[task.ID] = task
	return task, nil
}

func (b *Book) Get(id uuid.UUID) (model.SamplingTask, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	task, ok := b.tasks[id]
	return task, ok
}

// BySample 返回某传感器样本对应的链条。
func (b *Book) BySample(sampleID uuid.UUID) (model.SamplingTask, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, task := range b.tasks {
		if task.SampleID == sampleID {
			return task, true
		}
	}
	return model.SamplingTask{}, false
}

// ByBatch 返回某处理批次下的全部采样链条（按采样时间倒序）。
func (b *Book) ByBatch(batchID uuid.UUID) []model.SamplingTask {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]model.SamplingTask, 0)
	for _, task := range b.sortedLocked() {
		if task.BatchID == batchID {
			result = append(result, task)
		}
	}
	return result
}

func (b *Book) update(id uuid.UUID, apply func(model.SamplingTask) (model.SamplingTask, error)) (model.SamplingTask, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	task, ok := b.tasks[id]
	if !ok {
		return model.SamplingTask{}, errors.New("sampling task is not found")
	}
	updated, err := apply(task)
	if err != nil {
		return model.SamplingTask{}, err
	}
	b.tasks[id] = updated
	return updated, nil
}

func (b *Book) Seal(id uuid.UUID, sealedBy string, now time.Time) (model.SamplingTask, error) {
	return b.update(id, func(task model.SamplingTask) (model.SamplingTask, error) {
		return task.Seal(sealedBy, now)
	})
}

func (b *Book) Receive(id uuid.UUID, receivedBy string, now time.Time) (model.SamplingTask, error) {
	return b.update(id, func(task model.SamplingTask) (model.SamplingTask, error) {
		return task.Receive(receivedBy, now)
	})
}

func (b *Book) Archive(id uuid.UUID, actor string, now time.Time) (model.SamplingTask, error) {
	return b.update(id, func(task model.SamplingTask) (model.SamplingTask, error) {
		return task.Archive(actor, now)
	})
}

func (b *Book) MarkLost(id uuid.UUID, actor, reason string, now time.Time) (model.SamplingTask, error) {
	return b.update(id, func(task model.SamplingTask) (model.SamplingTask, error) {
		return task.MarkLost(actor, reason, now)
	})
}

func (b *Book) Revoke(id uuid.UUID, actor, reason string, now time.Time) (model.SamplingTask, error) {
	return b.update(id, func(task model.SamplingTask) (model.SamplingTask, error) {
		return task.Revoke(actor, reason, now)
	})
}

// ValidForLab 判定某样本当前是否处于可匹配化验结果的链条上。
// 没有登记链条的样本一律视为无效，杜绝绕过送检直接出结果。
func (b *Book) ValidForLab(sampleID uuid.UUID) (model.SamplingTask, bool) {
	task, ok := b.BySample(sampleID)
	if !ok || !task.ValidForLab() {
		return model.SamplingTask{}, false
	}
	return task, true
}

// sortedLocked 返回按采样时间倒序排列的全部链条副本，调用方须已持有读锁。
func (b *Book) sortedLocked() []model.SamplingTask {
	result := make([]model.SamplingTask, 0, len(b.tasks))
	for _, task := range b.tasks {
		result = append(result, task)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CollectedAt.After(result[j].CollectedAt) })
	return result
}

// sorted 加读锁后返回倒序副本。
func (b *Book) sorted() []model.SamplingTask {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.sortedLocked()
}

// PendingReceipt 待接收视图：已采样/已封样、等待实验室签收的链条。
func (b *Book) PendingReceipt() []model.SamplingTask {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]model.SamplingTask, 0)
	for _, task := range b.sortedLocked() {
		if task.Status == model.ChainSealed || task.Status == model.ChainCollected {
			result = append(result, task)
		}
	}
	return result
}

// Archived 已归档视图：化验完成后归档的链条。
func (b *Book) Archived() []model.SamplingTask {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]model.SamplingTask, 0)
	for _, task := range b.sortedLocked() {
		if task.Status == model.ChainArchived {
			result = append(result, task)
		}
	}
	return result
}

// All 返回全部链条，供状态总览使用。
func (b *Book) All() []model.SamplingTask {
	return b.sorted()
}
