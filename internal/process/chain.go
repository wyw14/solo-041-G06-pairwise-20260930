package process

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

// CreateSamplingChain 为一条已入库的传感器样本登记人工采样送检任务。
// batchID 为 uuid.Nil 时自动绑定该处理线当前的活动批次。
func (p *Plant) CreateSamplingChain(sampleID uuid.UUID, batchID uuid.UUID, sampler string) (model.SamplingTask, error) {
	if sampleID == uuid.Nil {
		return model.SamplingTask{}, errors.New("sample id is required")
	}
	sample, ok := p.sampler.Sample(sampleID)
	if !ok {
		return model.SamplingTask{}, errors.New("sensor sample is not found")
	}
	if batchID == uuid.Nil {
		batch, hasBatch := p.ActiveBatch(sample.LineID)
		if !hasBatch || !batch.IsActive() {
			return model.SamplingTask{}, errors.New("process line has no active batch for sampling")
		}
		batchID = batch.ID
	} else {
		p.mu.RLock()
		batch, exists := p.batches[batchID]
		p.mu.RUnlock()
		if !exists || batch.LineID != sample.LineID {
			return model.SamplingTask{}, errors.New("sampling batch does not belong to sample line")
		}
		if !batch.IsActive() {
			return model.SamplingTask{}, errors.New("sampling batch is not active")
		}
	}
	task, err := model.NewSamplingTask(sampleID, sample.LineID, sample.BasinID, batchID, sample.Kind, sampler, p.now())
	if err != nil {
		return model.SamplingTask{}, err
	}
	return p.chains.Create(task)
}

// SealSamplingChain 现场封样。
func (p *Plant) SealSamplingChain(id uuid.UUID, sealedBy string) (model.SamplingTask, error) {
	return p.chains.Seal(id, sealedBy, p.now())
}

// ReceiveSamplingChain 实验室签收，记录接收时间，链条自此可匹配化验结果。
func (p *Plant) ReceiveSamplingChain(id uuid.UUID, receivedBy string) (model.SamplingTask, error) {
	return p.chains.Receive(id, receivedBy, p.now())
}

// ArchiveSamplingChain 化验完成后归档链条。
func (p *Plant) ArchiveSamplingChain(id uuid.UUID, actor string) (model.SamplingTask, error) {
	return p.chains.Archive(id, actor, p.now())
}

// MarkSampleLost 标记送检途中丢失：样本作废，并吊销其批次已开出的许可。
func (p *Plant) MarkSampleLost(id uuid.UUID, actor, reason string) (model.SamplingTask, error) {
	return p.finalizeChain(id, actor, reason, true)
}

// RevokeSamplingChain 撤销样本：样本作废，并吊销其批次已开出的许可。
func (p *Plant) RevokeSamplingChain(id uuid.UUID, actor, reason string) (model.SamplingTask, error) {
	return p.finalizeChain(id, actor, reason, false)
}

func (p *Plant) finalizeChain(id uuid.UUID, actor, reason string, lost bool) (model.SamplingTask, error) {
	now := p.now()
	var (
		task model.SamplingTask
		err  error
	)
	if lost {
		task, err = p.chains.MarkLost(id, actor, reason, now)
	} else {
		task, err = p.chains.Revoke(id, actor, reason, now)
	}
	if err != nil {
		return model.SamplingTask{}, err
	}
	p.invalidateSampleBatch(task, now)
	if _, _, err := p.permits.InvalidateSample(task.BatchID, task.SampleID, now); err != nil {
		return model.SamplingTask{}, err
	}
	return task, nil
}

// invalidateSampleBatch 撤销/丢失链条样本后，若批次合格判定正是基于该样本，
// 则回退批次合格状态，避免作废样本继续支撑合格结论。
func (p *Plant) invalidateSampleBatch(task model.SamplingTask, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	batch, exists := p.batches[task.BatchID]
	if !exists || batch.QualificationSample != task.SampleID {
		return
	}
	batch.Qualified = false
	batch.QualificationSample = uuid.Nil
	batch.QualificationRev = 0
	batch.UpdatedAt = now.UTC()
	if batch.Stage == model.StageQualified {
		batch.Stage = model.StageSettling
	}
	p.batches[task.BatchID] = batch
	line := p.lines[batch.LineID]
	line.Stage = batch.Stage
	line.UpdatedAt = now.UTC()
	p.lines[batch.LineID] = line
}

func (p *Plant) SamplingChain(id uuid.UUID) (model.SamplingTask, error) {
	task, ok := p.chains.Get(id)
	if !ok {
		return model.SamplingTask{}, errors.New("sampling chain is not found")
	}
	return task, nil
}

// PendingChains 待接收视图。
func (p *Plant) PendingChains() []model.SamplingTask { return p.chains.PendingReceipt() }

// ArchivedChains 已归档视图。
func (p *Plant) ArchivedChains() []model.SamplingTask { return p.chains.Archived() }

// SamplingChains 全部链条。
func (p *Plant) SamplingChains() []model.SamplingTask { return p.chains.All() }
