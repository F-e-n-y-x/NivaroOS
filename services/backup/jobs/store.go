package jobs

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store is the job store (spec §4): <data dir>/backup.db, a pure-Go
// SQLite database in WAL mode, plus the logs/, previews/, staging/ and
// secrets/ folders next to it. Only state transitions are written here;
// live progress stays in memory.
//
// Every write is one SQLite transaction, so a crash leaves either the old
// or the new row, never half of one. Open is idempotent: it creates what
// is missing, fixes folder modes and brings the schema up to
// SchemaVersion, so an upgraded install needs no manual step.
type Store struct {
	db      *gorm.DB
	dataDir string
	// mu serialises read-modify-write sequences (revision checks,
	// coalescing) inside this process; SQLite's own locking covers other
	// processes (the release-scheduled-tasks subcommand).
	mu sync.Mutex
}

// Store folders under the data dir. All are 0700: logs and previews name
// every file of every backed-up folder.
const (
	LogsDir     = "logs"
	PreviewsDir = "previews"
	StagingDir  = "staging" // archive restore temp (an engine allowed root)
	SecretsDir  = "secrets" // v1.1 restic keys
	DBFile      = "backup.db"
)

// errNoRecord is returned (wrapped) by lookups of a job or run that
// doesn't exist; test with isNoRecord.
var errNoRecord = errors.New("not found")

func isNoRecord(err error) bool { return errors.Is(err, errNoRecord) }

// OpenStore opens (creating when missing) the store under dataDir.
// Schema errors are fatal (spec §4): the caller must refuse to serve jobs
// and report store_unavailable instead of running on a broken schema.
func OpenStore(dataDir string) (*Store, error) {
	if dataDir == "" {
		return nil, errors.New("store: empty data dir")
	}
	if err := ensureDataDirs(dataDir); err != nil {
		return nil, err
	}
	dsn := filepath.Join(dataDir, DBFile) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", DBFile, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	// One connection: SQLite serialises writers anyway, and a single
	// connection keeps "read then write" inside one transaction free of
	// SQLITE_BUSY between our own connections.
	sqlDB.SetMaxOpenConns(1)
	s := &Store{db: db, dataDir: dataDir}
	if err := s.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	// The database names every backed-up folder: owner only, like the
	// folder it is in (SQLite creates it with the process umask).
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Chmod(filepath.Join(dataDir, DBFile+suffix), 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("store: chmod %s: %w", DBFile+suffix, err)
		}
	}
	return s, nil
}

// ensureDataDirs creates the data dir and its folders with mode 0700, and
// tightens the mode of ones that already exist (an older install, or the
// installer creating them with a different umask).
func ensureDataDirs(dataDir string) error {
	for _, d := range []string{"", LogsDir, PreviewsDir, StagingDir, SecretsDir} {
		p := filepath.Join(dataDir, d)
		if err := os.MkdirAll(p, 0o700); err != nil {
			return fmt.Errorf("store: create %s: %w", p, err)
		}
		if err := os.Chmod(p, 0o700); err != nil {
			return fmt.Errorf("store: chmod %s: %w", p, err)
		}
	}
	return nil
}

// schemaSteps are the data migrations after AutoMigrate, keyed by the
// version they bring the store to. Each must be idempotent: a crash after
// the step but before schema_version is written reruns it.
var schemaSteps = map[int]func(tx *gorm.DB) error{
	1: func(tx *gorm.DB) error { return nil }, // the first schema: tables only
}

func (s *Store) migrate() error {
	if err := s.db.AutoMigrate(&JobRow{}, &RunRow{}, &MetaRow{}); err != nil {
		return fmt.Errorf("store: schema: %w", err)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		cur := 0
		var row MetaRow
		err := tx.Where("key = ?", MetaSchemaVersion).Take(&row).Error
		switch {
		case err == nil:
			if cur, err = strconv.Atoi(row.Value); err != nil {
				return fmt.Errorf("store: bad schema_version %q", row.Value)
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
		default:
			return fmt.Errorf("store: read schema_version: %w", err)
		}
		if cur > SchemaVersion {
			return fmt.Errorf("store: schema version %d is newer than this build (%d); refusing to downgrade", cur, SchemaVersion)
		}
		for v := cur + 1; v <= SchemaVersion; v++ {
			if step := schemaSteps[v]; step != nil {
				if err := step(tx); err != nil {
					return fmt.Errorf("store: migrate to schema %d: %w", v, err)
				}
			}
		}
		return tx.Save(&MetaRow{Key: MetaSchemaVersion, Value: strconv.Itoa(SchemaVersion)}).Error
	})
}

// Close closes the database.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// DataDir is the folder the store lives in.
func (s *Store) DataDir() string { return s.dataDir }

// Ping checks the database answers (capabilities, health).
func (s *Store) Ping() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

// ---------------------------------------------------------------------
// IDs

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand never fails on Linux (getrandom); if it ever did,
		// continuing with a predictable id would be worse than stopping.
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return hex.EncodeToString(b)
}

// NewJobID is "bk_" + 12 hex.
func NewJobID() string { return "bk_" + randomHex(6) }

// NewUUID4 is a random RFC 4122 version 4 UUID (destination folder ids).
func NewUUID4() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var ulidMu sync.Mutex
var ulidLast struct {
	ms   uint64
	rand [10]byte
}

// NewRunID is "run_" + a ULID: 48-bit millisecond time then 80 random
// bits, Crockford base32. IDs made in the same millisecond increment the
// random part, so ids sort in creation order (paging uses that).
func NewRunID(now time.Time) string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	ms := uint64(now.UnixMilli())
	if ms <= ulidLast.ms {
		ms = ulidLast.ms
		for i := len(ulidLast.rand) - 1; i >= 0; i-- {
			ulidLast.rand[i]++
			if ulidLast.rand[i] != 0 {
				break
			}
		}
	} else {
		ulidLast.ms = ms
		if _, err := rand.Read(ulidLast.rand[:]); err != nil {
			panic(fmt.Sprintf("crypto/rand: %v", err))
		}
		ulidLast.rand[0] &= 0x7f // leave room to increment within a millisecond
	}
	var b [16]byte
	for i := 0; i < 6; i++ {
		b[i] = byte(ms >> (40 - 8*i))
	}
	copy(b[6:], ulidLast.rand[:])
	// 128 bits -> 26 base32 digits (the first carries 3 bits).
	out := make([]byte, 26)
	var acc uint64
	bits := 0
	idx := 25
	for i := 15; i >= 0; i-- {
		acc |= uint64(b[i]) << bits
		bits += 8
		for bits >= 5 && idx >= 0 {
			out[idx] = crockford[acc&31]
			acc >>= 5
			bits -= 5
			idx--
		}
	}
	for idx >= 0 {
		out[idx] = crockford[acc&31]
		acc >>= 5
		idx--
	}
	return "run_" + string(out)
}

// ---------------------------------------------------------------------
// Jobs

// ListJobs returns every job, oldest first.
func (s *Store) ListJobs() ([]Job, error) {
	var rows []JobRow
	if err := s.db.Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: list jobs: %w", err)
	}
	out := make([]Job, 0, len(rows))
	for _, r := range rows {
		j, err := r.ToJob()
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}

// GetJob returns one job, or an error wrapping errNoRecord.
func (s *Store) GetJob(id string) (Job, error) {
	var r JobRow
	err := s.db.Where("id = ?", id).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Job{}, fmt.Errorf("job %s: %w", id, errNoRecord)
	}
	if err != nil {
		return Job{}, fmt.Errorf("store: get job %s: %w", id, err)
	}
	return r.ToJob()
}

// JobByMigratedFrom returns the job imported from a Scheduled Task.
func (s *Store) JobByMigratedFrom(taskID string) (Job, error) {
	var r JobRow
	err := s.db.Where("migrated_from = ?", taskID).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Job{}, fmt.Errorf("job migrated from %s: %w", taskID, errNoRecord)
	}
	if err != nil {
		return Job{}, fmt.Errorf("store: job by task %s: %w", taskID, err)
	}
	return r.ToJob()
}

// CreateJob stores a new job: it gets a fresh id, revision 1, a new
// destination folder id and its timestamps.
func (s *Store) CreateJob(j Job, now time.Time) (Job, error) {
	return s.createJobs([]Job{j}, now, nil)
}

// createJobs inserts jobs in one transaction; extra runs inside it too
// (the migration stores its bookkeeping in the same commit).
func (s *Store) createJobs(jobs []Job, now time.Time, extra func(tx *gorm.DB) error) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var last Job
	err := s.db.Transaction(func(tx *gorm.DB) error {
		for _, j := range jobs {
			j.ID = NewJobID()
			j.Revision = 1
			if j.DestFolderID == "" {
				j.DestFolderID = NewUUID4()
			}
			j.CreatedAt, j.UpdatedAt = now, now
			row, err := JobToRow(j)
			if err != nil {
				return err
			}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("store: create job: %w", err)
			}
			last = j
		}
		if extra != nil {
			return extra(tx)
		}
		return nil
	})
	return last, err
}

// RevisionConflict is returned by UpdateJob when the caller's revision
// isn't the stored one; Current is the stored job.
type RevisionConflict struct{ Current Job }

func (e *RevisionConflict) Error() string {
	return fmt.Sprintf("job %s is at revision %d", e.Current.ID, e.Current.Revision)
}

// UpdateJob replaces the user-editable fields of a job whose stored
// revision is expectRevision and bumps the revision. Server-owned fields
// (destination folder id, migration marker, created time) are kept; fn,
// when set, sees the job as it was (prev) and may adjust the new one
// (stored) in the same transaction.
func (s *Store) UpdateJob(id string, expectRevision int, edit Job, now time.Time, fn func(prev Job, stored *Job)) (Job, error) {
	return s.mutateJob(id, func(cur *Job) error {
		if cur.Revision != expectRevision {
			return &RevisionConflict{Current: *cur}
		}
		keep := *cur
		*cur = edit
		cur.ID, cur.Revision = keep.ID, keep.Revision
		cur.DestFolderID, cur.MigratedFrom, cur.CreatedAt = keep.DestFolderID, keep.MigratedFrom, keep.CreatedAt
		cur.NeedsAttention = keep.NeedsAttention
		if fn != nil {
			fn(keep, cur)
		}
		return nil
	}, now, true)
}

// MutateJob applies fn to a stored job in one transaction. bump says
// whether the change is the user's (it bumps the revision, so an editor
// open elsewhere gets a 409) or the server's bookkeeping (it doesn't).
func (s *Store) MutateJob(id string, now time.Time, bump bool, fn func(j *Job) error) (Job, error) {
	return s.mutateJob(id, fn, now, bump)
}

func (s *Store) mutateJob(id string, fn func(j *Job) error, now time.Time, bump bool) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Job
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var r JobRow
		err := tx.Where("id = ?", id).Take(&r).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("job %s: %w", id, errNoRecord)
		}
		if err != nil {
			return fmt.Errorf("store: get job %s: %w", id, err)
		}
		j, err := r.ToJob()
		if err != nil {
			return err
		}
		prevRev := j.Revision
		if err := fn(&j); err != nil {
			return err
		}
		j.ID, j.Revision = id, prevRev
		if bump {
			j.Revision = prevRev + 1
			j.UpdatedAt = now
		}
		row, err := JobToRow(j)
		if err != nil {
			return err
		}
		// Conditional on the revision we read, as a second fence against
		// a writer in another process.
		res := tx.Model(&JobRow{}).Where("id = ? AND revision = ?", id, prevRev).
			Select("*").UpdateColumns(&row)
		if res.Error != nil {
			return fmt.Errorf("store: update job %s: %w", id, res.Error)
		}
		if res.RowsAffected != 1 {
			return &RevisionConflict{Current: j}
		}
		out = j
		return nil
	})
	return out, err
}

// DeleteJob removes a job and every run of it except keepRunID, and
// returns the removed runs (so the caller can delete their files).
func (s *Store) DeleteJob(id, keepRunID string) ([]RunRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var runs []RunRow
	err := s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ?", id).Delete(&JobRow{})
		if res.Error != nil {
			return fmt.Errorf("store: delete job %s: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("job %s: %w", id, errNoRecord)
		}
		if err := tx.Where("job_id = ? AND id <> ?", id, keepRunID).Find(&runs).Error; err != nil {
			return fmt.Errorf("store: runs of %s: %w", id, err)
		}
		if err := tx.Where("job_id = ? AND id <> ?", id, keepRunID).Delete(&RunRow{}).Error; err != nil {
			return fmt.Errorf("store: delete runs of %s: %w", id, err)
		}
		return tx.Where("key = ?", jobStateKey(id)).Delete(&MetaRow{}).Error
	})
	return runs, err
}

// ---------------------------------------------------------------------
// Runs

// CreateRun inserts a run.
func (s *Store) CreateRun(r RunRow) error {
	if err := s.db.Create(&r).Error; err != nil {
		return fmt.Errorf("store: create run: %w", err)
	}
	return nil
}

// SaveRun writes every field of a run except Coalesced, which only
// IncCoalesced changes: a worker saving its copy of the run must not undo
// a trigger coalesced onto it meanwhile.
func (s *Store) SaveRun(r RunRow) error {
	if err := s.db.Omit("Coalesced").Save(&r).Error; err != nil {
		return fmt.Errorf("store: save run %s: %w", r.ID, err)
	}
	return nil
}

// SaveRunHooks writes only a run's HooksDone.
func (s *Store) SaveRunHooks(id, hooksDone string) error {
	res := s.db.Model(&RunRow{}).Where("id = ?", id).UpdateColumn("hooks_done", hooksDone)
	if res.Error != nil {
		return fmt.Errorf("store: run %s: %w", id, res.Error)
	}
	return nil
}

// RunsWithPendingRestarts returns, oldest first, the runs no worker owns
// (not queued or running) that still have a pre hook whose apps or VMs
// weren't all started again. HooksDone is written by encoding/json, so
// an open hook always reads "post_done":false.
func (s *Store) RunsWithPendingRestarts() ([]RunRow, error) {
	var rows []RunRow
	err := s.db.Where("hooks_done LIKE ? AND status NOT IN ?", `%"post_done":false%`,
		statusStrings([]RunStatus{StatusQueued, StatusRunning})).Order("id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: list runs with pending restarts: %w", err)
	}
	return rows, nil
}

// IncCoalesced records one more trigger coalesced onto a run.
func (s *Store) IncCoalesced(id string) error {
	res := s.db.Model(&RunRow{}).Where("id = ?", id).UpdateColumn("coalesced", gorm.Expr("coalesced + 1"))
	if res.Error != nil {
		return fmt.Errorf("store: run %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("run %s: %w", id, errNoRecord)
	}
	return nil
}

// GetRun returns one run, or an error wrapping errNoRecord.
func (s *Store) GetRun(id string) (RunRow, error) {
	var r RunRow
	err := s.db.Where("id = ?", id).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return RunRow{}, fmt.Errorf("run %s: %w", id, errNoRecord)
	}
	if err != nil {
		return RunRow{}, fmt.Errorf("store: get run %s: %w", id, err)
	}
	return r, nil
}

// RunFilter selects runs for ListRuns. Zero fields don't filter.
type RunFilter struct {
	JobID    string
	Statuses []RunStatus
	Kinds    []RunKind
	Before   string // run id, exclusive
	Limit    int
}

// ListRuns returns runs newest first (run ids sort by creation time).
func (s *Store) ListRuns(f RunFilter) ([]RunRow, error) {
	q := s.db.Model(&RunRow{})
	if f.JobID != "" {
		q = q.Where("job_id = ?", f.JobID)
	}
	if len(f.Statuses) > 0 {
		q = q.Where("status IN ?", statusStrings(f.Statuses))
	}
	if len(f.Kinds) > 0 {
		kinds := make([]string, len(f.Kinds))
		for i, k := range f.Kinds {
			kinds[i] = string(k)
		}
		q = q.Where("kind IN ?", kinds)
	}
	if f.Before != "" {
		q = q.Where("id < ?", f.Before)
	}
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	var rows []RunRow
	if err := q.Order("id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: list runs: %w", err)
	}
	return rows, nil
}

func statusStrings(ss []RunStatus) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = string(s)
	}
	return out
}

// LastRun returns a job's newest run of one of kinds in one of statuses
// (nil when there is none).
func (s *Store) LastRun(jobID string, kinds []RunKind, statuses []RunStatus) (*RunRow, error) {
	rows, err := s.ListRuns(RunFilter{JobID: jobID, Kinds: kinds, Statuses: statuses, Limit: 1})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// ---------------------------------------------------------------------
// Meta

// GetMeta returns a meta value; ok is false when the key is absent.
func (s *Store) GetMeta(key string) (string, bool, error) {
	var r MetaRow
	err := s.db.Where("key = ?", key).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: meta %s: %w", key, err)
	}
	return r.Value, true, nil
}

// SetMeta writes a meta value.
func (s *Store) SetMeta(key, value string) error {
	return setMetaTx(s.db, key, value)
}

func setMetaTx(tx *gorm.DB, key, value string) error {
	if err := tx.Save(&MetaRow{Key: key, Value: value}).Error; err != nil {
		return fmt.Errorf("store: set meta %s: %w", key, err)
	}
	return nil
}

// DeleteMeta removes a meta key (absent keys are fine).
func (s *Store) DeleteMeta(key string) error {
	if err := s.db.Where("key = ?", key).Delete(&MetaRow{}).Error; err != nil {
		return fmt.Errorf("store: delete meta %s: %w", key, err)
	}
	return nil
}

// MetaWithPrefix returns every meta row whose key starts with prefix.
func (s *Store) MetaWithPrefix(prefix string) ([]MetaRow, error) {
	var rows []MetaRow
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
	if err := s.db.Where(`key LIKE ? ESCAPE '\'`, escaped+"%").Order("key").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: meta %s*: %w", prefix, err)
	}
	return rows, nil
}

// GetMetaJSON decodes a JSON meta value into v; ok is false when absent.
func (s *Store) GetMetaJSON(key string, v interface{}) (bool, error) {
	raw, ok, err := s.GetMeta(key)
	if err != nil || !ok {
		return ok, err
	}
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		return true, fmt.Errorf("store: decode meta %s: %w", key, err)
	}
	return true, nil
}

// SetMetaJSON stores v as JSON.
func (s *Store) SetMetaJSON(key string, v interface{}) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("store: encode meta %s: %w", key, err)
	}
	return s.SetMeta(key, string(raw))
}

// ---------------------------------------------------------------------
// Settings

// Settings returns the app settings, defaults when never saved.
func (s *Store) Settings() (AppSettings, error) {
	st := DefaultAppSettings()
	if _, err := s.GetMetaJSON(MetaSettings, &st); err != nil {
		return DefaultAppSettings(), err
	}
	return st, nil
}

// SaveSettings stores the app settings.
func (s *Store) SaveSettings(st AppSettings) error { return s.SetMetaJSON(MetaSettings, st) }

// ---------------------------------------------------------------------
// Per-job bookkeeping that is not part of the job definition.

// metaJobState is the meta key prefix of JobState ("job." + job id).
const metaJobState = "job."

func jobStateKey(id string) string { return metaJobState + id }

// JobState is what the job side remembers about a job between runs.
type JobState struct {
	// LastSuccessAt / SizeBytes / Baseline come from the last successful
	// backup run (JobStats, the next run's sentinel baseline).
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	SizeBytes     int64      `json:"size_bytes"`
	SourceFiles   int64      `json:"source_files"`
	DestFiles     int64      `json:"dest_files"`
	// SourceFilesBy is the same per source (sourceKey), for a job whose
	// sources go through the engine one at a time (a one_at_a_time
	// archive): each of those transfers is compared with its own source's
	// count, never with the sum over all of them.
	SourceFilesBy map[string]int64 `json:"source_files_by,omitempty"`
	// VolumeFires: per volume_mounted trigger ref ("kind|ref_id"), the
	// mount id it last fired for and when (once per attach, min gap).
	VolumeFires map[string]VolumeFire `json:"volume_fires,omitempty"`
	// VolumeSeen: when the trigger volume was last seen mounted, for
	// catch-up after a restart.
	VolumeSeen map[string]time.Time `json:"volume_seen,omitempty"`
	// OfflineMisses counts consecutive runs skipped because the
	// destination stayed offline past its wait; the second one notifies.
	OfflineMisses   int        `json:"offline_misses"`
	OfflineNotified bool       `json:"offline_notified"`
	StaleNotifiedAt *time.Time `json:"stale_notified_at,omitempty"`
	// GuardTripped: the last backup run tripped a guard; pruning stays
	// suspended until a run succeeds (spec §9).
	GuardTripped bool `json:"guard_tripped"`
}

// VolumeFire is one volume trigger's last firing.
type VolumeFire struct {
	MountID int       `json:"mount_id"`
	At      time.Time `json:"at"`
}

// JobState returns a job's bookkeeping (zero value when none yet).
func (s *Store) JobState(id string) (JobState, error) {
	var st JobState
	_, err := s.GetMetaJSON(jobStateKey(id), &st)
	return st, err
}

// UpdateJobState applies fn to a job's bookkeeping in one transaction.
func (s *Store) UpdateJobState(id string, fn func(st *JobState)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Transaction(func(tx *gorm.DB) error {
		var st JobState
		var row MetaRow
		err := tx.Where("key = ?", jobStateKey(id)).Take(&row).Error
		switch {
		case err == nil:
			if err := json.Unmarshal([]byte(row.Value), &st); err != nil {
				return fmt.Errorf("store: decode job state %s: %w", id, err)
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
		default:
			return fmt.Errorf("store: job state %s: %w", id, err)
		}
		fn(&st)
		raw, err := json.Marshal(st)
		if err != nil {
			return err
		}
		return setMetaTx(tx, jobStateKey(id), string(raw))
	})
}

// ---------------------------------------------------------------------
// Remembered USB drives

// RememberedDrives lists every remembered drive, by UUID.
func (s *Store) RememberedDrives() (map[string]RememberedDrive, error) {
	rows, err := s.MetaWithPrefix(MetaRememberedDrive)
	if err != nil {
		return nil, err
	}
	out := make(map[string]RememberedDrive, len(rows))
	for _, r := range rows {
		var d RememberedDrive
		if err := json.Unmarshal([]byte(r.Value), &d); err != nil {
			return nil, fmt.Errorf("store: decode %s: %w", r.Key, err)
		}
		out[strings.TrimPrefix(r.Key, MetaRememberedDrive)] = d
	}
	return out, nil
}

// RememberDrive records (or refreshes) a USB drive. A user label is kept;
// seen updates LastSeen.
func (s *Store) RememberDrive(ep Endpoint, seen *time.Time) error {
	if ep.Kind != EPUSB || ep.RefID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := MetaRememberedDrive + ep.RefID
	var d RememberedDrive
	if _, err := s.GetMetaJSON(key, &d); err != nil {
		return err
	}
	label := d.Label
	d.Endpoint = Endpoint{Kind: EPUSB, RefID: ep.RefID, Match: ep.Match, Label: ep.Label}
	d.Label = label
	if seen != nil && seen.After(d.LastSeen) {
		d.LastSeen = *seen
	}
	return s.SetMetaJSON(key, d)
}

// ---------------------------------------------------------------------
// Files

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// writeFileAtomic replaces path with data: temp file in the same folder,
// fsync, rename, fsync of the folder. A crash leaves the old or the new
// file, never a torn one.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
