package downloads

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tikhonp/shelf/internal/source"
)

func TestBusyWhileTheSourceWrites(t *testing.T) {
	r := newRig(t)
	reached, release := make(chan struct{}), make(chan struct{})
	r.src.Hook = func(_ context.Context, _ source.Ref, pos int) error {
		if pos == 2 {
			close(reached)
			<-release
		}
		return nil
	}
	r.start()
	d := r.queue(QueueRequest{Ref: album("album-1")})
	<-reached
	det := r.get(d.ID)
	dir := filepath.Join(r.cfg.MusicDir, "fake")
	folder := filepath.Join(dir, det.Artist+" - "+det.Title)
	for p, want := range map[string]bool{
		folder: true, filepath.Join(folder, "CD 2"): true,
		dir: false, folder + " (Deluxe)": false, filepath.Join(r.cfg.MusicDir, "jazz", "Kind of Blue"): false,
	} {
		if got := r.svc.Busy(p); got != want {
			t.Errorf("Busy(%s) = %v", p, got)
		}
	}
	close(release)
	r.waitStatus(d.ID, Done)
	if r.svc.Busy(folder) {
		t.Fatal("still busy after the download")
	}
}

func TestWorkingFolderOutsideTheMusicFolder(t *testing.T) {
	r := newRig(t)
	r.svc.work(1, "../elsewhere")
	r.svc.work(1, "/abs")
	if r.svc.Busy(filepath.Join(filepath.Dir(r.cfg.MusicDir), "elsewhere")) || r.svc.Busy(r.cfg.MusicDir) {
		t.Fatal("a folder outside the music folder counts")
	}
}
