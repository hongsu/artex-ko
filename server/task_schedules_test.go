package server

import (
	"bytes"
	"encoding/json"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/schedule"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestScheduleAPIValidationAndTimezone(t *testing.T) {
	m, e := NewManager(t.TempDir(), "")
	if e != nil {
		t.Skipf("postgres unavailable: %v", e)
	}
	defer m.Close()
	s := newAdmissionTestServer(m, nil)
	t.Setenv("TZ", "UTC")
	old, _, _ := m.pg.GetSetting("timezone_override")
	defer m.pg.SetSetting("timezone_override", old)
	for _, body := range []string{`{"timezone_override":"Invalid/Zone"}`, `{"timezone_override":"Local"}`} {
		w := httptest.NewRecorder()
		s.putSettings(w, httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body)))
		if w.Code != 400 {
			t.Fatalf("invalid timezone accepted %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.putSettings(w, httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(`{"timezone_override":"Asia/Seoul"}`)))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	payload := s.settingsPayload()
	if payload["timezone_effective"] != "Asia/Seoul" || payload["timezone_env"] != "UTC" {
		t.Fatalf("wrong timezone fields: effective=%v env=%v", payload["timezone_effective"], payload["timezone_env"])
	}
	req := taskScheduleRequest{Spec: schedule.Spec{Kind: "daily", Time: "09:00"}, Task: &createTaskReq{Description: "test only", Goal: "no network"}}
	raw, _ := json.Marshal(req)
	w = httptest.NewRecorder()
	s.createTaskSchedule(w, httptest.NewRequest("POST", "/api/task-schedules", bytes.NewReader(raw)))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var saved db.TaskSchedule
	if e := json.Unmarshal(w.Body.Bytes(), &saved); e != nil {
		t.Fatal(e)
	}
	defer m.pg.DeleteTaskSchedule(saved.ID)
	if saved.Spec.Timezone != "Asia/Seoul" || saved.NextRunAt == nil || !saved.NextRunAt.After(time.Now()) {
		t.Fatalf("%+v", saved)
	}
	m.pg.SetSetting("timezone_override", "UTC")
	unchanged, _ := m.pg.GetTaskSchedule(saved.ID)
	if unchanged.Spec.Timezone != "Asia/Seoul" {
		t.Fatal("default edit changed existing reservation")
	}
}
func TestScheduledTickCreatesQueuedTaskWithoutModelCall(t *testing.T) {
	m, e := NewManager(t.TempDir(), "")
	if e != nil {
		t.Skipf("postgres unavailable: %v", e)
	}
	defer m.Close()
	s := newAdmissionTestServer(m, func(*Task) bool { return false })
	now := time.Now().UTC()
	at := now.Add(-time.Minute)
	row := &db.TaskSchedule{Spec: schedule.Spec{Kind: "once", Date: at.Format("2006-01-02"), Time: at.Format("15:04"), Timezone: "UTC"}, Task: db.ScheduledTask{Description: "local fixture", Goal: "no network"}, Enabled: true, NextRunAt: &at}
	if e := m.pg.SaveTaskSchedule(row); e != nil {
		t.Fatal(e)
	}
	defer m.pg.DeleteTaskSchedule(row.ID)
	s.tickTaskSchedules(now, false)
	runs, _ := m.pg.TaskScheduleRuns(row.ID)
	if len(runs) != 1 || runs[0].TaskID == nil {
		t.Fatalf("%+v", runs)
	}
	defer m.pg.DeleteTask(*runs[0].TaskID)
	task, _ := m.pg.GetTask(*runs[0].TaskID)
	if !task.Queued || task.FirstRunAt != nil {
		t.Fatalf("scheduler must queue, not call model: %+v", task)
	}
}

func TestScheduleTickDoesNotHydrateUnrelatedTasks(t *testing.T) {
	m, e := NewManager(t.TempDir(), "")
	if e != nil {
		t.Skipf("postgres unavailable: %v", e)
	}
	defer m.Close()
	pt, e := m.pg.CreateTask("unrelated persisted task", "local fixture", nil, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer m.pg.DeleteTask(pt.ID)
	m.pg.SetPaused(pt.ID, true)
	s := newAdmissionTestServer(m, nil)
	s.tickTaskSchedules(time.Now(), true)
	if _, ok := m.Task(strconv.FormatInt(pt.ID, 10)); ok {
		t.Fatal("scheduler must not steal ordinary runtime restoration")
	}
}

func TestScheduledRestoreDoesNotRepeatGoalDecomposition(t *testing.T) {
	m, e := NewManager(t.TempDir(), "")
	if e != nil {
		t.Skipf("postgres unavailable: %v", e)
	}
	defer m.Close()
	now := time.Now().UTC()
	row := &db.TaskSchedule{Spec: schedule.Spec{Kind: "once", Date: now.Format("2006-01-02"), Time: now.Format("15:04"), Timezone: "UTC"}, Task: db.ScheduledTask{Description: "local fixture", Goal: "local only"}, Enabled: true, NextRunAt: &now}
	if e = m.pg.SaveTaskSchedule(row); e != nil {
		t.Fatal(e)
	}
	defer m.pg.DeleteTaskSchedule(row.ID)
	if e = m.pg.FireTaskSchedule(row.ID, now, false); e != nil {
		t.Fatal(e)
	}
	runs, _ := m.pg.TaskScheduleRuns(row.ID)
	if len(runs) != 1 || runs[0].TaskID == nil {
		t.Fatal("missing task")
	}
	id := *runs[0].TaskID
	defer m.pg.DeleteTask(id)
	// Simulate crash after admission cleared queued but before decomposition.
	m.pg.Exec(`UPDATE tasks SET queued=false,queue_mode='' WHERE id=$1`, id)
	pt, _ := m.pg.GetTask(id)
	task := taskFromPG(pt, m.pg.Exploration(pt.ExplorationID), m.interceptor)
	s := newAdmissionTestServer(m, nil)
	if !s.shouldBootstrapScheduledTask(task) {
		t.Fatal("interrupted bootstrap not recovered")
	}
	if _, e = task.Store.AddGoal(map[string]any{"summary": "existing goal"}, "planner"); e != nil {
		t.Fatal(e)
	}
	// Normal running tasks still have status=created and can have FirstRunAt=0.
	if s.shouldBootstrapScheduledTask(task) {
		t.Fatal("existing goals would be decomposed twice on restart")
	}
}
