package chain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

func newTask(sampleID, batchID uuid.UUID, now time.Time) model.SamplingTask {
	task, err := model.NewSamplingTask(sampleID, "line-1", "basin-1", batchID, model.SampleCOD, "tech-a", now)
	if err != nil {
		panic(err)
	}
	return task
}

func TestBookViewsAndSingleChainPerSample(t *testing.T) {
	book := NewBook()
	base := time.Unix(1000, 0).UTC()

	a, err := book.Create(newTask(uuid.New(), uuid.New(), base))
	if err != nil {
		t.Fatal(err)
	}
	b, err := book.Create(newTask(uuid.New(), uuid.New(), base.Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}

	// 同一样本不得登记两条链条。
	if _, err := book.Create(newTask(a.SampleID, uuid.New(), base)); err == nil {
		t.Fatal("a sample may only have one sampling chain")
	}

	if len(book.PendingReceipt()) != 2 {
		t.Fatalf("both collected chains must be pending receipt, got %d", len(book.PendingReceipt()))
	}
	if len(book.Archived()) != 0 {
		t.Fatal("nothing archived yet")
	}

	if _, err := book.Seal(a.ID, "tech-a", base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Receive(a.ID, "lab-b", base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Archive(a.ID, "lab-b", base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// a 已归档，b 仍在待接收。
	if got := book.Archived(); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("archived view mismatch: %+v", got)
	}
	if got := book.PendingReceipt(); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("pending view mismatch: %+v", got)
	}

	task, ok := book.ValidForLab(a.SampleID)
	if !ok || task.ID != a.ID {
		t.Fatal("archived chain must remain valid for lab")
	}
	if _, ok := book.ValidForLab(b.SampleID); ok {
		t.Fatal("collected (unreceived) chain must not be valid for lab")
	}
}

func TestBookLostChainLeavesViewsAndInvalidForLab(t *testing.T) {
	book := NewBook()
	now := time.Unix(2000, 0).UTC()
	task, err := book.Create(newTask(uuid.New(), uuid.New(), now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.Seal(task.ID, "tech-a", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := book.MarkLost(task.ID, "courier", "missing", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(book.PendingReceipt()) != 0 || len(book.Archived()) != 0 {
		t.Fatal("lost chain must appear in neither pending nor archived view")
	}
	if _, ok := book.ValidForLab(task.SampleID); ok {
		t.Fatal("lost chain must never be valid for lab")
	}
}
