package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/libraryport"
)

// CatchUpHistory returns a cursor only once the decisions up to it are
// stored, because Clear deletes through it.
func (m *Module) CatchUpHistory(ctx context.Context, through int64) (int64, error) {
	return m.reconcileThrough(ctx, through)
}

// The hub can drop events. Advance this cursor only after recording every decision.
func (m *Module) reconcileSavesThrough(ctx context.Context, lib libraryport.Port, through int64) (int64, error) {
	var cursor int64
	if err := m.db.R.GetContext(ctx, &cursor, `SELECT value FROM agents_state WHERE key='save_cursor'`); err != nil {
		return 0, err
	}
	for cursor < through {
		page, err := lib.SaveHistory(ctx, cursor, 50)
		if err != nil {
			return cursor, err
		}
		for _, change := range page.Changes {
			if change.ID > through {
				break
			}
			if err = m.acceptSaved(ctx, change); err != nil {
				return cursor, err
			}
		}
		next := min(page.Next, through)
		if len(page.Changes) == 0 {
			next = min(max(next, page.TrimmedThrough), through)
		}
		if next <= cursor {
			return cursor, nil
		}
		if _, err = m.db.W.ExecContext(ctx, `UPDATE agents_state SET value=? WHERE key='save_cursor'`, next); err != nil {
			return cursor, err
		}
		cursor = next
	}
	return cursor, nil
}

// A Save can write more than the proposal. Retain the physical keys that prove
// this operation was written, including aliases the Library matched to its field.
func savedOpKeys(op Op, base []BaseTrack, change libraryport.HistoryChange) map[string][]string {
	if op.Type != "tag" || len(op.Tracks) == 0 {
		return nil
	}
	paths := map[int64]string{}
	for _, b := range base {
		paths[b.TrackID] = b.Path
	}
	out := map[string][]string{}
	for _, target := range op.Tracks {
		path := paths[target.TrackID]
		for _, f := range change.Files {
			if !f.Written || f.TrackID != target.TrackID || path == "" || f.Path != path {
				continue
			}
			for _, tag := range f.Tags {
				if op.Field != "" && slices.Contains(tag.Fields, op.Field) || op.TagKey != "" && strings.EqualFold(tag.Key, op.TagKey) {
					if !slices.Equal(tag.Values, target.Values) {
						return nil
					}
					for _, key := range f.Keys {
						if key.Key == tag.Key {
							out[path] = append(out[path], key.Key)
						}
					}
				}
			}
			break
		}
		if len(out[path]) == 0 {
			return nil
		}
	}
	return out
}

// Older Save snapshots contain the full library change. Keep them in History,
// where they can be read with the other writes made by that Save.
type savePreview struct {
	libraryport.Preview
	ProposalFieldsOnly bool `json:"proposal_fields_only"`
}

func savePreviewFor(change libraryport.HistoryChange, keys map[string]map[string]bool) savePreview {
	p := emptyPreview()
	p.Album = libraryport.PreviewAlbum{Folder: change.Folder, Title: change.Title, Artist: change.Artist}
	for _, f := range change.Files {
		if !f.Written || len(keys[f.Path]) == 0 {
			continue
		}
		pf := libraryport.PreviewFile{TrackID: f.TrackID, Path: f.Path, Keys: []libraryport.KeyChange{}}
		for _, key := range f.Keys {
			if keys[f.Path][key.Key] {
				pf.Keys = append(pf.Keys, key)
			}
		}
		p.Files = append(p.Files, pf)
	}
	sum := sha256.Sum256([]byte(mustJSON(p)))
	p.Digest = hex.EncodeToString(sum[:])
	return savePreview{Preview: p, ProposalFieldsOnly: true}
}

func (m *Module) acceptSaved(ctx context.Context, change libraryport.HistoryChange) error {
	if change.Kind != "save" || change.Origin != "" || change.Truncated {
		return nil
	}
	// Only proposals that target a file this Save wrote can match it, so most
	// Saves open no write transaction at all.
	paths := []string{}
	for _, f := range change.Files {
		if f.Written {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	q, args, err := sqlx.In(`SELECT `+proposalCols+` FROM agents_proposals WHERE pending>0 AND withdrawn_at IS NULL
		AND id IN (SELECT proposal_id FROM agents_targets WHERE path IN (?)) ORDER BY id`, paths)
	if err != nil {
		return err
	}
	var proposals []proposalRow
	if err := m.db.R.SelectContext(ctx, &proposals, q, args...); err != nil {
		return err
	}
	for _, p := range proposals {
		changed := false
		err := m.db.Write(ctx, func(tx *sqlx.Tx) error {
			current, err := getProposal(ctx, tx, p.ID)
			if err != nil {
				return err
			}
			if current.withdrawn() {
				return nil
			}
			p = current
			r, err := getRevision(ctx, tx, p.ID, p.Head)
			if err != nil {
				return err
			}
			at, err := time.Parse(time.RFC3339Nano, change.CreatedAt)
			if err != nil {
				return err
			}
			if at.Before(r.CreatedAt.Time) {
				return nil
			}
			rows, err := getOps(ctx, tx, p.ID)
			if err != nil {
				return err
			}
			ops := []Op{}
			ids := []string{}
			keys := map[string]map[string]bool{}
			for _, op := range r.ops() {
				if rows[op.ID].Decision != "pending" {
					continue
				}
				matches := savedOpKeys(op, r.base(), change)
				if matches == nil {
					continue
				}
				ops = append(ops, op)
				ids = append(ids, op.ID)
				for path, names := range matches {
					if keys[path] == nil {
						keys[path] = map[string]bool{}
					}
					for _, key := range names {
						keys[path][key] = true
					}
				}
			}
			if len(ids) == 0 {
				return nil
			}
			slices.Sort(ids)
			preview := savePreviewFor(change, keys)
			result, err := tx.ExecContext(ctx, `INSERT INTO agents_acceptances(proposal_id,revision,op_ids,idem_key,digest,preview,change_id,author_id,author,created_at,via_save) VALUES(?,?,?,?,?,?,?,?,?,?,1)`, p.ID, p.Head, mustJSON(ids), "save:"+itoa(change.ID)+":proposal:"+itoa(p.ID), preview.Digest, mustJSON(preview), change.ID, change.AuthorID, change.Author, m.now())
			if err != nil {
				return err
			}
			aid, err := result.LastInsertId()
			if err != nil {
				return err
			}
			for _, id := range ids {
				if _, err = tx.ExecContext(ctx, `UPDATE agents_ops SET decision='accepted',decided_rev=?,acceptance_id=?,decided_at=?,decided_by_id=?,decided_by=? WHERE proposal_id=? AND op_id=?`, p.Head, aid, m.now(), change.AuthorID, change.Author, p.ID, id); err != nil {
					return err
				}
			}
			if err = setPending(ctx, tx, p.ID, m.now()); err != nil {
				return err
			}
			_, err = m.insertEvent(ctx, tx, event{Kind: "accepted", Proposal: p.ID, Revision: p.Head, Acceptance: aid, ActorKind: "user", ActorID: change.AuthorID, Actor: change.Author, Data: map[string]any{"acceptance_id": aid, "change_id": change.ID, "ops": ids, "revision": p.Head, "values": ops}})
			changed = err == nil
			return err
		})
		if err != nil {
			return err
		}
		if changed {
			m.publish(p.ID)
		}
	}
	return nil
}
