package library

import (
	"cmp"
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tikhonp/shelf/internal/source"
)

// Record is the downloads module's record of an item and the release folder
// it went to. main converts the downloads service's records to these.
type Record struct {
	Source, Kind, ItemID string
	// Folder is the release folder inside the music folder.
	Folder     string
	DownloadID int64
	// Quality is what was delivered, such as "FLAC 24/96".
	Quality string
	// Imported is a row from an older tool's history.
	Imported bool
	At       time.Time
}

// RecordsReader lists the records. The downloads service is it.
type RecordsReader func(ctx context.Context) ([]Record, error)

// How shelf knows an album's source, from the surest to the least sure.
const (
	byDownload = "download" // a download of shelf's wrote the folder
	byImport   = "import"   // an older tool's history names the folder
	byFile     = "file"     // the folder's shelf.txt, with no record left
	byFolder   = "folder"   // the album sits in the folder a source writes into
)

// AlbumSource is where an album came from.
type AlbumSource struct {
	// ID is a source's ID, empty when nothing tells.
	ID   string `json:"id"`
	Name string `json:"name"`
	// By is how shelf knows: "download", "import", "file" or "folder".
	By     string `json:"by,omitempty"`
	Kind   string `json:"kind,omitempty"`
	ItemID string `json:"item_id,omitempty"`
	// DownloadID is shelf's download, for a link to it.
	DownloadID int64 `json:"download_id,omitempty"`
	// Quality is what the source delivered.
	Quality string `json:"quality,omitempty"`
	// At is when the download finished, RFC 3339 in UTC. An import has none,
	// because the older tools kept no date.
	At string `json:"at,omitempty"`
	// Link is the item's page on the source's site, from shelf.txt.
	Link string `json:"link,omitempty"`
	// Recorded is the folder the record names, when the album was moved
	// out of it.
	Recorded string `json:"recorded,omitempty"`
}

// sourceInfo is what the walk knows of each registered source.
type sourceInfo struct{ id, name, folder string }

func sourcesOf(reg *source.Registry) []sourceInfo {
	if reg == nil {
		return nil
	}
	var out []sourceInfo
	for _, s := range reg.All() {
		si := sourceInfo{id: s.ID(), name: s.Name()}
		if f, ok := s.(source.Folderer); ok {
			si.folder = strings.Trim(f.Folder(), "/")
		}
		out = append(out, si)
	}
	return out
}

// tellSources sets each album's source. The records come first, by the
// album's folder, then by its folder's name when the owner moved it. Then
// the folder's shelf.txt, then the source folder the album sits in. blocks
// are the shelf.txt blocks by folder.
func tellSources(albums []*album, recs []Record, blocks map[string][]provenance, sources []sourceInfo) {
	names := map[string]sourceInfo{}
	ids := map[string]sourceInfo{}
	for _, s := range sources {
		names[strings.ToLower(s.name)] = s
		ids[s.id] = s
	}
	inFolder := map[string][]Record{}
	for _, r := range recs {
		inFolder[r.Folder] = append(inFolder[r.Folder], r)
	}
	// A moved folder keeps its name. A name counts only when one album and
	// one record folder have it, and no album is left in the record's
	// folder.
	albumNames := map[string]int{}
	held := map[string]bool{}
	for _, a := range albums {
		albumNames[path.Base(a.Folder)]++
		held[a.Folder] = true
	}
	byName := map[string][]string{}
	for f := range inFolder {
		if !held[f] {
			byName[path.Base(f)] = append(byName[path.Base(f)], f)
		}
	}
	for _, a := range albums {
		a.Source = AlbumSource{}
		if rs := inFolder[a.Folder]; rs != nil {
			a.Source = fromRecord(pickRecord(rs), ids)
		} else if fs := byName[path.Base(a.Folder)]; len(fs) == 1 && albumNames[path.Base(a.Folder)] == 1 {
			a.Source = fromRecord(pickRecord(inFolder[fs[0]]), ids)
			a.Source.Recorded = fs[0]
		}
		if b := pickProvenance(blocks[a.Folder]); b != nil {
			if s, ok := names[strings.ToLower(b.Source)]; ok {
				switch {
				case a.Source.ID == "":
					a.Source = fromBlock(b, s)
				case a.Source.ID == s.id && a.Source.ItemID == b.SourceID:
					a.Source.Link = b.Link
				}
			}
		}
		if a.Source.ID == "" {
			for _, s := range sources {
				if s.folder != "" && s.folder != "." && strings.HasPrefix(a.Folder, s.folder+"/") {
					a.Source = AlbumSource{ID: s.id, Name: s.name, By: byFolder}
					break
				}
			}
		}
	}
}

// pickRecord is the folder's album or playlist, else its latest track: a
// folder that got a single track and later its album came from the album.
func pickRecord(rs []Record) Record {
	return slices.MaxFunc(rs, func(a, b Record) int {
		whole := func(r Record) int {
			if r.Kind == string(source.KindTrack) {
				return 0
			}
			return 1
		}
		return cmp.Or(cmp.Compare(whole(a), whole(b)), a.At.Compare(b.At), cmp.Compare(a.DownloadID, b.DownloadID))
	})
}

func fromRecord(r Record, ids map[string]sourceInfo) AlbumSource {
	out := AlbumSource{ID: r.Source, Name: r.Source, By: byDownload, Kind: r.Kind, ItemID: r.ItemID,
		DownloadID: r.DownloadID, Quality: r.Quality}
	if s, ok := ids[r.Source]; ok {
		out.Name = s.name
	}
	if r.Imported {
		out.By = byImport
	} else if !r.At.IsZero() {
		out.At = r.At.UTC().Format(time.RFC3339)
	}
	return out
}

func fromBlock(b *provenance, s sourceInfo) AlbumSource {
	out := AlbumSource{ID: s.id, Name: s.name, By: byFile, Kind: b.Kind, ItemID: b.SourceID,
		DownloadID: b.DownloadID, Quality: b.Delivered, Link: b.Link}
	if t, err := time.Parse(time.RFC3339, b.Finished); err == nil {
		out.At = t.UTC().Format(time.RFC3339)
	}
	return out
}

// readBlocks reads the shelf.txt of each folder that has one. A file that
// can't be read tells nothing.
func readBlocks(root string, folders map[string]bool) map[string][]provenance {
	out := map[string][]provenance{}
	for f := range folders {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f), "shelf.txt"))
		if err == nil {
			out[f] = parseProvenance(b)
		}
	}
	return out
}
