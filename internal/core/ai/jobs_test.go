package ai

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func newTestStore() (*JobStore, *time.Time) {
	base := time.UnixMilli(1_000_000)
	now := base
	n := 0
	s := NewJobStore(JobStoreOptions{
		Now:        func() time.Time { return now },
		NewID:      func() string { n++; return fmt.Sprintf("job-%d", n) },
		MaxRunning: 2,
		TTL:        time.Hour,
	})
	return s, &now
}

func TestJobLifecycle(t *testing.T) {
	s, now := newTestStore()
	job, err := s.Start("u1", JobKindPassage)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := s.View(job.ID, "u1")
	if !ok || v.ID != "job-1" || v.Kind != JobKindPassage || v.Status != JobRunning || v.ElapsedMs != 0 {
		t.Fatalf("initial view = %+v", v)
	}
	*now = now.Add(5 * time.Second)
	v, _ = s.View(job.ID, "u1")
	if v.ElapsedMs != 5000 {
		t.Fatalf("elapsed = %d", v.ElapsedMs)
	}
	if !s.Finish(job.ID, map[string]string{"id": "p1"}) {
		t.Fatal("finish should succeed")
	}
	*now = now.Add(10 * time.Second)
	v, _ = s.View(job.ID, "u1")
	if v.Status != JobDone || v.ElapsedMs != 5000 || v.Result == nil {
		t.Fatalf("done view = %+v", v)
	}
}

func TestJobFailureAndFinality(t *testing.T) {
	s, _ := newTestStore()
	job, _ := s.Start("u1", JobKindExample)
	if !s.Fail(job.ID, "请求超时（30 秒）") {
		t.Fatal("fail should succeed")
	}
	if s.Finish(job.ID, map[string]string{}) {
		t.Fatal("finished job should not be re-finishable")
	}
	if s.Fail(job.ID, "x") {
		t.Fatal("finished job should not be re-failable")
	}
	v, _ := s.View(job.ID, "u1")
	if v.Status != JobFailed || v.Error != "请求超时（30 秒）" || v.Result != nil {
		t.Fatalf("view = %+v", v)
	}
	if s.Finish("nope", map[string]string{}) {
		t.Fatal("finishing missing job should fail")
	}
}

func TestJobViewOwnership(t *testing.T) {
	s, _ := newTestStore()
	job, _ := s.Start("u1", JobKindPassage)
	if _, ok := s.View(job.ID, "u2"); ok {
		t.Fatal("other owner should not see the job")
	}
	if _, ok := s.View("missing", "u1"); ok {
		t.Fatal("missing job should not be found")
	}
}

func TestJobConcurrencyLimit(t *testing.T) {
	s, _ := newTestStore()
	a, err := s.Start("u1", JobKindPassage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start("u1", JobKindExamples); err != nil {
		t.Fatal(err)
	}
	_, err = s.Start("u1", JobKindPassage)
	if !errors.Is(err, ErrTooManyJobs) {
		t.Fatalf("expected ErrTooManyJobs, got %v", err)
	}
	if err.Error() != TooManyJobsMessage {
		t.Fatalf("message = %q", err.Error())
	}
	if _, err := s.Start("u2", JobKindPassage); err != nil {
		t.Fatalf("other owner should not be limited: %v", err)
	}
	s.Finish(a.ID, map[string]string{})
	if n := s.runningCount("u1"); n != 1 {
		t.Fatalf("running count = %d", n)
	}
	if _, err := s.Start("u1", JobKindPassage); err != nil {
		t.Fatalf("should be able to start after one finished: %v", err)
	}
}

func TestJobSweep(t *testing.T) {
	s, now := newTestStore()
	done, _ := s.Start("u1", JobKindPassage)
	running, _ := s.Start("u1", JobKindPassage)
	s.Finish(done.ID, map[string]string{})
	*now = now.Add(3_599_999 * time.Millisecond)
	if _, ok := s.View(done.ID, "u1"); !ok {
		t.Fatal("should still be visible just before TTL")
	}
	*now = now.Add(1 * time.Millisecond)
	if _, ok := s.View(done.ID, "u1"); ok {
		t.Fatal("should be swept after TTL")
	}
	v, ok := s.View(running.ID, "u1")
	if !ok || v.Status != JobRunning {
		t.Fatalf("running job should remain: %+v", v)
	}
	if s.Size() != 1 {
		t.Fatalf("size = %d", s.Size())
	}
}
