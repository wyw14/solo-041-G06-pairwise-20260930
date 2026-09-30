package model

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ChainStatus 是人工采样送检链条所处的阶段。
type ChainStatus string

const (
	// ChainCollected 已采样、待封样。
	ChainCollected ChainStatus = "collected"
	// ChainSealed 已封样、在途、待实验室接收。
	ChainSealed ChainStatus = "sealed"
	// ChainReceived 实验室已接收，可匹配化验结果。
	ChainReceived ChainStatus = "received"
	// ChainArchived 结果已归档，链条终结。
	ChainArchived ChainStatus = "archived"
	// ChainLost 样本在送检途中丢失，不得继续流转。
	ChainLost ChainStatus = "lost"
	// ChainRevoked 样本被撤销，不得开出/维持出水许可。
	ChainRevoked ChainStatus = "revoked"
)

// TerminalChainStatus 判断链条是否已终结（不可再交接）。
func TerminalChainStatus(status ChainStatus) bool {
	return status == ChainArchived || status == ChainLost || status == ChainRevoked
}

// Transfer 记录一次交接痕迹：谁、在什么时间、把样本交到了哪个环节。
type Transfer struct {
	From       ChainStatus `json:"from"`
	To         ChainStatus `json:"to"`
	Actor      string      `json:"actor"`
	Note       string      `json:"note,omitempty"`
	OccurredAt time.Time   `json:"occurred_at"`
}

// SamplingTask 是连接传感器样本与化验批次的人工采样送检任务。
type SamplingTask struct {
	ID          uuid.UUID   `json:"id"`
	SampleID    uuid.UUID   `json:"sample_id"`
	LineID      LineID      `json:"line_id"`
	BasinID     string      `json:"basin_id"`
	BatchID     uuid.UUID   `json:"batch_id"`
	Kind        SampleKind  `json:"kind"`
	Sampler     string      `json:"sampler"`
	Status      ChainStatus `json:"status"`
	CollectedAt time.Time   `json:"collected_at"`
	SealedAt    *time.Time  `json:"sealed_at,omitempty"`
	SealedBy    string      `json:"sealed_by,omitempty"`
	ReceivedAt  *time.Time  `json:"received_at,omitempty"`
	ReceivedBy  string      `json:"received_by,omitempty"`
	ArchivedAt  *time.Time  `json:"archived_at,omitempty"`
	EndedAt     *time.Time  `json:"ended_at,omitempty"`
	Transfers   []Transfer  `json:"transfers"`
}

// NewSamplingTask 创建一条待封样的采样送检任务。sampleID 指向已入库的传感器样本，
// batchID 指向该样本所属的处理批次。
func NewSamplingTask(sampleID uuid.UUID, lineID LineID, basinID string, batchID uuid.UUID, kind SampleKind, sampler string, now time.Time) (SamplingTask, error) {
	sampler = strings.TrimSpace(sampler)
	if sampleID == uuid.Nil || batchID == uuid.Nil || lineID == "" || kind == "" || sampler == "" {
		return SamplingTask{}, errors.New("sampling task identity is incomplete")
	}
	return SamplingTask{
		ID:          uuid.New(),
		SampleID:    sampleID,
		LineID:      lineID,
		BasinID:     strings.TrimSpace(basinID),
		BatchID:     batchID,
		Kind:        kind,
		Sampler:     sampler,
		Status:      ChainCollected,
		CollectedAt: now.UTC(),
		Transfers: []Transfer{{
			From: "", To: ChainCollected, Actor: sampler,
			OccurredAt: now.UTC(),
		}},
	}, nil
}

// ValidForLab 报告该链条当前是否允许化验结果匹配其样本：
// 仅实验室已接收且未终结/作废的链条有效。
func (t SamplingTask) ValidForLab() bool {
	return t.Status == ChainReceived || t.Status == ChainArchived
}

// CanIssuePermit 报告该链条样本是否可作为出水许可依据：丢失或撤销样本一律不可。
func (t SamplingTask) CanIssuePermit() bool {
	return t.Status != ChainLost && t.Status != ChainRevoked
}

// moveTo 校验状态迁移并追加一条交接痕迹，返回更新后的任务。
func (t SamplingTask) moveTo(next ChainStatus, actor string, note string, now time.Time) (SamplingTask, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return t, errors.New("handover actor is required")
	}
	if t.Status == next {
		return t, errors.New("sampling task is already in status " + string(next))
	}
	if TerminalChainStatus(t.Status) {
		return t, errors.New("sampling task is already finalized")
	}
	allowed := map[ChainStatus]map[ChainStatus]bool{
		ChainCollected: {ChainSealed: true, ChainLost: true, ChainRevoked: true},
		ChainSealed:    {ChainReceived: true, ChainLost: true, ChainRevoked: true},
		ChainReceived:  {ChainArchived: true, ChainRevoked: true},
	}
	if !allowed[t.Status][next] {
		return t, errors.New("sampling task cannot move from " + string(t.Status) + " to " + string(next))
	}
	stamp := now.UTC()
	previous := t.Status
	t.Status = next
	t.Transfers = append(t.Transfers, Transfer{
		From: previous, To: next, Actor: actor, Note: strings.TrimSpace(note), OccurredAt: stamp,
	})
	switch next {
	case ChainSealed:
		t.SealedAt = &stamp
		t.SealedBy = actor
	case ChainReceived:
		t.ReceivedAt = &stamp
		t.ReceivedBy = actor
	case ChainArchived:
		t.ArchivedAt = &stamp
		t.EndedAt = &stamp
	case ChainLost, ChainRevoked:
		t.EndedAt = &stamp
	}
	return t, nil
}

// Seal 封样：采样者现场封口、加标识后准备送检。
func (t SamplingTask) Seal(sealedBy string, now time.Time) (SamplingTask, error) {
	return t.moveTo(ChainSealed, sealedBy, "", now)
}

// Receive 实验室签收：记录接收时间与接收人，链条自此可匹配化验结果。
func (t SamplingTask) Receive(receivedBy string, now time.Time) (SamplingTask, error) {
	return t.moveTo(ChainReceived, receivedBy, "", now)
}

// Archive 归档：化验完成后封存链条。
func (t SamplingTask) Archive(actor string, now time.Time) (SamplingTask, error) {
	return t.moveTo(ChainArchived, actor, "", now)
}

// MarkLost 标记样本在送检途中丢失。
func (t SamplingTask) MarkLost(actor, reason string, now time.Time) (SamplingTask, error) {
	return t.moveTo(ChainLost, actor, reason, now)
}

// Revoke 撤销样本（如污染、错采）。
func (t SamplingTask) Revoke(actor, reason string, now time.Time) (SamplingTask, error) {
	return t.moveTo(ChainRevoked, actor, reason, now)
}
