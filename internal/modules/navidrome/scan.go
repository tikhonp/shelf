package navidrome

import (
	"context"
	"errors"
	"time"

	"github.com/tikhonp/shelf/internal/platform/jobs"
)

// JobType is the scan job.
const JobType = "navidrome.scan"

// scanKey coalesces every scan request into the one queued scan job.
const scanKey = "scan"

// Retries of a scan that Navidrome didn't take.
const (
	// RetryAfter is the wait after Navidrome didn't answer.
	RetryAfter = time.Minute
	// maxFailures ends the scan job after about ten minutes of no answer.
	maxFailures = 10
)

// scanPayload counts the scan's failed tries. A request that merges into the
// job keeps the count.
type scanPayload struct {
	Failures int `json:"failures,omitempty"`
}

// JobTypes is the scan job. A failure defers it rather than counting an
// attempt, so it stays the one queued scan that requests merge into.
func (m *Module) JobTypes() []jobs.Type {
	return []jobs.Type{{
		Name: JobType, Queue: jobs.QueueLibrary, MaxAttempts: 1,
		Steps: []jobs.Step{{Name: "scan", Run: m.scan}},
	}}
}

// Schedules: none.
func (*Module) Schedules() []jobs.Schedule { return nil }

// RequestScan is the downloads module's Scanner port. It queues a scan for
// SHELF_NAVIDROME_SCAN_DELAY from now. A request in a burst merges into the
// queued scan and moves it later, so a burst of downloads gives one scan.
func (m *Module) RequestScan(ctx context.Context) error {
	if !m.Enabled() {
		return nil
	}
	_, err := m.jobs.EnqueueNow(ctx, jobs.Request{
		Type: JobType, CoalescingKey: scanKey, Delay: m.cfg.ScanDelay, CreatedBy: "downloads",
	})
	return err
}

// ScanNow queues a scan that runs at once, or moves the queued one to now.
func (m *Module) ScanNow(ctx context.Context, by string) (int64, error) {
	e, err := m.jobs.EnqueueNow(ctx, jobs.Request{Type: JobType, CoalescingKey: scanKey, CreatedBy: by})
	if err != nil {
		return 0, err
	}
	if e.Merged {
		// A merge keeps the later start of the two.
		if err := m.jobs.RunNow(ctx, e.ID); err != nil && !errors.Is(err, jobs.ErrNotActive) {
			return 0, err
		}
		m.jobs.Kick()
	}
	return e.ID, nil
}

// scan starts a Navidrome scan unless one runs, and has Run follow it until
// it ends. A scan that runs already defers the job, so a scan that starts
// after the downloads that asked reads their files.
func (m *Module) scan(ctx context.Context, r *jobs.Run) error {
	if !m.Enabled() {
		r.Log().Info("%s", textOff)
		return nil
	}
	st, err := m.lib.ScanStatus(ctx)
	if err != nil {
		return m.failed(ctx, r, err)
	}
	if st.Scanning {
		m.follow()
		r.Log().Info("Navidrome is already scanning; trying again in %s", m.DeferFor)
		return jobs.Defer(m.DeferFor, "Navidrome is already scanning")
	}
	if err := m.lib.StartScan(ctx); err != nil {
		return m.failed(ctx, r, err)
	}
	r.Log().Info("Navidrome started a scan")
	st.Scanning = true
	m.publish(st)
	m.follow()
	return nil
}

// failed defers the scan for RetryAfter, up to maxFailures times. A refusal,
// such as a wrong password, ends it at once, since trying again won't help.
func (m *Module) failed(ctx context.Context, r *jobs.Run, err error) error {
	if ctx.Err() != nil {
		return err
	}
	var se *SubsonicError
	if errors.As(err, &se) {
		return jobs.Permanent(err)
	}
	var p scanPayload
	if perr := r.Payload(&p); perr != nil {
		return jobs.Permanent(perr)
	}
	if p.Failures++; p.Failures >= maxFailures {
		return jobs.Permanent(err)
	}
	if serr := r.SavePayload(ctx, p); serr != nil {
		return serr
	}
	r.Log().Warn("Navidrome did not answer, trying again in %s: %v", m.RetryAfter, err)
	return jobs.Defer(m.RetryAfter, "Navidrome did not answer")
}

// follow asks Run to read the scan state until the scan ends.
func (m *Module) follow() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Run follows scans while serve runs. Polling runs only while a scan is
// known to run.
func (m *Module) Run(ctx context.Context) error {
	if !m.Enabled() {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-m.wake:
		}
		m.poll(ctx)
	}
}

// poll reads the scan state every PollEvery and publishes it once the scan
// has ended.
func (m *Module) poll(ctx context.Context) {
	t := time.NewTicker(m.PollEvery)
	defer t.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		st, err := m.lib.ScanStatus(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if failures++; failures >= maxPollFailures {
				m.log.Warn("navidrome: stopped following the scan", "error", err)
				return
			}
			continue
		}
		failures = 0
		if st.Scanning {
			continue
		}
		// A wake that came in while this scan ran is about this scan. A scan
		// started from now on wakes again.
		select {
		case <-m.wake:
		default:
		}
		m.publish(st)
		return
	}
}
