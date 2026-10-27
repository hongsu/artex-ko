package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Autumn-27/artex/schedule"
)

type ScheduledTask struct {
	TaskCreateOptions
	Description     string `json:"description"`
	Goal            string `json:"goal"`
	SeedFirstIntent bool   `json:"seed_first_intent"`
}
type TaskSchedule struct {
	Revision  int64         `json:"revision"`
	ID        int64         `json:"id"`
	Spec      schedule.Spec `json:"spec"`
	Task      ScheduledTask `json:"task"`
	Enabled   bool          `json:"enabled"`
	NextRunAt *time.Time    `json:"next_run_at"`
}
type TaskScheduleRun struct {
	ID          int64     `json:"id"`
	ScheduledAt time.Time `json:"scheduled_at"`
	TaskID      *int64    `json:"task_id"`
	Outcome     string    `json:"outcome"`
	TaskStatus  string    `json:"task_status,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

var ErrScheduleConflict = errors.New("schedule changed; reload before saving")

var ErrScheduleNotFound = errors.New("schedule not found")

const scheduleCols = `id,spec,task,enabled,next_run_at,revision`

func scanTaskSchedule(row interface{ Scan(...any) error }) (*TaskSchedule, error) {
	var s TaskSchedule
	var spec, task []byte
	if e := row.Scan(&s.ID, &spec, &task, &s.Enabled, &s.NextRunAt, &s.Revision); e != nil {
		return nil, e
	}
	if e := json.Unmarshal(spec, &s.Spec); e != nil {
		return nil, e
	}
	if e := json.Unmarshal(task, &s.Task); e != nil {
		return nil, e
	}
	return &s, nil
}
func (d *DB) SaveTaskSchedule(s *TaskSchedule) error {
	if e := s.Spec.Validate(); e != nil {
		return e
	}
	spec, e := json.Marshal(s.Spec)
	if e != nil {
		return e
	}
	task, e := json.Marshal(s.Task)
	if e != nil {
		return e
	}
	if s.ID == 0 {
		return d.QueryRow(`INSERT INTO task_schedules(spec,task,enabled,next_run_at) VALUES($1,$2,$3,$4) RETURNING id`, spec, task, s.Enabled, s.NextRunAt).Scan(&s.ID)
	}
	err := d.QueryRow(`UPDATE task_schedules SET spec=$2,task=$3,enabled=$4,next_run_at=$5,updated_at=now(),revision=revision+1 WHERE id=$1 AND revision=$6 AND deleted_at IS NULL RETURNING revision`, s.ID, spec, task, s.Enabled, s.NextRunAt, s.Revision).Scan(&s.Revision)
	if err == sql.ErrNoRows {
		var exists bool
		if e := d.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_schedules WHERE id=$1 AND deleted_at IS NULL)`, s.ID).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			return ErrScheduleNotFound
		}
		return ErrScheduleConflict
	}
	return err
}
func (d *DB) GetTaskSchedule(id int64) (*TaskSchedule, error) {
	s, e := scanTaskSchedule(d.QueryRow(`SELECT `+scheduleCols+` FROM task_schedules WHERE id=$1 AND deleted_at IS NULL`, id))
	if e == sql.ErrNoRows {
		return nil, ErrScheduleNotFound
	}
	return s, e
}
func (d *DB) ListTaskSchedules() ([]*TaskSchedule, error) {
	rows, e := d.Query(`SELECT ` + scheduleCols + ` FROM task_schedules WHERE deleted_at IS NULL ORDER BY id DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []*TaskSchedule{}
	for rows.Next() {
		s, e := scanTaskSchedule(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (d *DB) DeleteTaskSchedule(id int64) error {
	res, e := d.Exec(`UPDATE task_schedules SET enabled=false,next_run_at=NULL,deleted_at=now() WHERE id=$1 AND deleted_at IS NULL`, id)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e == nil && n == 0 {
		return ErrScheduleNotFound
	}
	return e
}
func (d *DB) TaskScheduleRuns(id int64) ([]TaskScheduleRun, error) {
	rows, e := d.Query(`SELECT r.id,r.scheduled_at,r.task_id,r.outcome,COALESCE(t.status,''),r.created_at FROM task_schedule_runs r LEFT JOIN tasks t ON t.id=r.task_id WHERE r.schedule_id=$1 ORDER BY r.id DESC LIMIT 100`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []TaskScheduleRun{}
	for rows.Next() {
		var r TaskScheduleRun
		if e := rows.Scan(&r.ID, &r.ScheduledAt, &r.TaskID, &r.Outcome, &r.TaskStatus, &r.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FireTaskSchedule locks the schedule against ticks, edits and cancellation. The
// task, durable bootstrap queue entry, history and next date commit together.
// startup skips missed recurring dates but catches up one-shot reservations.
func (d *DB) FireTaskSchedule(id int64, now time.Time, startup bool) error {
	tx, e := d.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	s, e := scanTaskSchedule(tx.QueryRow(`SELECT `+scheduleCols+` FROM task_schedules WHERE id=$1 AND deleted_at IS NULL FOR UPDATE SKIP LOCKED`, id))
	if e == sql.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	if !s.Enabled || s.NextRunAt == nil || s.NextRunAt.After(now) {
		return nil
	}
	due := *s.NextRunAt
	// A date can be revisited by an edit. Never create a second task for that date.
	var exists bool
	if e := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_schedule_runs WHERE schedule_id=$1 AND scheduled_at=$2)`, id, due).Scan(&exists); e != nil {
		return e
	}
	next, e := s.Spec.Next(now)
	if e != nil {
		return e
	}
	var nextPtr *time.Time
	if !next.IsZero() {
		nextPtr = &next
	}
	if _, e = tx.Exec(`UPDATE task_schedules SET next_run_at=$2,enabled=$3,updated_at=now(),revision=revision+1 WHERE id=$1`, id, nextPtr, nextPtr != nil); e != nil {
		return e
	}
	if exists {
		return tx.Commit()
	}
	outcome := "created"
	var taskID *int64
	var overlap bool
	if e := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_schedule_runs r JOIN tasks t ON t.id=r.task_id WHERE r.schedule_id=$1 AND t.status NOT IN ('done','failed','timeout') AND t.deleted_at IS NULL AND t.archived_at IS NULL)`, id).Scan(&overlap); e != nil {
		return e
	}
	switch {
	case startup && s.Spec.Kind != "once":
		outcome = "skipped_missed"
	case overlap:
		outcome = "skipped_overlap"
	default:
		if _, e = tx.Exec(`SAVEPOINT scheduled_task`); e != nil {
			return e
		}
		task, createErr := createTaskWithOptionsTx(tx, s.Task.Description, s.Task.Goal, s.Task.TaskCreateOptions)
		if createErr != nil {
			if _, e = tx.Exec(`ROLLBACK TO SAVEPOINT scheduled_task`); e != nil {
				return e
			}
			outcome = "failed"
		} else {
			taskID = &task.ID
			if _, e = tx.Exec(`UPDATE tasks SET queued=true,queued_at=now(),queue_mode='bootstrap' WHERE id=$1`, task.ID); e != nil {
				return e
			}
			// Seed intent belongs to the same transaction, so recovery cannot duplicate it.
			if s.Task.SeedFirstIntent {
				payload, _ := json.Marshal(map[string]string{"summary": "完成任务目标：" + s.Task.Goal + "（任务：" + s.Task.Description + "）"})
				var intent int64
				if e = tx.QueryRow(`INSERT INTO exploration_nodes(exploration_id,kind,payload,priority,state,origin) VALUES($1,'intent',$2,8,'open','seed') RETURNING id`, task.ExplorationID, payload).Scan(&intent); e != nil {
					return e
				}
				if _, e = tx.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) SELECT $1,id,$2,$3 FROM exploration_nodes WHERE exploration_id=$1 AND state='origin' AND kind='fact'`, task.ExplorationID, RelDerivedFrom, intent); e != nil {
					return e
				}
			}
		}
	}
	if _, e = tx.Exec(`INSERT INTO task_schedule_runs(schedule_id,scheduled_at,task_id,outcome) VALUES($1,$2,$3,$4)`, id, due, taskID, outcome); e != nil {
		return e
	}
	return tx.Commit()
}

// PrepareScheduledTask runs the idempotent asset seed before bootstrap, including
// recovery after the task commit. The lock also serializes concurrent preparers.
func (d *DB) PrepareScheduledTask(taskID int64, seed func()) error {
	tx, e := d.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id int64
	var prepared bool
	e = tx.QueryRow(`SELECT id,prepared FROM task_schedule_runs WHERE task_id=$1 FOR UPDATE`, taskID).Scan(&id, &prepared)
	if e == sql.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	if !prepared {
		seed()
		if _, e = tx.Exec(`UPDATE task_schedule_runs SET prepared=true WHERE id=$1`, id); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (d *DB) IsScheduledTask(id int64) bool {
	var yes bool
	_ = d.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_schedule_runs WHERE task_id=$1)`, id).Scan(&yes)
	return yes
}

func (d *DB) PendingScheduledTaskIDs() ([]int64, error) {
	rows, e := d.Query(`SELECT t.id FROM task_schedule_runs r JOIN tasks t ON t.id=r.task_id WHERE t.status='created' AND t.queued AND t.deleted_at IS NULL AND t.archived_at IS NULL`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if e := rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
