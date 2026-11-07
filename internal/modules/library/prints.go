package library

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/tikhonp/shelf/internal/platform/jobs"
)

// FingerprintJob identifies an album by its audio.
const FingerprintJob = "library.fingerprint"

// SuggestionsTopic is the hub topic that says an album has new suggestions to
// ask for.
const SuggestionsTopic = "suggestions"

const (
	// printTracks is how many tracks an album is identified by. Each takes
	// seconds, and a release has to fit the whole album anyway.
	printTracks = 4
	// printScore is the lowest match AcoustID's answer counts.
	printScore = 0.7
)

// printHit is a release the album's audio points at, with the share of the
// tracks heard that are on it.
type printHit struct {
	ID    string  `json:"id"`
	Share float64 `json:"share"`
}

func printKey(row albumRow) string { return "acoustid:" + row.Folder + ":" + row.NDID }

// printed is the releases the album's audio points at. The first asking
// finds none and starts the job, because fingerprints take longer than an
// opening album should wait.
func (m *Module) printed(ctx context.Context, s *subject) []scored {
	if m.Prints == nil {
		return nil
	}
	status, body, ok := m.lookups.Get(ctx, printKey(s.row))
	if !ok && s.noPrints {
		return nil
	}
	if !ok {
		_, err := m.jobs.EnqueueNow(ctx, jobs.Request{Type: FingerprintJob, Payload: printPayload{Album: s.row.ID},
			CoalescingKey: strconv.FormatInt(s.row.ID, 10), CreatedBy: "web"})
		if err != nil {
			m.log.Warn("library: ask for a fingerprint", "album", s.row.ID, "error", err)
			return nil
		}
		s.fingerprinting = true
		return nil
	}
	if status != http.StatusOK {
		return nil
	}
	var hits []printHit
	if err := json.Unmarshal(body, &hits); err != nil {
		return nil
	}
	out := make([]scored, len(hits))
	for i, h := range hits {
		out[i] = scored{id: h.ID, p: pPrint + 0.35*h.Share}
	}
	return out
}

type printPayload struct {
	Album int64 `json:"album"`
}

// spread picks up to n tracks across an album, because its first tracks alone
// may be on every edition.
func spread[T any](ts []T, n int) []T {
	if len(ts) <= n {
		return ts
	}
	out := make([]T, n)
	for i := range n {
		out[i] = ts[i*len(ts)/n]
	}
	return out
}

// fingerprint listens to a few tracks and stores the releases they share. A
// failure stores nothing, so the next opening tries again.
func (m *Module) fingerprint(ctx context.Context, r *jobs.Run) error {
	var p printPayload
	if err := r.Payload(&p); err != nil {
		return jobs.Permanent(err)
	}
	if m.Prints == nil {
		return nil
	}
	s, err := m.readSubject(ctx, p.Album)
	if errors.Is(err, errNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	picks := spread(s.tracks, printTracks)
	count := map[string]int{}
	for _, t := range picks {
		pr, err := m.Prints.Fingerprint(ctx, m.abs(t.Path))
		if err != nil {
			return err
		}
		matches, err := m.Prints.Lookup(ctx, pr)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, match := range matches {
			if match.Score < printScore {
				continue
			}
			for _, id := range match.Releases {
				if !seen[id] {
					seen[id] = true
					count[id]++
				}
			}
		}
	}
	hits := make([]printHit, 0, len(count))
	for id, n := range count {
		hits = append(hits, printHit{ID: id, Share: float64(n) / float64(len(picks))})
	}
	slices.SortFunc(hits, func(a, b printHit) int { return cmp.Or(cmp.Compare(b.Share, a.Share), strings.Compare(a.ID, b.ID)) })
	body, err := json.Marshal(hits)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if len(hits) == 0 {
		// Stored as a miss, so it serves for a day and new prints get a chance.
		status = http.StatusNotFound
	}
	m.lookups.Put(ctx, printKey(s.row), status, body)
	r.Log().Info("AcoustID found %d releases for %s", len(hits), s.row.Folder)
	if m.hub != nil {
		m.hub.Publish(SuggestionsTopic, printPayload{Album: p.Album})
	}
	return nil
}
