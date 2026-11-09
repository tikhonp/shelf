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

// The hub can drop events. Advance this cursor only after recording every decision.
func (m *Module) reconcileSaves(ctx context.Context, lib libraryport.Port) error {
	var cursor int64
	if err := m.db.R.GetContext(ctx, &cursor, `SELECT value FROM agents_state WHERE key='save_cursor'`); err != nil {
		return err
	}
	for {
		page, err := lib.SaveHistory(ctx, cursor, 50)
		if err != nil {
			return err
		}
		for _, change := range page.Changes {
			if err = m.acceptSaved(ctx, change); err != nil {
				return err
			}
		}
		next := page.Next
		if len(page.Changes) == 0 {
			next = max(next, page.TrimmedThrough)
		}
		if next <= cursor {
			return nil
		}
		if _, err = m.db.W.ExecContext(ctx, `UPDATE agents_state SET value=? WHERE key='save_cursor'`, next); err != nil {
			return err
		}
		cursor = next
	}
}

func savedOp(op Op, base []BaseTrack, change libraryport.HistoryChange) bool {
	if op.Type != "tag" || len(op.Tracks) == 0 {
		return false
	}
	paths := map[int64]string{}
	for _, b := range base {
		paths[b.TrackID] = b.Path
	}
	for _, target := range op.Tracks {
		found := false
		for _, f := range change.Files {
			if !f.Written || f.TrackID != target.TrackID || paths[target.TrackID] == "" || f.Path != paths[target.TrackID] {
				continue
			}
			matched := false
			touched := false
			for _, tag := range f.Tags {
				if op.Field != "" && slices.Contains(tag.Fields, op.Field) || op.TagKey != "" && strings.EqualFold(tag.Key, op.TagKey) {
					if !slices.Equal(tag.Values, target.Values) {
						return false
					}
					matched = true
					for _, key := range f.Keys {
						if key.Key == tag.Key {
							touched = true
						}
					}
				}
			}
			if !matched || !touched {
				return false
			}
			found = true
		}
		if !found {
			return false
		}
	}
	return true
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
			for _, op := range r.ops() {
				if rows[op.ID].Decision == "pending" && savedOp(op, r.base(), change) {
					ops = append(ops, op)
					ids = append(ids, op.ID)
				}
			}
			if len(ids) == 0 {
				return nil
			}
			slices.Sort(ids)
			preview := emptyPreview()
			preview.Album = libraryport.PreviewAlbum{Folder: change.Folder, Title: change.Title, Artist: change.Artist}
			for _, f := range change.Files {
				preview.Files = append(preview.Files, f.PreviewFile)
			}
			sum := sha256.Sum256([]byte(mustJSON(preview)))
			preview.Digest = hex.EncodeToString(sum[:])
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
