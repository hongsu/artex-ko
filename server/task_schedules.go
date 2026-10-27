package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/schedule"
)

func (s *Server) timezoneSettings() (env, override, effective, source string) {
	env = strings.TrimSpace(os.Getenv("TZ"))
	override, _, _ = s.m.pg.GetSetting("timezone_override")
	effective, source = schedule.ResolveTimezone(override, env)
	return
}

type taskScheduleRequest struct {
	Revision *int64         `json:"revision,omitempty"`
	Spec     schedule.Spec  `json:"spec"`
	Task     *createTaskReq `json:"task,omitempty"`
	Enabled  *bool          `json:"enabled,omitempty"`
}

func decodeSchedule(w http.ResponseWriter, r *http.Request) (taskScheduleRequest, bool) {
	var req taskScheduleRequest
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if e := dec.Decode(&req); e != nil {
		writeErr(w, 400, "invalid schedule request")
		return req, false
	}
	return req, true
}
func (s *Server) scheduleSpec(spec schedule.Spec) (schedule.Spec, error) {
	if spec.Timezone == "" {
		_, _, spec.Timezone, _ = s.timezoneSettings()
	}
	return spec, spec.Validate()
}
func scheduleError(w http.ResponseWriter, e error) {
	if errors.Is(e, db.ErrScheduleConflict) {
		writeErr(w, 409, e.Error())
	} else if errors.Is(e, db.ErrScheduleNotFound) {
		writeErr(w, 404, "schedule not found")
	} else {
		writeErr(w, 500, "schedule operation failed")
	}
}
func (s *Server) listTaskSchedules(w http.ResponseWriter, r *http.Request) {
	rows, e := s.m.pg.ListTaskSchedules()
	if e != nil {
		scheduleError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"schedules": rows})
}
func (s *Server) createTaskSchedule(w http.ResponseWriter, r *http.Request) {
	s.saveTaskSchedule(w, r, 0)
}
func (s *Server) updateTaskSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, 400, "invalid schedule id")
		return
	}
	s.saveTaskSchedule(w, r, id)
}
func (s *Server) saveTaskSchedule(w http.ResponseWriter, r *http.Request, id int64) {
	req, ok := decodeSchedule(w, r)
	if !ok {
		return
	}
	row := &db.TaskSchedule{Enabled: true}
	if id != 0 {
		var e error
		row, e = s.m.pg.GetTaskSchedule(id)
		if e != nil {
			scheduleError(w, e)
			return
		}
	}
	if req.Revision != nil && *req.Revision != row.Revision {
		scheduleError(w, db.ErrScheduleConflict)
		return
	}
	if req.Task != nil {
		opts, e := s.taskRequestOptions(req.Task)
		if e != nil {
			writeErr(w, 400, e.Error())
			return
		}
		// Validate references now as well as at firing time; no task is created here.
		if opts.CategoryID != nil {
			var exists bool
			e = s.m.pg.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_categories WHERE id=$1)`, opts.CategoryID).Scan(&exists)
			if e != nil {
				scheduleError(w, e)
				return
			}
			if !exists {
				writeErr(w, 400, errCreateTaskCategoryInvalid)
				return
			}
		}
		for _, company := range opts.CompanyIDs {
			var exists bool
			e = s.m.pg.QueryRow(`SELECT EXISTS(SELECT 1 FROM companies WHERE id=$1)`, company).Scan(&exists)
			if e != nil {
				scheduleError(w, e)
				return
			}
			if !exists {
				writeErr(w, 400, errCreateTaskCompanyInvalid)
				return
			}
		}
		row.Task = db.ScheduledTask{TaskCreateOptions: opts, Description: req.Task.Description, Goal: req.Task.Goal, SeedFirstIntent: req.Task.SeedFirstIntent != nil && *req.Task.SeedFirstIntent}
	} else if id == 0 {
		writeErr(w, 400, "task is required")
		return
	}
	if req.Enabled != nil {
		row.Enabled = *req.Enabled
	}
	// Empty spec is allowed for pause/resume; existing snapshot zone is preserved.
	if req.Spec.Kind != "" {
		var e error
		row.Spec, e = s.scheduleSpec(req.Spec)
		if e != nil {
			writeErr(w, 400, e.Error())
			return
		}
	}
	if e := row.Spec.Validate(); e != nil {
		writeErr(w, 400, e.Error())
		return
	}
	if row.Enabled || req.Spec.Kind != "" {
		next, e := row.Spec.Next(time.Now())
		if e != nil {
			writeErr(w, 400, e.Error())
			return
		}
		if next.IsZero() && row.Enabled {
			writeErr(w, 400, "one-time schedule must be in the future")
			return
		}
		row.NextRunAt = nil
		if !next.IsZero() {
			row.NextRunAt = &next
		}
	}
	if e := s.m.pg.SaveTaskSchedule(row); e != nil {
		scheduleError(w, e)
		return
	}
	code := 200
	if id == 0 {
		code = 201
	}
	writeJSON(w, code, row)
}
func (s *Server) previewTaskSchedule(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSchedule(w, r)
	if !ok {
		return
	}
	spec, e := s.scheduleSpec(req.Spec)
	if e != nil {
		writeErr(w, 400, e.Error())
		return
	}
	next, e := spec.Next(time.Now())
	if e != nil || next.IsZero() {
		writeErr(w, 400, "no future occurrence for this schedule")
		return
	}
	writeJSON(w, 200, map[string]any{"next_run_at": next, "timezone": spec.Timezone})
}
func (s *Server) deleteTaskSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, 400, "invalid schedule id")
		return
	}
	if e := s.m.pg.DeleteTaskSchedule(id); e != nil {
		scheduleError(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) taskScheduleHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, 400, "invalid schedule id")
		return
	}
	if _, e := s.m.pg.GetTaskSchedule(id); e != nil {
		scheduleError(w, e)
		return
	}
	runs, e := s.m.pg.TaskScheduleRuns(id)
	if e != nil {
		scheduleError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"runs": runs})
}
func (s *Server) tickTaskSchedules(now time.Time, startup bool) {
	rows, e := s.m.pg.ListTaskSchedules()
	if e != nil {
		log.Printf("[schedule] list failed: %v", e)
		return
	}
	for _, row := range rows {
		if row.Enabled && row.NextRunAt != nil && !row.NextRunAt.After(now) {
			if e := s.m.pg.FireTaskSchedule(row.ID, now, startup); e != nil {
				log.Printf("[schedule] fire %d failed: %v", row.ID, e)
			}
		}
	}
	// Rows are already durably queued. A crash before hydration is repaired on startup.
	ids, e := s.m.pg.PendingScheduledTaskIDs()
	if e != nil {
		log.Printf("[schedule] pending tasks: %v", e)
		return
	}
	for _, id := range ids {
		key := strconv.FormatInt(id, 10)
		if _, exists := s.m.Task(key); exists {
			continue
		}
		pt, e := s.m.pg.GetTask(id)
		if e != nil || pt == nil {
			continue
		}
		task := taskFromPG(pt, s.m.pg.Exploration(pt.ExplorationID), s.m.interceptor)
		s.m.mu.Lock()
		if s.m.tasks[key] == nil {
			s.m.tasks[key] = task
		}
		s.m.mu.Unlock()
	}
}

// Persisted status alone is insufficient: active tasks may still be "created".
// Reuse graph-aware admission so existing goals are never decomposed again.
func (s *Server) shouldBootstrapScheduledTask(t *Task) bool {
	state := t.lifecycleSnapshot()
	if state.Paused || state.Queued || isTerminalStatus(state.Status) {
		return false
	}
	id, _ := strconv.ParseInt(t.ID, 10, 64)
	return s.m.pg.IsScheduledTask(id) && s.resumeAdmissionMode(t) == "bootstrap"
}
