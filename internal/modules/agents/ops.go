package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/tikhonp/shelf/internal/libraryport"
)

// Limits of one revision.
const (
	maxOps        = 30
	maxEdits      = 500
	maxValues     = 100
	maxValueLen   = 2000
	maxLyricsLen  = 64 << 10
	maxEvidence   = 20
	maxRequires   = 30
	maxTitle      = 200
	maxReason     = 4000
	maxUncertain  = 2000
	maxBody       = 4000
	maxGroup      = 100
	maxClient     = 200
	maxAlbumsPage = 100
)

// Op is one allowlisted operation of a proposal: a tag edit over some
// tracks, or the album's cover.
type Op struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Field      string    `json:"field,omitempty"`
	TagKey     string    `json:"tag_key,omitempty"`
	Tracks     []OpTrack `json:"tracks,omitempty"`
	Anchor     string    `json:"anchor,omitempty"`
	Action     string    `json:"action,omitempty"`
	Image      string    `json:"image,omitempty"`
	EveryTrack bool      `json:"every_track,omitempty"`
	Requires   []string  `json:"requires,omitempty"`
}

// OpTrack is one track's new values. Empty values remove the tag.
type OpTrack struct {
	TrackID int64    `json:"track_id"`
	Values  []string `json:"values"`
}

// BaseTrack is a track revision a revision was prepared against.
type BaseTrack struct {
	TrackID  int64  `json:"track_id"`
	Path     string `json:"path,omitempty"`
	Revision string `json:"revision"`
}

// Evidence is a pointer the reviewer can follow. Shelf never fetches a link.
type Evidence struct {
	Kind   string `json:"kind"`
	Source string `json:"source,omitempty"`
	Field  string `json:"field,omitempty"`
	Anchor string `json:"anchor,omitempty"`
	Key    string `json:"key,omitempty"`
	Change int64  `json:"change,omitempty"`
	Event  int64  `json:"event,omitempty"`
	Text   string `json:"text,omitempty"`
	URL    string `json:"url,omitempty"`
}

var (
	opIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,47}$`)
	hashPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	fieldPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

func isLyrics(o Op) bool {
	return strings.Contains(strings.ToLower(o.Field), "lyrics") || strings.Contains(strings.ToUpper(o.TagKey), "LYRICS")
}

func (o Op) slot() string {
	if o.Field != "" {
		return "field:" + o.Field
	}
	return "key:" + strings.ToUpper(o.TagKey)
}

// normalize gives every empty value list a non-nil slice, so JSON keeps [].
func normalize(ops []Op) {
	for i := range ops {
		for j := range ops[i].Tracks {
			if ops[i].Tracks[j].Values == nil {
				ops[i].Tracks[j].Values = []string{}
			}
		}
	}
}

func checkEvidence(ev []Evidence) error {
	if len(ev) > maxEvidence {
		return invalid("at most %d evidence items", maxEvidence)
	}
	for i, e := range ev {
		switch e.Kind {
		case "suggestion", "memory", "change", "event", "note", "link":
		default:
			return invalid("evidence %d: kind is suggestion, memory, change, event, note or link", i)
		}
		for _, s := range []string{e.Source, e.Field, e.Anchor, e.Key, e.Text, e.URL} {
			if len(s) > 2000 {
				return invalid("evidence %d: a value over 2000 bytes", i)
			}
		}
	}
	return nil
}

// checkOps checks the shape of a revision's ops against the album: IDs,
// targets, limits, the one-op-per-slot rule and requires. decided are op
// IDs that stay in the list unchanged; they count toward the slots.
func checkOps(ops []Op, tracks map[int64]libraryport.Track) error {
	if len(ops) == 0 {
		return invalid("a proposal needs at least one op")
	}
	if len(ops) > maxOps {
		return invalid("at most %d ops in a revision", maxOps)
	}
	ids := map[string]bool{}
	slots := map[string]string{}
	edits, covers := 0, 0
	for _, o := range ops {
		if !opIDPattern.MatchString(o.ID) {
			return invalid("op id %q must match %s", o.ID, opIDPattern)
		}
		if ids[o.ID] {
			return invalid("op id %q is used twice", o.ID)
		}
		ids[o.ID] = true
		switch o.Type {
		case "tag":
			if (o.Field == "") == (o.TagKey == "") {
				return invalid("op %q needs exactly one of field and tag_key", o.ID)
			}
			if o.Field != "" && !fieldPattern.MatchString(o.Field) {
				return invalid("op %q: field %q is not a field name", o.ID, o.Field)
			}
			if o.TagKey != "" && (len(o.TagKey) > 128 || strings.ContainsAny(o.TagKey, "=\x00\n")) {
				return invalid("op %q: tag_key %q is not a tag key", o.ID, o.TagKey)
			}
			if o.Action != "" || o.Image != "" || o.EveryTrack {
				return invalid("op %q: a tag op has no action, image or every_track", o.ID)
			}
			if len(o.Anchor) > 64 {
				return invalid("op %q: anchor is a MusicBrainz release ID", o.ID)
			}
			if len(o.Tracks) == 0 {
				return invalid("op %q needs at least one track", o.ID)
			}
			limit := maxValueLen
			if isLyrics(o) {
				limit = maxLyricsLen
			}
			for _, t := range o.Tracks {
				if _, ok := tracks[t.TrackID]; !ok {
					return conflict("stale_base", fmt.Sprintf("op %q: track %d is not on the album", o.ID, t.TrackID))
				}
				slot := fmt.Sprintf("%d/%s", t.TrackID, o.slot())
				if other, ok := slots[slot]; ok {
					return invalid("ops %q and %q both change %s of track %d", other, o.ID, strings.TrimPrefix(strings.TrimPrefix(o.slot(), "field:"), "key:"), t.TrackID)
				}
				slots[slot] = o.ID
				if len(t.Values) > maxValues {
					return invalid("op %q: at most %d values per track", o.ID, maxValues)
				}
				for _, v := range t.Values {
					if len(v) > limit || !utf8.ValidString(v) {
						return invalid("op %q: a value over %d bytes or not UTF-8", o.ID, limit)
					}
				}
				edits++
			}
		case "cover":
			covers++
			if covers > 1 {
				return invalid("at most one cover op")
			}
			if o.Field != "" || o.TagKey != "" || len(o.Tracks) > 0 || o.Anchor != "" {
				return invalid("op %q: a cover op has no field, tag_key, tracks or anchor", o.ID)
			}
			switch o.Action {
			case "replace":
				if !hashPattern.MatchString(o.Image) {
					return invalid("op %q: image is the cover ID an upload or offer returned", o.ID)
				}
			case "remove":
				if o.Image != "" || o.EveryTrack {
					return invalid("op %q: a removal has no image or every_track", o.ID)
				}
			default:
				return invalid("op %q: action is replace or remove", o.ID)
			}
		default:
			return invalid("op %q: type is tag or cover", o.ID)
		}
	}
	if edits > maxEdits {
		return invalid("at most %d track edits in a revision", maxEdits)
	}
	return checkRequires(ops)
}

// checkRequires refuses an unknown or self reference and a cycle. A
// requirement points one way: selecting the op selects what it requires.
func checkRequires(ops []Op) error {
	byID := map[string]Op{}
	for _, o := range ops {
		byID[o.ID] = o
	}
	for _, o := range ops {
		if len(o.Requires) > maxRequires {
			return invalid("op %q: too many requires", o.ID)
		}
		seen := map[string]bool{}
		for _, r := range o.Requires {
			if r == o.ID {
				return invalid("op %q requires itself", o.ID)
			}
			if _, ok := byID[r]; !ok {
				return invalid("op %q requires %q, which is not in this revision", o.ID, r)
			}
			if seen[r] {
				return invalid("op %q requires %q twice", o.ID, r)
			}
			seen[r] = true
		}
	}
	// Depth-first search for a cycle.
	state := map[string]int{}
	var visit func(id string, path []string) error
	visit = func(id string, path []string) error {
		switch state[id] {
		case 1:
			return invalid("requires form a cycle: %s", strings.Join(append(path, id), " -> "))
		case 2:
			return nil
		}
		state[id] = 1
		for _, r := range byID[id].Requires {
			if err := visit(r, append(path, id)); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id, nil); err != nil {
			return err
		}
	}
	return nil
}

// closure is ids with everything they require, sorted.
func closure(ops []Op, ids []string) []string {
	byID := map[string]Op{}
	for _, o := range ops {
		byID[o.ID] = o
	}
	seen := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, r := range byID[id].Requires {
			walk(r)
		}
	}
	for _, id := range ids {
		walk(id)
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// planRequest turns ops into the library's Save request. Tracks keep the
// order they first appear in.
func planRequest(ops []Op, confirm []string) libraryport.PlanRequest {
	r := libraryport.PlanRequest{Confirm: append([]string{}, confirm...)}
	idx := map[int64]int{}
	for _, o := range ops {
		switch o.Type {
		case "tag":
			for _, t := range o.Tracks {
				i, ok := idx[t.TrackID]
				if !ok {
					i = len(r.Files)
					idx[t.TrackID] = i
					r.Files = append(r.Files, libraryport.PlanFile{TrackID: t.TrackID})
				}
				e := libraryport.Edit{Values: append([]string{}, t.Values...), Anchor: o.Anchor}
				if o.Field != "" {
					e.Field = o.Field
				} else {
					e.Key = o.TagKey
				}
				r.Files[i].Edits = append(r.Files[i].Edits, e)
			}
		case "cover":
			r.Cover = &libraryport.PlanCover{Image: o.Image, Remove: o.Action == "remove", EveryTrack: o.EveryTrack}
		}
	}
	return r
}

func pick(ops []Op, ids []string) []Op {
	out := []Op{}
	for _, o := range ops {
		if slices.Contains(ids, o.ID) {
			out = append(out, o)
		}
	}
	return out
}

func opByID(ops []Op, id string) (Op, bool) {
	for _, o := range ops {
		if o.ID == id {
			return o, true
		}
	}
	return Op{}, false
}

// fingerprint identifies what an op does, by file path and values, so the
// same change filed twice is caught across proposals.
func fingerprint(o Op, paths map[int64]string) string {
	type tr struct {
		Path   string   `json:"path"`
		Values []string `json:"values"`
	}
	v := struct {
		Type       string `json:"type"`
		Slot       string `json:"slot"`
		Image      string `json:"image"`
		EveryTrack bool   `json:"every_track"`
		Tracks     []tr   `json:"tracks"`
	}{Type: o.Type, Image: o.Image, EveryTrack: o.EveryTrack, Tracks: []tr{}}
	if o.Type == "cover" {
		v.Slot = "action:" + o.Action
	} else {
		v.Slot = o.slot()
	}
	if o.Type == "cover" {
		for _, p := range paths {
			v.Tracks = append(v.Tracks, tr{p, []string{}})
		}
	}
	for _, t := range o.Tracks {
		vals := t.Values
		if vals == nil {
			vals = []string{}
		}
		v.Tracks = append(v.Tracks, tr{paths[t.TrackID], vals})
	}
	sort.Slice(v.Tracks, func(i, j int) bool { return v.Tracks[i].Path < v.Tracks[j].Path })
	h := sha256.Sum256([]byte(mustJSON(v)))
	return hex.EncodeToString(h[:])
}

func sameOp(a, b Op) bool {
	normalize([]Op{a, b})
	return mustJSON(a) == mustJSON(b)
}

func emptyPreview() libraryport.Preview {
	return libraryport.Preview{Files: []libraryport.PreviewFile{}, Folder: []libraryport.PreviewCover{}, Warnings: []libraryport.Warning{}}
}

func refused(r *libraryport.Refusal) *apiError {
	return fail(http.StatusUnprocessableEntity, "refused", r.Error, "refusal", r)
}

func decodeOps(s string) []Op {
	var ops []Op
	_ = json.Unmarshal([]byte(s), &ops)
	normalize(ops)
	return ops
}

func requestWithBase(ops []Op, confirm []string, base []BaseTrack) libraryport.PlanRequest {
	r := planRequest(ops, confirm)
	if r.Cover != nil && len(r.Files) == 0 && len(base) > 0 {
		r.Files = []libraryport.PlanFile{{TrackID: base[0].TrackID}}
	}
	return r
}
