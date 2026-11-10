package agents

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/modules/library"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/testwait"
)

func TestHistoryRemovalCatchesUpMatchingSave(t *testing.T) {
	for _, mode := range []string{"clear", "retention", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			lib := library.New()
			r := rigWithLibrary(t, mode != "disabled", nil, lib)
			var proposal int64
			if mode != "disabled" {
				proposal = create(t, r)
			}
			r.m.SetLibrary(lib.Port())
			at := time.Now().Add(time.Second).UTC().Format("2006-01-02T15:04:05.000Z")
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := r.app.DB.W.Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("INSERT INTO library_files(id,path,size,mtime_ns,first_seen) VALUES(1,'music/album/1.flac',1,1,?)", at)
			exec("INSERT INTO library_changes(id,kind,album_id,folder,title,artist,state,created_at,finished_at) VALUES(1,'save',1,'music/album','Album','Artist','done',?,?)", at, at)
			exec(`INSERT INTO library_change_files(change_id,path,before,after,written) VALUES(1,'music/album/1.flac','{"GENRE":["Rock"]}','{"GENRE":["Art Rock"]}',1)`)

			if mode == "retention" {
				for i := 2; i <= library.HistoryLimit+1; i++ {
					exec("INSERT INTO library_changes(id,kind,album_id,folder,title,artist,state,created_at,finished_at) VALUES(?,'save',1,'music/other','Other','Artist','done',?,?)", i, at, at)
				}
				ctx, cancel := context.WithCancel(context.Background())
				stopped := make(chan error, 1)
				go func() { stopped <- r.app.Jobs.Start(ctx) }()
				defer func() {
					cancel()
					if err := <-stopped; err != nil {
						t.Error(err)
					}
				}()
				enq, err := r.app.Jobs.EnqueueNow(ctx, jobs.Request{Type: library.RetentionJob, CreatedBy: "test"})
				if err != nil {
					t.Fatal(err)
				}
				deadline := testwait.Deadline(10 * time.Second)
				for {
					var state string
					if err := r.app.DB.R.Get(&state, "SELECT state FROM jobs WHERE id=?", enq.ID); err != nil {
						t.Fatal(err)
					}
					if state == "succeeded" {
						break
					}
					if state == "failed" || time.Now().After(deadline) {
						t.Fatalf("retention state %s", state)
					}
					time.Sleep(5 * time.Millisecond)
				}
			} else {
				want(t, r.call(t, http.MethodDelete, "/api/library/history", nil, false), 200)
			}
			var remaining int
			if err := r.app.DB.R.Get(&remaining, "SELECT count(*) FROM library_changes WHERE id=1"); err != nil || remaining != 0 {
				t.Fatalf("remaining %d: %v", remaining, err)
			}
			if mode == "disabled" {
				return
			}
			as, err := getAcceptances(context.Background(), r.app.DB.R, proposal)
			if err != nil || len(as) != 1 || !as[0].ViaSave || as[0].outcome().State != "done" {
				t.Fatalf("acceptances %+v: %v", as, err)
			}
			if err := r.m.reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			as, err = getAcceptances(context.Background(), r.app.DB.R, proposal)
			if err != nil || len(as) != 1 || !as[0].outcome().Expired || as[0].outcome().State != "done" {
				t.Fatalf("expired acceptances %+v: %v", as, err)
			}
		})
	}
}
