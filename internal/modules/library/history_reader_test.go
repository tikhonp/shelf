package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/libraryport"
)

type historyReaderFunc func(context.Context, int64) (int64, error)

func (f historyReaderFunc) CatchUpHistory(ctx context.Context, id int64) (int64, error) {
	return f(ctx, id)
}

func TestHistoryReaderTrimBoundary(t *testing.T) {
	for _, mode := range []string{"confirmed", "behind", "failure", "timeout", "finishes", "new save", "new undo", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			r := newRig(t)
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := r.m.store.db.W.Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			add := func(id int, state string) {
				exec("INSERT INTO library_changes(id,kind,album_id,folder,title,artist,state,created_at,finished_at) VALUES(?,'save',1,'a','a','a',?,?,?)", id, state, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
			}
			add(1, "done")
			if mode == "finishes" {
				add(2, "writing")
				add(3, "done")
			}
			calls := 0
			reader := historyReaderFunc(func(ctx context.Context, through int64) (int64, error) {
				calls++
				if through != 1 {
					t.Fatalf("cutoff %d", through)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("reader has no deadline")
				}
				switch mode {
				case "behind":
					return 0, nil
				case "failure":
					return 1, errors.New("reader failed")
				case "timeout":
					return 0, context.DeadlineExceeded
				case "finishes":
					exec("UPDATE library_changes SET state='done' WHERE id=2")
				case "new save":
					add(2, "done")
				case "new undo":
					exec("INSERT INTO library_changes(id,kind,target,album_id,folder,title,artist,state,created_at) VALUES(2,'undo',1,1,'a','a','a','done',?)", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
				}
				return through, nil
			})
			if mode != "disabled" {
				r.m.store.historyReader = reader
			}
			n, err := r.m.store.trim(bg, 0, time.Now())
			failed := mode == "failure" || mode == "timeout"
			if (err != nil) != failed {
				t.Fatalf("trim %d: %v", n, err)
			}
			want := int64(1)
			if failed || mode == "behind" || mode == "new undo" {
				want = 0
			}
			if n != want {
				t.Fatalf("deleted %d, want %d", n, want)
			}
			if mode == "disabled" && calls != 0 {
				t.Fatal("disabled reader called")
			}
			if mode == "finishes" || mode == "new save" {
				page, err := r.m.Port().SaveHistory(bg, 1, 50)
				want := 1
				if mode == "finishes" {
					want = 2
				}
				if err != nil || len(page.Changes) != want {
					t.Fatalf("unread history %+v: %v", page, err)
				}
			}
			if mode == "new undo" {
				var n int
				if err := r.m.store.db.R.Get(&n, "SELECT count(*) FROM library_changes"); err != nil || n != 2 {
					t.Fatalf("cascade removed unread undo: %d %v", n, err)
				}
			}
		})
	}
}

var _ libraryport.HistoryReader = historyReaderFunc(nil)
