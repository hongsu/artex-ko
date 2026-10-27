# Task schedules implementation plan

> Execute with superpowers:executing-plans. User authorized implementation and a PR in hongsu's fork.

Goal: one-time, daily and weekly task scheduling with TZ/environment + system override and existing ARTEX UX.

Constraints: preserve existing Codex OAuth WIP; no production DB migration, container replacement or deployment. PR contains scheduling changes only. Use isolated PostgreSQL for tests. No credentials in artifacts.

Decisions: override > valid TZ > Asia/Seoul. Snapshot zone per schedule. Daily/weekly schedules use local wall time; nonexistent DST time is skipped, ambiguous time runs once (earlier occurrence). Once missed runs after recovery; recurring missed occurrences are skipped on scheduler startup, normal ticks tolerate delay. Previous nonterminal run causes skip. A durable task/outbox record is committed atomically with schedule advancement. Existing admission controls remain authoritative.

- [x] Task 1: schedule calculation + validation and timezone precedence tests; implement `schedule.Spec`, `Next`, `ResolveTimezone`.
- [x] Task 2: DB schedule/run tables, CRUD and row-locked due processing; atomically create task and durable pending delivery; integration tests for concurrent ticks, edit/delete ordering, rollback and overlap.
- [x] Task 3: authenticated schedule CRUD/history/preview and timezone APIs; reuse task validation; scheduler startup missed-run policy and pending dispatch; server tests.
- [x] Task 4: reusable schedule controls in existing task sheet, schedule management/history on tasks page, timezone card in system settings, five locales; UI tests/typecheck/build.
- [x] Task 5: regression suite, fresh review, scheduling-only patch in publication worktree, fork and PR preparation. No live deployment.

Pre-flight: DB consumes normalized task options and schedule.Spec; API returns schedule records, frontend sends existing create-task payload plus spec. Existing queued task admission is reused after durable dispatch.
Ruling: user explicitly requested execution; no further plan approval needed. Existing OAuth edits stay local and are excluded from scheduling PR via a baseline snapshot.

## Verification record

- Calendar and timezone tests: pass (including DST gap/fold).
- DB integration tests: full db package passed against a fresh isolated PostgreSQL database. A prior reused fixture DB failed TestCompanyScopeAttribution; clean isolation resolved it.
- Server integration tests: full server package passed against its own fresh isolated PostgreSQL database.
- Scheduling-only publication tree: `go test ./...` passed with DB-dependent cases skipped; explicit DB/server integration and targeted race runs passed separately.
- `go build -buildvcs=false ./...` and `go vet ./agent/ ./schedule/ ./db/ ./server/` passed. Plain build hit this host's ancestor .git/worktree discovery issue; no product workaround was committed.
- Frontend: 14 tests passed, five-locale key parity passed, TypeScript and Korean static build passed. Biome check exits successfully with existing warnings/info.
- Browser smoke against locally served static export: timezone override, weekly creation payload, pause, and history task link passed with mocked API responses. No model calls or target scans performed by this smoke.
- Independent review: runtime restoration ordering, scheduled-only hydration, revision CAS and graph-aware bootstrap recovery verified after fixes.
- Publication target: hongsu/artex-ko, feat/task-schedules. OAuth WIP and local secrets excluded. No production DB changes or deployment.
