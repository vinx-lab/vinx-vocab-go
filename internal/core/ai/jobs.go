package ai

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// JobKind 后台任务种类。
type JobKind = string

const (
	JobKindPassage  JobKind = "passage"
	JobKindExamples JobKind = "examples"
	JobKindExample  JobKind = "example"
)

// JobStatus 任务状态。
type JobStatus = string

const (
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

// JobMaxRunning 同一个账号同时进行中的任务上限。
const JobMaxRunning = 2

// JobTTL 任务完成后保留多久。
const JobTTL = time.Hour

// TooManyJobsMessage 超出并发上限的说明。
var TooManyJobsMessage = fmt.Sprintf("同时最多进行 %d 个生成任务，请等前面的完成后再试", JobMaxRunning)

// ErrTooManyJobs 同一账号进行中的任务已达上限（服务层转 429 TOO_MANY_REQUESTS）。
var ErrTooManyJobs = errors.New(TooManyJobsMessage)

// Job 一个后台任务（进程内存，api 重启后丢失）。
type Job struct {
	ID         string
	OwnerID    string
	Kind       JobKind
	Status     JobStatus
	StartedAt  time.Time
	FinishedAt *time.Time
	Result     any
	Err        string
}

// JobView GET /ai/jobs/:id 的视图。
type JobView struct {
	ID        string    `json:"id"`
	Kind      JobKind   `json:"kind"`
	Status    JobStatus `json:"status"`
	ElapsedMs int64     `json:"elapsedMs"`
	Result    any       `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// JobStarted 生成接口的返回。
type JobStarted struct {
	JobID string `json:"jobId"`
}

// JobStoreOptions 构造选项（测试注入时间与 id 生成）。
type JobStoreOptions struct {
	Now        func() time.Time
	NewID      func() string
	MaxRunning int
	TTL        time.Duration
}

// JobStore AI 生成后台任务的内存存放（线程安全）：开始（每个账号同时最多 N 个进行中，超出返回
// ErrTooManyJobs）、完成、失败、按发起人查询、过期清理（完成后保留 1 小时）。
type JobStore struct {
	mu         sync.Mutex
	jobs       map[string]*Job
	now        func() time.Time
	newID      func() string
	maxRunning int
	ttl        time.Duration
}

func randomJobID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NewJobStore 构造；未提供的选项用默认值。
func NewJobStore(opts JobStoreOptions) *JobStore {
	s := &JobStore{jobs: map[string]*Job{}, now: opts.Now, newID: opts.NewID, maxRunning: opts.MaxRunning, ttl: opts.TTL}
	if s.now == nil {
		s.now = time.Now
	}
	if s.newID == nil {
		s.newID = randomJobID
	}
	if s.maxRunning == 0 {
		s.maxRunning = JobMaxRunning
	}
	if s.ttl == 0 {
		s.ttl = JobTTL
	}
	return s
}

// sweep 删掉完成超过 ttl 的任务；调用方需持有锁。返回删掉的个数。
func (s *JobStore) sweep() int {
	t := s.now()
	removed := 0
	for id, job := range s.jobs {
		if job.FinishedAt != nil && t.Sub(*job.FinishedAt) >= s.ttl {
			delete(s.jobs, id)
			removed++
		}
	}
	return removed
}

func (s *JobStore) runningCount(ownerID string) int {
	n := 0
	for _, job := range s.jobs {
		if job.OwnerID == ownerID && job.Status == JobRunning {
			n++
		}
	}
	return n
}

// Start 开始一个任务；同一账号进行中的任务已达上限时返回 ErrTooManyJobs。
func (s *JobStore) Start(ownerID string, kind JobKind) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	if s.runningCount(ownerID) >= s.maxRunning {
		return nil, ErrTooManyJobs
	}
	job := &Job{ID: s.newID(), OwnerID: ownerID, Kind: kind, Status: JobRunning, StartedAt: s.now()}
	s.jobs[job.ID] = job
	return job, nil
}

// settle 只有进行中的任务可以结束；已结束或不存在的忽略（返回 false）。
func (s *JobStore) settle(id, status string, result any, errMsg string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok || job.Status != JobRunning {
		return false
	}
	job.Status, job.Result, job.Err = status, result, errMsg
	t := s.now()
	job.FinishedAt = &t
	return true
}

// Finish 任务成功完成。
func (s *JobStore) Finish(id string, result any) bool { return s.settle(id, JobDone, result, "") }

// Fail 任务失败。
func (s *JobStore) Fail(id string, errMsg string) bool { return s.settle(id, JobFailed, nil, errMsg) }

// View 按发起人查询；不是本人的、不存在的、已过期的都返回 (nil, false)。
func (s *JobStore) View(id, ownerID string) (*JobView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	job, ok := s.jobs[id]
	if !ok || job.OwnerID != ownerID {
		return nil, false
	}
	end := s.now()
	if job.FinishedAt != nil {
		end = *job.FinishedAt
	}
	elapsed := end.Sub(job.StartedAt).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	v := &JobView{ID: job.ID, Kind: job.Kind, Status: job.Status, ElapsedMs: elapsed}
	if job.Status == JobDone {
		v.Result = job.Result
	}
	if job.Status == JobFailed {
		v.Error = job.Err
	}
	return v, true
}

// Size 当前任务数（测试用）。
func (s *JobStore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs)
}
