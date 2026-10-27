package db

import (
	"github.com/Autumn-27/artex/schedule"
	"sync"
	"testing"
	"time"
)

func scheduleTestDB(t *testing.T) *DB {
	t.Helper()
	d, e := Open(testDSN(t))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
func testSchedule(t *testing.T, d *DB, kind string, at time.Time) *TaskSchedule {
	t.Helper()
	s := &TaskSchedule{Spec: schedule.Spec{Kind: kind, Date: at.Format("2006-01-02"), Time: at.Format("15:04"), Timezone: "UTC"}, Task: ScheduledTask{Description: "schedule unit test", Goal: "no network"}, Enabled: true, NextRunAt: &at}
	if e := d.SaveTaskSchedule(s); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Exec("DELETE FROM task_schedules WHERE id=$1", s.ID) })
	return s
}
func TestScheduleConcurrentTickAndOverlap(t *testing.T) {
	d := scheduleTestDB(t)
	now := time.Now().UTC().Truncate(time.Minute)
	s := testSchedule(t, d, "daily", now)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := d.FireTaskSchedule(s.ID, now, false); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	runs, e := d.TaskScheduleRuns(s.ID)
	if e != nil || len(runs) != 1 || runs[0].TaskID == nil {
		t.Fatalf("%+v %v", runs, e)
	}
	task, e := d.GetTask(*runs[0].TaskID)
	if e != nil || !task.Queued || task.QueueMode != "bootstrap" {
		t.Fatalf("task not durably queued: %+v %v", task, e)
	}
	t.Cleanup(func() { d.DeleteTask(task.ID) })
	if e := d.FireTaskSchedule(s.ID, now.AddDate(0, 0, 1), false); e != nil {
		t.Fatal(e)
	}
	runs, _ = d.TaskScheduleRuns(s.ID)
	if len(runs) != 2 || runs[0].Outcome != "skipped_overlap" {
		t.Fatalf("overlap not skipped: %+v", runs)
	}
}
func TestScheduleRecoveryAndDisabled(t *testing.T) {
	d := scheduleTestDB(t)
	now := time.Now().UTC().Truncate(time.Minute)
	s := testSchedule(t, d, "daily", now.Add(-48*time.Hour))
	if e := d.FireTaskSchedule(s.ID, now, true); e != nil {
		t.Fatal(e)
	}
	runs, _ := d.TaskScheduleRuns(s.ID)
	if len(runs) != 1 || runs[0].Outcome != "skipped_missed" || runs[0].TaskID != nil {
		t.Fatalf("%+v", runs)
	}
	once := testSchedule(t, d, "once", now.Add(-time.Hour))
	if e := d.FireTaskSchedule(once.ID, now, true); e != nil {
		t.Fatal(e)
	}
	runs, _ = d.TaskScheduleRuns(once.ID)
	if len(runs) != 1 || runs[0].TaskID == nil {
		t.Fatalf("%+v", runs)
	}
	createdID := *runs[0].TaskID
	t.Cleanup(func() { d.DeleteTask(createdID) })
	got, _ := d.GetTaskSchedule(once.ID)
	if got.Enabled || got.NextRunAt != nil {
		t.Fatal("once not consumed")
	}
	s, _ = d.GetTaskSchedule(s.ID)
	s.Enabled = false
	if e := d.SaveTaskSchedule(s); e != nil {
		t.Fatal(e)
	}
	if e := d.FireTaskSchedule(s.ID, now.AddDate(0, 0, 3), false); e != nil {
		t.Fatal(e)
	}
	runs, _ = d.TaskScheduleRuns(s.ID)
	if len(runs) != 1 {
		t.Fatal("disabled schedule fired")
	}
}
func TestScheduleCreationFailureIsRecorded(t *testing.T) {
	d := scheduleTestDB(t)
	now := time.Now().UTC()
	s := testSchedule(t, d, "once", now)
	bad := int64(-123)
	s.Task.CategoryID = &bad
	if e := d.SaveTaskSchedule(s); e != nil {
		t.Fatal(e)
	}
	if e := d.FireTaskSchedule(s.ID, now, false); e != nil {
		t.Fatal(e)
	}
	runs, _ := d.TaskScheduleRuns(s.ID)
	if len(runs) != 1 || runs[0].Outcome != "failed" || runs[0].TaskID != nil {
		t.Fatalf("%+v", runs)
	}
}

func TestScheduleCancelPreservesCreatedTaskAndPreparation(t *testing.T) {
	d := scheduleTestDB(t)
	now := time.Now().UTC()
	s := testSchedule(t, d, "once", now)
	s.Task.SeedFirstIntent = true
	if e := d.SaveTaskSchedule(s); e != nil {
		t.Fatal(e)
	}
	if e := d.FireTaskSchedule(s.ID, now, false); e != nil {
		t.Fatal(e)
	}
	runs, _ := d.TaskScheduleRuns(s.ID)
	if len(runs) != 1 || runs[0].TaskID == nil {
		t.Fatalf("%+v", runs)
	}
	id := *runs[0].TaskID
	t.Cleanup(func() { d.DeleteTask(id) })
	if e := d.DeleteTaskSchedule(s.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := d.GetTaskSchedule(s.ID); e != ErrScheduleNotFound {
		t.Fatalf("canceled schedule remains visible: %v", e)
	}
	task, _ := d.GetTask(id)
	if task == nil || !task.Queued {
		t.Fatal("cancel changed created task")
	}
	calls := 0
	for i := 0; i < 2; i++ {
		if e := d.PrepareScheduledTask(id, func() { calls++ }); e != nil {
			t.Fatal(e)
		}
	}
	if calls != 1 {
		t.Fatalf("seed repeated %d", calls)
	}
	var intents int
	if e := d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='intent' AND origin='seed'`, task.ExplorationID).Scan(&intents); e != nil || intents != 1 {
		t.Fatalf("seed intents %d %v", intents, e)
	}
	if e := d.SaveTaskSchedule(s); e != ErrScheduleNotFound {
		t.Fatalf("edit resurrected canceled row: %v", e)
	}
}

func TestScheduleConcurrentEditCannotOverwrite(t *testing.T) {
	d := scheduleTestDB(t)
	now := time.Now().UTC()
	s := testSchedule(t, d, "daily", now)
	stale, e := d.GetTaskSchedule(s.ID)
	if e != nil {
		t.Fatal(e)
	}
	s.Spec.Time = "10:30"
	if e := d.SaveTaskSchedule(s); e != nil {
		t.Fatal(e)
	}
	stale.Enabled = false
	if e := d.SaveTaskSchedule(stale); e != ErrScheduleConflict {
		t.Fatalf("stale update accepted: %v", e)
	}
	got, _ := d.GetTaskSchedule(s.ID)
	if got.Spec.Time != "10:30" {
		t.Fatal("stale pause reverted schedule")
	}
}
