package process

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

func newTestPlant(t *testing.T) *Plant {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	clock := func() time.Time { return now }
	plant, err := NewPlant(t.TempDir(), []model.LineID{"line-1"}, clock)
	if err != nil {
		t.Fatalf("new plant: %v", err)
	}
	return plant
}

func seedBatchAndSample(t *testing.T, plant *Plant, seq uint64) (model.Batch, model.Sample) {
	t.Helper()
	ctx := context.Background()
	batch, err := plant.StartBatch(ctx, "line-1", 760, "north-intake")
	if err != nil {
		t.Fatalf("start batch: %v", err)
	}
	sample := model.NewSample("line-1", "basin-1", model.SampleCOD, seq, 30, "mg/L", plant.Clock())
	if err := plant.ReceiveSample(ctx, sample); err != nil {
		t.Fatalf("receive sample: %v", err)
	}
	return batch, sample
}

func labPayload(t *testing.T, sample model.Sample, batchID uuid.UUID, revision uint64) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"sample_id": sample.ID.String(),
		"line_id":   string(sample.LineID),
		"batch_id":  batchID.String(),
		"revision":  revision,
		"cod":       30,
		"ammonia":   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestLabResultRequiresReceivedChain(t *testing.T) {
	plant := newTestPlant(t)
	ctx := context.Background()
	batch, sample := seedBatchAndSample(t, plant, 1)

	// 没有链条：结果一律拒绝。
	if _, err := plant.SubmitLabPayload(ctx, labPayload(t, sample, batch.ID, 1)); err == nil {
		t.Fatal("lab result without sampling chain must be rejected")
	}

	task, err := plant.CreateSamplingChain(sample.ID, uuid.Nil, "tech-a")
	if err != nil {
		t.Fatalf("create chain: %v", err)
	}
	// 已采样、未封样：仍拒绝。
	if _, err := plant.SubmitLabPayload(ctx, labPayload(t, sample, batch.ID, 1)); err == nil {
		t.Fatal("collected (unsealed) chain must not accept lab results")
	}
	if _, err := plant.SealSamplingChain(task.ID, "tech-a"); err != nil {
		t.Fatalf("seal: %v", err)
	}
	// 已封样、实验室尚未接收：仍拒绝。
	if _, err := plant.SubmitLabPayload(ctx, labPayload(t, sample, batch.ID, 1)); err == nil {
		t.Fatal("sealed but unreceived chain must not accept lab results")
	}
	if _, err := plant.ReceiveSamplingChain(task.ID, "lab-b"); err != nil {
		t.Fatalf("receive: %v", err)
	}
	// 接收后：结果可匹配，并据此开出有效许可。
	outcome, err := plant.SubmitLabPayload(ctx, labPayload(t, sample, batch.ID, 1))
	if err != nil {
		t.Fatalf("submit after receipt: %v", err)
	}
	if outcome.Permit == nil || !outcome.Permit.Active {
		t.Fatalf("qualified received sample must open an active permit: %+v", outcome.Permit)
	}

	if len(plant.PendingChains()) != 0 {
		t.Fatal("received chain should leave the pending-receipt view")
	}
	if _, err := plant.ArchiveSamplingChain(task.ID, "lab-b"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if len(plant.ArchivedChains()) != 1 {
		t.Fatal("archived chain must appear in the archived view")
	}
}

func TestRevokedChainRevokesPermitAndRejectsResults(t *testing.T) {
	plant := newTestPlant(t)
	ctx := context.Background()
	batch, sample := seedBatchAndSample(t, plant, 1)
	task, _ := plant.CreateSamplingChain(sample.ID, uuid.Nil, "tech-a")
	_, _ = plant.SealSamplingChain(task.ID, "tech-a")
	_, _ = plant.ReceiveSamplingChain(task.ID, "lab-b")
	outcome, err := plant.SubmitLabPayload(ctx, labPayload(t, sample, batch.ID, 1))
	if err != nil || outcome.Permit == nil {
		t.Fatalf("expected permit before revocation: %v", err)
	}

	if _, err := plant.RevokeSamplingChain(task.ID, "qa", "mislabeled vial"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	permit, ok := plant.permits.Current(batch.ID)
	if !ok || permit.Active || permit.RevokedAt == nil {
		t.Fatalf("permit must be revoked when its sample is revoked: %+v", permit)
	}
	status, err := plant.Status("line-1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, valve := range status.Valves {
		if valve.Open {
			t.Fatal("discharge valve must be held closed after revocation")
		}
	}
	// 撤销后任何修订结果都不得再匹配该样本。
	if _, err := plant.SubmitLabPayload(ctx, labPayload(t, sample, batch.ID, 2)); err == nil {
		t.Fatal("revoked sample must never accept further lab results")
	}
}

func TestRevokingOneSampleDoesNotTouchPermitFromAnother(t *testing.T) {
	plant := newTestPlant(t)
	ctx := context.Background()
	batch, s1 := seedBatchAndSample(t, plant, 1)
	c1, _ := plant.CreateSamplingChain(s1.ID, uuid.Nil, "tech-a")
	_, _ = plant.SealSamplingChain(c1.ID, "tech-a")
	_, _ = plant.ReceiveSamplingChain(c1.ID, "lab-b")
	if _, err := plant.SubmitLabPayload(ctx, labPayload(t, s1, batch.ID, 1)); err != nil {
		t.Fatalf("permit from sample 1: %v", err)
	}

	// 第二条有效样本以更高修订号接管许可。
	s2 := model.NewSample("line-1", "basin-1", model.SampleCOD, 2, 28, "mg/L", plant.Clock())
	if err := plant.ReceiveSample(ctx, s2); err != nil {
		t.Fatalf("receive sample 2: %v", err)
	}
	c2, _ := plant.CreateSamplingChain(s2.ID, batch.ID, "tech-a")
	_, _ = plant.SealSamplingChain(c2.ID, "tech-a")
	_, _ = plant.ReceiveSamplingChain(c2.ID, "lab-b")
	out, err := plant.SubmitLabPayload(ctx, labPayload(t, s2, batch.ID, 2))
	if err != nil || out.Permit == nil || out.Permit.SampleID != s2.ID {
		t.Fatalf("permit should now be backed by sample 2: %v", err)
	}

	// 作废旧的样本 1：由样本 2 支撑的许可必须保持有效。
	if _, err := plant.RevokeSamplingChain(c1.ID, "qa", "old vial"); err != nil {
		t.Fatalf("revoke sample 1: %v", err)
	}
	permit, ok := plant.permits.Current(batch.ID)
	if !ok || !permit.Active || permit.SampleID != s2.ID {
		t.Fatalf("permit backed by still-valid sample 2 must remain active: %+v", permit)
	}
}

func TestLostChainCannotIssuePermit(t *testing.T) {
	plant := newTestPlant(t)
	ctx := context.Background()
	batch, sample := seedBatchAndSample(t, plant, 1)
	task, _ := plant.CreateSamplingChain(sample.ID, uuid.Nil, "tech-a")
	_, _ = plant.SealSamplingChain(task.ID, "tech-a")
	if _, err := plant.MarkSampleLost(task.ID, "courier", "cooler never arrived"); err != nil {
		t.Fatalf("mark lost: %v", err)
	}
	if _, err := plant.SubmitLabPayload(ctx, labPayload(t, sample, batch.ID, 1)); err == nil {
		t.Fatal("lost sample must not accept lab results or issue a permit")
	}
	if _, exists := plant.permits.Current(batch.ID); exists {
		t.Fatal("lost sample must never produce a discharge permit")
	}
}
