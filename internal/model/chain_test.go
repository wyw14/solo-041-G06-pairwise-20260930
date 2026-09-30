package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func testTask() SamplingTask {
	task, err := NewSamplingTask(uuid.New(), "line-1", "basin-1", uuid.New(), SampleCOD, "tech-a", time.Unix(100, 0).UTC())
	if err != nil {
		panic(err)
	}
	return task
}

func TestSamplingChainHappyPath(t *testing.T) {
	task := testTask()
	if task.Status != ChainCollected || task.Sampler != "tech-a" {
		t.Fatalf("unexpected initial task: %+v", task)
	}
	if task.ValidForLab() {
		t.Fatal("collected sample must not be valid for lab")
	}
	now := time.Unix(200, 0).UTC()
	var err error
	if task, err = task.Seal("tech-a", now); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if task, err = task.Receive("lab-b", now.Add(time.Minute)); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if !task.ValidForLab() {
		t.Fatal("received sample must be valid for lab")
	}
	if task, err = task.Archive("lab-b", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !task.ValidForLab() {
		t.Fatal("archived sample must remain valid for lab")
	}
	if len(task.Transfers) != 4 {
		t.Fatalf("expected 4 transfer traces, got %d", len(task.Transfers))
	}
	if task.ReceivedAt == nil || task.ArchivedAt == nil || task.SealedAt == nil {
		t.Fatal("sealed/received/archived timestamps must be recorded")
	}
}

func TestSamplingChainCannotSkipHandover(t *testing.T) {
	task := testTask()
	if _, err := task.Receive("lab-b", time.Now()); err == nil {
		t.Fatal("collected sample must not be received without sealing")
	}
	sealed, _ := task.Seal("tech-a", time.Now())
	if _, err := sealed.Archive("lab-b", time.Now()); err == nil {
		t.Fatal("sealed sample must not be archived before receipt")
	}
}

func TestLostAndRevokedSamplesAreInvalid(t *testing.T) {
	cases := map[string]func(SamplingTask) (SamplingTask, error){
		"lost": func(s SamplingTask) (SamplingTask, error) {
			return s.MarkLost("courier", "box missing", time.Now())
		},
		"revoked": func(s SamplingTask) (SamplingTask, error) {
			s, _ = s.Seal("tech-a", time.Now())
			s, _ = s.Receive("lab-b", time.Now())
			return s.Revoke("lab-b", "contaminated", time.Now())
		},
	}
	for name, finalize := range cases {
		t.Run(name, func(t *testing.T) {
			task, err := finalize(testTask())
			if err != nil {
				t.Fatalf("finalize: %v", err)
			}
			if task.CanIssuePermit() {
				t.Fatalf("%s sample must not support a permit", name)
			}
			if task.ValidForLab() {
				t.Fatalf("%s sample must not accept lab results", name)
			}
			if _, err := task.Archive("x", time.Now()); err == nil {
				t.Fatalf("%s chain must be terminal", name)
			}
		})
	}
}

func TestNewSamplingTaskRequiresIdentity(t *testing.T) {
	if _, err := NewSamplingTask(uuid.Nil, "line-1", "b", uuid.New(), SampleCOD, "x", time.Now()); err == nil {
		t.Fatal("missing sample must be rejected")
	}
	if _, err := NewSamplingTask(uuid.New(), "line-1", "b", uuid.New(), SampleCOD, "  ", time.Now()); err == nil {
		t.Fatal("missing sampler must be rejected")
	}
}
