package jobs

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/jmoiron/sqlx"
)

// Run is what a step gets: the job's identity, payload and log.
type Run struct {
	s    *System
	info Info
	log  *Logger

	mu       sync.Mutex
	payload  json.RawMessage
	cancel   chan struct{}
	stop     context.CancelFunc // ends the steps' context
	cancelBy string
	canceled bool
}

// Info describes the job.
func (r *Run) Info() Info { return r.info }

// Log is the job's logger.
func (r *Run) Log() *Logger { return r.log }

// Payload decodes the job's payload into v.
func (r *Run) Payload(v any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return json.Unmarshal(r.payload, v)
}

// SavePayload replaces the payload with v, for results later steps need. It
// runs in its own transaction.
func (r *Run) SavePayload(ctx context.Context, v any) error {
	b, err := marshalObject(v)
	if err != nil {
		return err
	}
	if len(b) > MaxPayload {
		return ErrPayloadTooLarge
	}
	err = r.s.d.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE jobs SET payload = ? WHERE id = ?`, string(b), r.info.ID)
		return err
	})
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.payload = b
	r.mu.Unlock()
	return nil
}

// Cancelling is closed when someone asks to cancel the job. The step's
// context ends at the same moment, so a step that only watches its context
// stops too.
func (r *Run) Cancelling() <-chan struct{} { return r.cancel }

func (r *Run) requestCancel(by string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.canceled {
		r.canceled, r.cancelBy = true, by
		close(r.cancel)
		if r.stop != nil {
			r.stop()
		}
	}
}

func (r *Run) cancelled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.canceled
}
