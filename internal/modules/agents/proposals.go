package agents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/libraryport"
)

const proposalBodyLimit = 2 << 20

type textFields struct {
	Title         string      `json:"title"`
	Reason        string      `json:"reason"`
	Uncertainties string      `json:"uncertainties"`
	Confidence    *int        `json:"confidence"`
	Evidence      []Evidence  `json:"evidence"`
	Base          []BaseTrack `json:"base"`
	Ops           []Op        `json:"ops"`
}

type createBody struct {
	RunID          int64  `json:"run_id"`
	AlbumID        int64  `json:"album_id"`
	Group          string `json:"group"`
	FollowUpOf     *int64 `json:"follow_up_of"`
	FollowUpReason string `json:"follow_up_reason"`
	textFields
}

type reviseBody struct {
	RunID          int64 `json:"run_id"`
	ParentRevision int   `json:"parent_revision"`
	textFields
}

func checkText(t *textFields) error {
	if n := utf8.RuneCountInString(t.Title); n < 1 || n > maxTitle {
		return invalid("title has 1 to %d characters", maxTitle)
	}
	if n := utf8.RuneCountInString(t.Reason); n < 1 || n > maxReason {
		return invalid("reason has 1 to %d characters", maxReason)
	}
	if utf8.RuneCountInString(t.Uncertainties) > maxUncertain {
		return invalid("uncertainties has at most %d characters", maxUncertain)
	}
	if t.Confidence != nil && (*t.Confidence < 0 || *t.Confidence > 100) {
		return invalid("confidence is 0 to 100")
	}
	if t.Evidence == nil {
		t.Evidence = []Evidence{}
	}
	normalize(t.Ops)
	return checkEvidence(t.Evidence)
}

var errNoLibrary = fail(http.StatusServiceUnavailable, "unavailable", "the library is not available")

func (m *Module) lib() (libraryport.Port, error) {
	l := m.library()
	if l == nil {
		return nil, errNoLibrary
	}
	return l, nil
}

// prepared is a revision checked against the files as they are now.
type prepared struct {
	album   libraryport.Album
	ops     []Op
	pending []string
	preview libraryport.Preview
	digests map[string]string
	fps     map[string]string
	base    []BaseTrack
	targets []string
	images  []string
}

// prepare checks ops against album and plans them. pending are the op IDs
// the revision leaves pending; only they are planned. With checkBase the
// given base must match the current revision of every track the pending
// ops touch, and of every track for a cover op.
func (m *Module) prepare(ctx context.Context, lib libraryport.Port, album libraryport.Album, ops []Op, pending []string, base []BaseTrack, checkBase bool) (prepared, error) {
	p := prepared{album: album, ops: ops, pending: pending, digests: map[string]string{}, fps: map[string]string{}}
	tracks := map[int64]libraryport.Track{}
	paths := map[int64]string{}
	for _, t := range album.Tracks {
		tracks[t.ID] = t
		paths[t.ID] = t.Path
	}
	validationTracks := map[int64]libraryport.Track{}
	for id, t := range tracks {
		validationTracks[id] = t
	}
	for _, b := range base {
		if _, ok := validationTracks[b.TrackID]; !ok {
			validationTracks[b.TrackID] = libraryport.Track{ID: b.TrackID, Path: b.Path, Revision: b.Revision}
		}
		if _, ok := paths[b.TrackID]; !ok {
			paths[b.TrackID] = b.Path
		}
	}
	for _, o := range pick(ops, pending) {
		for _, tr := range o.Tracks {
			if _, ok := tracks[tr.TrackID]; !ok {
				return p, conflict("stale_base", "a pending operation targets a file outside the album")
			}
		}
	}
	if err := checkOps(ops, validationTracks); err != nil {
		return p, err
	}
	pendOps := pick(ops, pending)
	need := map[int64]bool{}
	cover := false
	for _, o := range pendOps {
		if o.Type == "cover" {
			cover = true
		}
		for _, t := range o.Tracks {
			need[t.TrackID] = true
		}
	}
	if cover {
		for id := range tracks {
			need[id] = true
		}
	}
	if checkBase {
		current := make([]BaseTrack, 0, len(album.Tracks))
		for _, t := range album.Tracks {
			current = append(current, BaseTrack{TrackID: t.ID, Path: t.Path, Revision: t.Revision})
		}
		given := map[int64]string{}
		for _, b := range base {
			t, ok := tracks[b.TrackID]
			if !ok {
				if !need[b.TrackID] && paths[b.TrackID] != "" {
					continue
				}
				return p, conflict("stale_base", fmt.Sprintf("track %d is no longer on the album; read the album again", b.TrackID), "current", current)
			}
			if _, dup := given[b.TrackID]; dup {
				return p, invalid("base names track %d twice", b.TrackID)
			}
			given[b.TrackID] = b.Revision
			if b.Revision != t.Revision {
				return p, conflict("stale_base", fmt.Sprintf("track %d changed since you read it; read the album again", b.TrackID), "current", current)
			}
		}
		ids := make([]int64, 0, len(need))
		for id := range need {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if _, ok := given[id]; !ok {
				return p, invalid("base lacks track %d, which the ops touch", id)
			}
		}
		for _, b := range base {
			p.base = append(p.base, BaseTrack{TrackID: b.TrackID, Path: paths[b.TrackID], Revision: b.Revision})
		}
		sort.Slice(p.base, func(i, j int) bool { return p.base[i].TrackID < p.base[j].TrackID })
	} else {
		p.base = base
	}
	if p.base == nil {
		p.base = []BaseTrack{}
	}
	targets := map[string]bool{}
	for _, o := range ops {
		for _, t := range o.Tracks {
			targets[paths[t.TrackID]] = true
		}
		if o.Type == "cover" {
			for _, t := range album.Tracks {
				targets[t.Path] = true
			}
		}
		p.fps[o.ID] = fingerprint(o, paths)
	}
	for t := range targets {
		p.targets = append(p.targets, t)
	}
	sort.Strings(p.targets)
	for _, o := range pendOps {
		if o.Type != "cover" || o.Action != "replace" {
			continue
		}
		img, err := lib.CoverInfo(ctx, o.Image)
		if errors.Is(err, libraryport.ErrNotFound) {
			return p, invalid("op %q: image %s is not in the cover store; upload it or materialize an offer first", o.ID, o.Image)
		}
		if err != nil {
			return p, err
		}
		if img.MIME != "image/jpeg" && img.MIME != "image/png" {
			return p, invalid("op %q: a cover is a JPEG or PNG", o.ID)
		}
		p.images = append(p.images, o.Image)
	}
	if len(pendOps) == 0 {
		p.preview = emptyPreview()
		return p, nil
	}
	plan, err := lib.Plan(ctx, requestWithBase(pendOps, nil, p.base))
	if err != nil {
		return p, err
	}
	if plan.Refusal != nil {
		return p, refused(plan.Refusal)
	}
	p.preview = plan.Preview
	p.images = append(p.images, previewImages(p.preview)...)

	for _, id := range pending {
		d, err := opDigest(ctx, lib, ops, pending, id, p.base)
		if err != nil {
			return p, err
		}
		p.digests[id] = d
	}
	return p, nil
}

// opDigest plans one op with the pending ops it requires. A refused op
// alone has the digest "".
func opDigest(ctx context.Context, lib libraryport.Port, ops []Op, pending []string, id string, base []BaseTrack) (string, error) {
	set := []string{}
	for _, r := range closure(ops, []string{id}) {
		if slices.Contains(pending, r) {
			set = append(set, r)
		}
	}
	plan, err := lib.Plan(ctx, requestWithBase(pick(ops, set), nil, base))
	if err != nil || plan.Refusal != nil {
		return "", err
	}
	return plan.Preview.Digest, nil
}

// currentAlbum is the album the proposal's files are on now. Files spread
// over albums, or lost, can't be reviewed against one album.
func (m *Module) currentAlbum(ctx context.Context, lib libraryport.Port, proposal int64) (libraryport.Album, error) {
	id, err := m.currentAlbumID(ctx, lib, proposal)
	if err != nil {
		return libraryport.Album{}, err
	}
	if id == 0 {
		return libraryport.Album{}, conflict("stale_base", "the proposal's files are no longer on one album")
	}
	a, err := lib.Album(ctx, id)
	if errors.Is(err, libraryport.ErrNotFound) {
		return a, conflict("stale_base", "the proposal's album is gone")
	}
	return a, err
}

func (m *Module) currentAlbumID(ctx context.Context, lib libraryport.Port, proposal int64) (int64, error) {
	p, err := getProposal(ctx, m.db.R, proposal)
	if err != nil {
		return 0, err
	}
	r, err := getRevision(ctx, m.db.R, proposal, p.Head)
	if err != nil {
		return 0, err
	}
	rows, err := getOps(ctx, m.db.R, proposal)
	if err != nil {
		return 0, err
	}
	wanted := map[int64]bool{}
	all := false
	for _, o := range r.ops() {
		if rows[o.ID].Decision != "pending" {
			continue
		}
		if o.Type == "cover" {
			all = true
		}
		for _, t := range o.Tracks {
			wanted[t.TrackID] = true
		}
	}
	paths := []string{}
	for _, b := range r.base() {
		if all || len(wanted) == 0 || wanted[b.TrackID] {
			paths = append(paths, b.Path)
		}
	}
	if len(paths) == 0 {
		return 0, nil
	}
	of, err := lib.AlbumsOf(ctx, paths)
	if err != nil {
		return 0, err
	}
	var id int64
	for _, p := range paths {
		a := of[p]
		if a == 0 || id != 0 && a != id {
			return 0, nil
		}
		id = a
	}
	return id, nil
}

// checkDuplicates refuses ops that repeat a pending or declined op of
// another proposal. A follow-up may repeat what its original declined.
func checkDuplicates(ctx context.Context, tx *sqlx.Tx, self, followUp int64, fps map[string]string, ids []string) error {
	for _, id := range ids {
		rows := []struct {
			Proposal int64  `db:"proposal_id"`
			Op       string `db:"op_id"`
			Decision string `db:"decision"`
		}{}
		if err := tx.SelectContext(ctx, &rows, `SELECT proposal_id, op_id, decision FROM agents_ops
			WHERE fingerprint = ? AND decision IN ('pending', 'declined') AND proposal_id != ? ORDER BY proposal_id`, fps[id], self); err != nil {
			return err
		}
		for _, r := range rows {
			if r.Decision == "declined" && followUp == r.Proposal {
				continue
			}
			msg := fmt.Sprintf("op %q repeats op %q of proposal %d, which is %s", id, r.Op, r.Proposal, r.Decision)
			if r.Decision == "declined" {
				msg += "; to reconsider it, set follow_up_of to that proposal with a follow_up_reason"
			}
			return conflict("duplicate", msg, "op", id, "proposal", r.Proposal, "proposal_op", r.Op, "decision", r.Decision)
		}
	}
	return nil
}

func (m *Module) pin(ctx context.Context, tx *sqlx.Tx, lib libraryport.Port, proposal int64, images []string) error {
	if len(images) == 0 {
		return nil
	}
	err := lib.Pin(ctx, tx, holder(proposal), images)
	switch {
	case errors.Is(err, libraryport.ErrNotFound):
		return invalid("a cover image left the store; upload it again")
	case errors.Is(err, libraryport.ErrRefused):
		return fail(http.StatusUnprocessableEntity, "refused", "the cover image was refused")
	}
	return err
}

func holder(proposal int64) string { return "agents:proposal:" + itoa(proposal) }

func insertRevision(ctx context.Context, tx *sqlx.Tx, id int64, rev int, kind string, conn, run int64, authorID, author string,
	t textFields, p prepared, now any) error {
	var conf any
	if t.Confidence != nil {
		conf = *t.Confidence
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO agents_revisions (proposal_id, revision, author_kind, connection_id, run_id, author_id, author,
		title, reason, uncertainties, confidence, evidence, base, ops, preview, op_digests, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, rev, kind, nz(conn), nz(run), authorID, author, t.Title, t.Reason, t.Uncertainties, conf,
		mustJSON(t.Evidence), mustJSON(p.base), mustJSON(p.ops), mustJSON(p.preview), mustJSON(p.digests), now)
	return err
}

func insertTargets(ctx context.Context, tx *sqlx.Tx, id int64, paths []string) error {
	for _, p := range paths {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO agents_targets (proposal_id, path) VALUES (?, ?)`, id, p); err != nil {
			return err
		}
	}
	return nil
}

// postProposal files a new proposal against one album.
func (m *Module) postProposal(c *echo.Context) error {
	ctx, conn := c.Request().Context(), current(c)
	var b createBody
	if err := decode(c, proposalBodyLimit, &b); err != nil {
		return err
	}
	if err := checkText(&b.textFields); err != nil {
		return err
	}
	if b.AlbumID <= 0 {
		return invalid("album_id is required")
	}
	if utf8.RuneCountInString(b.Group) > maxGroup {
		return invalid("group has at most %d characters", maxGroup)
	}
	var followUp int64
	if b.FollowUpOf != nil {
		followUp = *b.FollowUpOf
		if n := utf8.RuneCountInString(b.FollowUpReason); n < 1 || n > maxUncertain {
			return invalid("a follow-up needs a follow_up_reason saying what changed")
		}
	} else if b.FollowUpReason != "" {
		return invalid("follow_up_reason needs follow_up_of")
	}
	if err := m.checkRunRead(ctx, conn, b.RunID); err != nil {
		return err
	}
	lib, err := m.lib()
	if err != nil {
		return err
	}
	album, err := lib.Album(ctx, b.AlbumID)
	if errors.Is(err, libraryport.ErrNotFound) {
		return notFound("album")
	}
	if err != nil {
		return err
	}
	ids := make([]string, len(b.Ops))
	for i, o := range b.Ops {
		ids[i] = o.ID
	}
	p, err := m.prepare(ctx, lib, album, b.Ops, ids, b.Base, true)
	if err != nil {
		return err
	}
	var id int64
	err = m.db.Write(ctx, func(tx *sqlx.Tx) error {
		if err := m.checkRun(ctx, tx, conn, b.RunID); err != nil {
			return err
		}
		if err := m.spend(ctx, tx, conn.ID, "proposal", 1); err != nil {
			return err
		}
		if err := checkPendingQuota(ctx, tx, conn.ID); err != nil {
			return err
		}
		if followUp != 0 {
			if _, err := getProposal(ctx, tx, followUp); err != nil {
				return notFound("follow_up_of proposal")
			}
		}
		if err := checkDuplicates(ctx, tx, 0, followUp, p.fps, ids); err != nil {
			return err
		}
		now := m.now()
		res, err := tx.ExecContext(ctx, `INSERT INTO agents_proposals (connection_id, run_id, album_id, folder, title, artist, group_key,
			follow_up_of, head, pending, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
			conn.ID, b.RunID, album.ID, album.Folder, album.Title, album.Artist, b.Group, nz(followUp), len(ids), now, now)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		if err := insertRevision(ctx, tx, id, 1, "agent", conn.ID, b.RunID, agentID(conn.ID), conn.Name, b.textFields, p, now); err != nil {
			return err
		}
		for _, o := range b.Ops {
			if _, err := tx.ExecContext(ctx, `INSERT INTO agents_ops (proposal_id, op_id, type, fingerprint, decision) VALUES (?, ?, ?, ?, 'pending')`,
				id, o.ID, o.Type, p.fps[o.ID]); err != nil {
				return err
			}
		}
		if err := insertTargets(ctx, tx, id, p.targets); err != nil {
			return err
		}
		if err := m.pin(ctx, tx, lib, id, p.images); err != nil {
			return err
		}
		e := agentEvent(conn, b.RunID, "proposal_created")
		e.Proposal, e.Revision = id, 1
		data := map[string]any{"ops": ids}
		if followUp != 0 {
			data["follow_up_of"], data["follow_up_reason"] = followUp, b.FollowUpReason
		}
		e.Data = data
		_, err = m.insertEvent(ctx, tx, e)
		return err
	})
	if err != nil {
		return err
	}
	m.publish(id)
	d, err := m.detail(ctx, id, m.reviewBase(c), false)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, d)
}

// postRevision is an agent's new revision: the full op list, with decided
// ops unchanged.
func (m *Module) postRevision(c *echo.Context) error {
	ctx, conn := c.Request().Context(), current(c)
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var b reviseBody
	if err := decode(c, proposalBodyLimit, &b); err != nil {
		return err
	}
	if err := checkText(&b.textFields); err != nil {
		return err
	}
	if err := m.checkRunRead(ctx, conn, b.RunID); err != nil {
		return err
	}
	prop, err := getProposal(ctx, m.db.R, id)
	if err != nil {
		return err
	}
	if prop.withdrawn() {
		return conflict("invalid", "the proposal was withdrawn")
	}
	if b.ParentRevision != prop.Head {
		return staleRevision(prop.Head)
	}
	rows, err := getOps(ctx, m.db.R, id)
	if err != nil {
		return err
	}
	if prop.Pending == 0 {
		return conflict("decided_op", "every op of this proposal is decided; file a follow-up proposal instead")
	}
	head, err := getRevision(ctx, m.db.R, id, prop.Head)
	if err != nil {
		return err
	}
	old := head.ops()
	var changed, dropped, added, pending []string
	for _, o := range old {
		n, ok := opByID(b.Ops, o.ID)
		if rows[o.ID].Decision != "pending" {
			if !ok || !sameOp(n, o) {
				return conflict("decided_op", fmt.Sprintf("op %q is %s; keep it unchanged", o.ID, rows[o.ID].Decision), "op", o.ID)
			}
			continue
		}
		switch {
		case !ok:
			dropped = append(dropped, o.ID)
		case !sameOp(n, o):
			changed = append(changed, o.ID)
			pending = append(pending, o.ID)
		default:
			pending = append(pending, o.ID)
		}
	}
	for _, n := range b.Ops {
		if _, ok := opByID(old, n.ID); ok {
			continue
		}
		if _, used := rows[n.ID]; used {
			return invalid("op id %q was used before in this proposal; pick a new one", n.ID)
		}
		added = append(added, n.ID)
		pending = append(pending, n.ID)
	}
	if len(changed)+len(dropped)+len(added) == 0 && sameText(head, b.textFields) {
		return invalid("the revision changes nothing")
	}
	lib, err := m.lib()
	if err != nil {
		return err
	}
	album, err := m.currentAlbum(ctx, lib, id)
	if err != nil {
		return err
	}
	recordedPaths := map[int64]string{}
	for _, base := range head.base() {
		recordedPaths[base.TrackID] = base.Path
	}
	for i := range b.Base {
		b.Base[i].Path = recordedPaths[b.Base[i].TrackID]
	}
	p, err := m.prepare(ctx, lib, album, b.Ops, pending, b.Base, true)
	if err != nil {
		return err
	}
	s := snap(prop, rows)
	rev := prop.Head + 1
	err = m.db.Write(ctx, func(tx *sqlx.Tx) error {
		cur, err := recheck(ctx, tx, id, s)
		if err != nil {
			return err
		}
		if cur.withdrawn() {
			return conflict("invalid", "the proposal was withdrawn")
		}
		if err := m.checkRun(ctx, tx, conn, b.RunID); err != nil {
			return err
		}
		if err := m.spend(ctx, tx, conn.ID, "revision", 1); err != nil {
			return err
		}
		if err := checkDuplicates(ctx, tx, id, prop.FollowUpOf.Int64, p.fps, append(append([]string{}, changed...), added...)); err != nil {
			return err
		}
		now := m.now()
		if err := insertRevision(ctx, tx, id, rev, "agent", conn.ID, b.RunID, agentID(conn.ID), conn.Name, b.textFields, p, now); err != nil {
			return err
		}
		for _, op := range dropped {
			if _, err := tx.ExecContext(ctx, `UPDATE agents_ops SET decision = 'superseded', decided_rev = ?, decided_at = ?, decided_by_id = ?, decided_by = ?
				WHERE proposal_id = ? AND op_id = ?`, rev, now, agentID(conn.ID), conn.Name, id, op); err != nil {
				return err
			}
		}
		for _, o := range b.Ops {
			if slices.Contains(added, o.ID) {
				_, err = tx.ExecContext(ctx, `INSERT INTO agents_ops (proposal_id, op_id, type, fingerprint, decision) VALUES (?, ?, ?, ?, 'pending')`,
					id, o.ID, o.Type, p.fps[o.ID])
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE agents_ops SET fingerprint = ? WHERE proposal_id = ? AND op_id = ?`, p.fps[o.ID], id, o.ID)
			}
			if err != nil {
				return err
			}
		}
		if err := insertTargets(ctx, tx, id, p.targets); err != nil {
			return err
		}
		if err := m.pin(ctx, tx, lib, id, p.images); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agents_proposals SET head = ?, changes_requested_at = NULL WHERE id = ?`, rev, id); err != nil {
			return err
		}
		if err := setPending(ctx, tx, id, now); err != nil {
			return err
		}
		e := agentEvent(conn, b.RunID, "proposal_revised")
		e.Proposal, e.Revision = id, rev
		e.Data = map[string]any{"from": prop.Head, "to": rev, "changed": orEmpty(changed), "dropped": orEmpty(dropped), "added": orEmpty(added)}
		_, err = m.insertEvent(ctx, tx, e)
		return err
	})
	if err != nil {
		return err
	}
	m.publish(id)
	d, err := m.detail(ctx, id, m.reviewBase(c), false)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, d)
}

func sameText(r revisionRow, t textFields) bool {
	conf := sql.NullInt64{}
	if t.Confidence != nil {
		conf = sql.NullInt64{Int64: int64(*t.Confidence), Valid: true}
	}
	return r.Title == t.Title && r.Reason == t.Reason && r.Uncertainties == t.Uncertainties && r.Confidence == conf &&
		mustJSON(r.evidence()) == mustJSON(t.Evidence) && mustJSON(r.base()) == mustJSON(t.Base)
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

type userEdit struct {
	ID         string     `json:"id"`
	Tracks     *[]OpTrack `json:"tracks"`
	EveryTrack *bool      `json:"every_track"`
}

type userReviseBody struct {
	ParentRevision int        `json:"parent_revision"`
	Ops            []userEdit `json:"ops"`
}

// postUserRevision is a reviewer's edit of pending values. It adds no op,
// track or image; it writes no file.
func (m *Module) postUserRevision(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var b userReviseBody
	if err := decode(c, proposalBodyLimit, &b); err != nil {
		return err
	}
	if len(b.Ops) == 0 {
		return invalid("name at least one op to edit")
	}
	prop, err := getProposal(ctx, m.db.R, id)
	if err != nil {
		return err
	}
	if prop.withdrawn() {
		return conflict("invalid", "the proposal was withdrawn")
	}
	if b.ParentRevision != prop.Head {
		return staleRevision(prop.Head)
	}
	rows, err := getOps(ctx, m.db.R, id)
	if err != nil {
		return err
	}
	head, err := getRevision(ctx, m.db.R, id, prop.Head)
	if err != nil {
		return err
	}
	ops := head.ops()
	type change struct {
		Op      string `json:"op"`
		TrackID int64  `json:"track_id,omitempty"`
		Before  any    `json:"before"`
		After   any    `json:"after"`
		Field   string `json:"field,omitempty"`
	}
	edits := []change{}
	seen := map[string]bool{}
	for _, e := range b.Ops {
		if seen[e.ID] {
			return invalid("op %q is listed twice", e.ID)
		}
		seen[e.ID] = true
		i := slices.IndexFunc(ops, func(o Op) bool { return o.ID == e.ID })
		if i < 0 {
			return invalid("op %q is not in revision %d", e.ID, prop.Head)
		}
		if d := rows[e.ID].Decision; d != "pending" {
			return conflict("decided_op", fmt.Sprintf("op %q is %s", e.ID, d), "op", e.ID)
		}
		o := ops[i]
		switch o.Type {
		case "tag":
			if e.EveryTrack != nil {
				return invalid("op %q: every_track is for a cover op", e.ID)
			}
			if e.Tracks == nil {
				return invalid("op %q: give tracks with their values", e.ID)
			}
			if len(*e.Tracks) == 0 {
				return invalid("op %q: keep at least one track; decline the op to drop it", e.ID)
			}
			before := map[int64][]string{}
			for _, t := range o.Tracks {
				before[t.TrackID] = t.Values
			}
			next := []OpTrack{}
			got := map[int64]bool{}
			for _, t := range *e.Tracks {
				old, ok := before[t.TrackID]
				if !ok {
					return invalid("op %q: track %d is not in the op; an edit can't add tracks", e.ID, t.TrackID)
				}
				if got[t.TrackID] {
					return invalid("op %q: track %d is listed twice", e.ID, t.TrackID)
				}
				got[t.TrackID] = true
				if t.Values == nil {
					t.Values = []string{}
				}
				if mustJSON(old) != mustJSON(t.Values) {
					edits = append(edits, change{Op: e.ID, TrackID: t.TrackID, Before: old, After: t.Values})
				}
				next = append(next, t)
			}
			for _, t := range o.Tracks {
				if !got[t.TrackID] {
					edits = append(edits, change{Op: e.ID, TrackID: t.TrackID, Before: t.Values, After: nil})
				}
			}
			o.Tracks = next
		case "cover":
			if e.Tracks != nil {
				return invalid("op %q: a cover op has no tracks", e.ID)
			}
			if e.EveryTrack == nil {
				return invalid("op %q: give every_track", e.ID)
			}
			if o.Action != "replace" {
				return invalid("op %q: a removal has no every_track", e.ID)
			}
			if o.EveryTrack != *e.EveryTrack {
				edits = append(edits, change{Op: e.ID, Field: "every_track", Before: o.EveryTrack, After: *e.EveryTrack})
			}
			o.EveryTrack = *e.EveryTrack
		}
		ops[i] = o
	}
	if len(edits) == 0 {
		return invalid("the edit changes nothing")
	}
	pending := []string{}
	for _, o := range ops {
		if rows[o.ID].Decision == "pending" {
			pending = append(pending, o.ID)
		}
	}
	lib, err := m.lib()
	if err != nil {
		return err
	}
	album, err := m.currentAlbum(ctx, lib, id)
	if err != nil {
		return err
	}
	p, err := m.prepare(ctx, lib, album, ops, pending, head.base(), false)
	if err != nil {
		return err
	}
	a := userActor(ctx)
	s := snap(prop, rows)
	rev := prop.Head + 1
	t := textFields{Title: head.Title, Reason: head.Reason, Uncertainties: head.Uncertainties, Evidence: head.evidence()}
	if head.Confidence.Valid {
		v := int(head.Confidence.Int64)
		t.Confidence = &v
	}
	err = m.db.Write(ctx, func(tx *sqlx.Tx) error {
		if _, err := recheck(ctx, tx, id, s); err != nil {
			return err
		}
		now := m.now()
		if err := insertRevision(ctx, tx, id, rev, "user", 0, 0, a.ID, a.Name, t, p, now); err != nil {
			return err
		}
		for _, o := range ops {
			if _, err := tx.ExecContext(ctx, `UPDATE agents_ops SET fingerprint = ? WHERE proposal_id = ? AND op_id = ?`, p.fps[o.ID], id, o.ID); err != nil {
				return err
			}
		}
		if err := m.pin(ctx, tx, lib, id, p.images); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agents_proposals SET head = ? WHERE id = ?`, rev, id); err != nil {
			return err
		}
		if err := setPending(ctx, tx, id, now); err != nil {
			return err
		}
		e := userEvent(a, "proposal_edited")
		e.Proposal, e.Revision = id, rev
		e.Data = map[string]any{"from": prop.Head, "to": rev, "edits": edits}
		_, err := m.insertEvent(ctx, tx, e)
		return err
	})
	if err != nil {
		return err
	}
	m.publish(id)
	d, err := m.detail(ctx, id, m.reviewBase(c), true)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, d)
}

type withdrawBody struct {
	RunID    int64  `json:"run_id"`
	Revision int    `json:"revision"`
	Reason   string `json:"reason"`
}

// postWithdraw is an agent taking back its pending ops.
func (m *Module) postWithdraw(c *echo.Context) error {
	ctx, conn := c.Request().Context(), current(c)
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var b withdrawBody
	if err := decode(c, 1<<20, &b); err != nil {
		return err
	}
	if utf8.RuneCountInString(b.Reason) > maxBody {
		return invalid("reason has at most %d characters", maxBody)
	}
	prop, err := getProposal(ctx, m.db.R, id)
	if err != nil {
		return err
	}
	if prop.withdrawn() {
		return conflict("invalid", "the proposal was already withdrawn")
	}
	if b.Revision != prop.Head {
		return staleRevision(prop.Head)
	}
	rows, err := getOps(ctx, m.db.R, id)
	if err != nil {
		return err
	}
	s := snap(prop, rows)
	err = m.db.Write(ctx, func(tx *sqlx.Tx) error {
		if _, err := recheck(ctx, tx, id, s); err != nil {
			return err
		}
		if err := m.checkRun(ctx, tx, conn, b.RunID); err != nil {
			return err
		}
		now := m.now()
		if _, err := tx.ExecContext(ctx, `UPDATE agents_ops SET decision = 'superseded', decided_rev = ?, decided_at = ?, decided_by_id = ?, decided_by = ?
			WHERE proposal_id = ? AND decision = 'pending'`, prop.Head, now, agentID(conn.ID), conn.Name, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agents_proposals SET withdrawn_at = ? WHERE id = ?`, now, id); err != nil {
			return err
		}
		if err := setPending(ctx, tx, id, now); err != nil {
			return err
		}
		e := agentEvent(conn, b.RunID, "proposal_withdrawn")
		e.Proposal, e.Revision, e.Body = id, prop.Head, b.Reason
		_, err := m.insertEvent(ctx, tx, e)
		return err
	})
	if err != nil {
		return err
	}
	m.publish(id)
	d, err := m.detail(ctx, id, m.reviewBase(c), false)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, d)
}

type commentBody struct {
	RunID int64  `json:"run_id"`
	Body  string `json:"body"`
}

func checkComment(s string) error {
	if n := utf8.RuneCountInString(s); n < 1 || n > maxBody {
		return invalid("a comment has 1 to %d characters", maxBody)
	}
	return nil
}

func (m *Module) postAgentComment(c *echo.Context) error {
	ctx, conn := c.Request().Context(), current(c)
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var b commentBody
	if err := decode(c, 1<<20, &b); err != nil {
		return err
	}
	if err := checkComment(b.Body); err != nil {
		return err
	}
	var eid int64
	err = m.db.Write(ctx, func(tx *sqlx.Tx) error {
		p, err := getProposal(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := m.checkRun(ctx, tx, conn, b.RunID); err != nil {
			return err
		}
		if err := m.spend(ctx, tx, conn.ID, "comment", 1); err != nil {
			return err
		}
		e := agentEvent(conn, b.RunID, "comment")
		e.Proposal, e.Revision, e.Body = id, p.Head, b.Body
		eid, err = m.insertEvent(ctx, tx, e)
		return err
	})
	if err != nil {
		return err
	}
	m.publish(id)
	return m.answerEvent(c, eid)
}

func (m *Module) answerEvent(c *echo.Context, id int64) error {
	var r eventRow
	if err := m.db.R.GetContext(c.Request().Context(), &r, `SELECT `+eventCols+` FROM agents_events WHERE id = ?`, id); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, r.json())
}
