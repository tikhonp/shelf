package downloads

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tikhonp/shelf/internal/platform/obs"
	"github.com/tikhonp/shelf/internal/source"
)

// ProvenanceFile is the provenance file's name in a release folder.
const ProvenanceFile = "shelf.txt"

// header is a block's first line, which names its download.
func header(id int64) string { return "Music Shelf download " + strconv.FormatInt(id, 10) }

// provenance renders d's block, in the format of docs/provenance.md.
func (s *Service) provenance(d Download, p payload, tracks []Track) string {
	var b strings.Builder
	line := func(key, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\n", key, oneLine(value))
		}
	}
	b.WriteString(header(d.ID) + "\n")
	if u := s.cfg.PublicURL; u != nil {
		line("Shelf link", downloadLink(u, d.ID))
	}
	srcName, formatWords := d.Ref.Source, d.Format
	if src, ok := s.sources.Get(d.Ref.Source); ok {
		srcName = src.Name()
		if dl, ok := src.(source.Downloader); ok {
			if f, ok := source.FindFormat(dl.Formats(), d.Format); ok {
				formatWords = f.Description
			}
		}
	}
	facts := itemFacts{}
	if p.Item != nil {
		facts = *p.Item
	}
	line("Source", srcName)
	line("Kind", string(d.Ref.Kind))
	line("Source ID", d.Ref.ID)
	line("Source link", facts.URL)
	line("Title", d.Title)
	line("Artist", d.Artist)
	line("Label", facts.Label)
	line("Release date", facts.ReleaseDate)
	if d.Ref.Kind == source.KindAlbum {
		line("UPC", facts.UPC)
	}
	if d.Ref.Kind == source.KindTrack {
		line("ISRC", facts.ISRC)
	}
	line("Requested format", formatWords)
	if q := delivered(tracks); !q.IsZero() {
		line("Delivered", q.Words())
	}
	line("Requested by", requestedBy(d))
	loc := s.cfg.TZ
	if loc == nil {
		loc = time.UTC
	}
	if !d.StartedAt.IsZero() {
		line("Started", d.StartedAt.In(loc).Format(time.RFC3339))
	}
	line("Finished", d.FinishedAt.In(loc).Format(time.RFC3339))
	tools := "shelf " + obs.AppVersion
	noun := "track"
	if p.Result != nil {
		if p.Result.Tool != "" {
			tools += ", " + p.Result.Tool
		}
		if p.Result.TrackNoun != "" {
			noun = p.Result.TrackNoun
		}
	}
	line("Tools", tools)
	b.WriteString("Tracks:\n")
	for _, t := range tracks {
		cols := []string{strconv.Itoa(t.Position)}
		if t.Path != "" {
			cols = append(cols, path.Base(filepath.ToSlash(t.Path)))
		} else {
			cols = append(cols, oneLine(t.Title))
		}
		if t.ItemID != "" {
			cols = append(cols, noun+" "+t.ItemID)
		}
		if t.ISRC != "" {
			cols = append(cols, "ISRC "+t.ISRC)
		}
		if q := t.quality(); !q.IsZero() && t.Status != "failed" {
			cols = append(cols, q.Short())
		}
		cols = append(cols, t.Status)
		b.WriteString("  " + strings.Join(cols, "  ") + "\n")
	}
	return b.String()
}

// downloadLink is the download's page in the UI.
func downloadLink(base *url.URL, id int64) string {
	return strings.TrimRight(base.String(), "/") + "/downloads/" + strconv.FormatInt(id, 10)
}

func requestedBy(d Download) string {
	switch d.Origin {
	case OriginWeb:
		return "Web"
	case OriginCLI:
		return "CLI"
	case OriginTelegram:
		who := "Telegram"
		if d.OriginRef.Username != "" {
			who += " @" + d.OriginRef.Username
		}
		if d.OriginRef.UserID != 0 {
			who += ", user " + strconv.FormatInt(d.OriginRef.UserID, 10)
		}
		return who
	}
	return d.Origin
}

// oneLine keeps a catalog value from starting a line of its own.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// AppendProvenance appends block to <folder>/shelf.txt, unless a block for
// the same download is there already, which a resumed finish step would
// otherwise add twice. Blocks are separated by one empty line. A new file
// gets mode 0644; an existing one keeps its owner and mode.
func AppendProvenance(folder string, id int64, block string) error {
	file := filepath.Join(folder, ProvenanceFile)
	old, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if hasBlock(old, id) {
		return nil
	}
	var out bytes.Buffer
	if len(old) > 0 {
		if !bytes.HasSuffix(old, []byte("\n")) {
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
	}
	out.WriteString(block)
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(out.Bytes()); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// hasBlock reports whether data has a block whose first line names id.
func hasBlock(data []byte, id int64) bool {
	want := header(id)
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	start := true
	for sc.Scan() {
		l := strings.TrimRight(sc.Text(), "\r")
		if start && l == want {
			return true
		}
		start = l == ""
	}
	return false
}
