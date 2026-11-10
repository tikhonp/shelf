package agents

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/libraryport"
)

const uploadLimit = 32 << 20

func (m *Module) agentRoutes(g *echo.Group) {
	g.GET("", m.h(m.description))
	g.POST("/runs", m.h(m.bootstrap))
	g.GET("/albums", m.need(OpLibraryRead, m.albums))
	g.GET("/albums/:id", m.need(OpLibraryRead, m.album))
	g.GET("/albums/:id/cover", m.need(OpLibraryRead, m.albumCover))
	g.GET("/albums/:id/suggestions", m.need(OpLibraryRead, m.suggestions))
	g.GET("/history", m.need(OpHistoryRead, m.history))
	g.POST("/covers", m.need(OpCoversUpload, m.upload))
	g.POST("/covers/offers/:offer", m.need(OpLibraryLookup, m.offer))
	g.GET("/proposals", m.h(m.listProposals))
	g.GET("/proposals/:id", m.h(m.getDetail))
	g.POST("/proposals", m.need(OpProposalsWrite, m.postProposal))
	g.POST("/proposals/:id/revisions", m.need(OpProposalsWrite, m.postRevision))
	g.POST("/proposals/:id/comments", m.need(OpProposalsWrite, m.postAgentComment))
	g.POST("/proposals/:id/withdraw", m.need(OpProposalsWrite, m.postWithdraw))
	g.GET("/events", m.h(m.getEvents))
	g.GET("/memory", m.h(m.getMemory))
	g.GET("/memory/:key", m.h(m.getMemoryKey))
	g.POST("/memory/commit", m.need(OpMemoryWrite, m.commitMemory))
	g.GET("/learning", m.h(m.getLearning))
}
func (m *Module) inboxRoutes(g *echo.Group) {
	g.GET("", m.h(m.info))
	g.GET("/proposals", m.h(m.listProposals))
	g.GET("/proposals/:id", m.h(m.getDetail))
	g.POST("/proposals/:id/preview", m.h(m.previewSelection))
	g.POST("/proposals/:id/accept", m.h(m.postAccept))
	g.POST("/proposals/:id/revisions", m.h(m.postUserRevision))
	for _, r := range []struct{ path, kind string }{{"decline", "declined"}, {"request-changes", "changes_requested"}, {"comments", "comment"}} {
		g.POST("/proposals/:id/"+r.path, m.h(func(c *echo.Context) error { return m.postDecision(c, r.kind) }))
	}
	g.GET("/albums/:id/proposals", m.h(m.albumProposals))
	g.GET("/connections", m.h(m.listConnections))
	g.POST("/connections", m.h(m.addConnection))
	g.POST("/connections/:id/revoke", m.h(m.revokeConnection))
	g.GET("/setup", m.h(m.setup))
	g.GET("/memory", m.h(m.getMemory))
	g.POST("/memory", m.h(func(c *echo.Context) error { return m.userMemory(c, "create") }))
	g.PUT("/memory/:id", m.h(func(c *echo.Context) error { return m.userMemory(c, "correct") }))
	g.DELETE("/memory/:id", m.h(func(c *echo.Context) error { return m.userMemory(c, "delete") }))
	g.POST("/memory/:id/confirm", m.h(func(c *echo.Context) error { return m.userMemory(c, "confirm") }))
	g.POST("/memory/reset", m.h(m.resetMemory))
	g.GET("/memory/export", m.h(m.exportMemory))
}
func (m *Module) info(c *echo.Context) error {
	var pending, changes, connections int
	ctx := c.Request().Context()
	for _, q := range []struct {
		sql string
		out *int
	}{{`SELECT coalesce(sum(pending),0) FROM agents_proposals WHERE withdrawn_at IS NULL`, &pending}, {`SELECT count(*) FROM agents_proposals WHERE changes_requested_at IS NOT NULL AND pending>0 AND withdrawn_at IS NULL`, &changes}, {`SELECT count(*) FROM agents_connections WHERE revoked_at IS NULL`, &connections}} {
		if e := m.db.R.GetContext(ctx, q.out, q.sql); e != nil {
			return e
		}
	}
	mode := "none"
	if m.accounts != nil && m.accounts.AuthenticationEnabled() {
		mode = "simple"
	}
	return c.JSON(200, map[string]any{"pending": pending, "changes_requested": changes, "connections": connections, "accounts": mode, "can_create_connections": mode == "simple"})
}
func (m *Module) listConnections(c *echo.Context) error {
	cs, e := m.ListConnections(c.Request().Context())
	if e != nil {
		return e
	}
	return c.JSON(200, cs)
}
func (m *Module) addConnection(c *echo.Context) error {
	if m.accounts == nil || !m.accounts.AuthenticationEnabled() {
		return fail(403, "forbidden_operation", "use shelf manage agents add <name> without Shelf sign-in")
	}
	var b struct {
		Name       string   `json:"name"`
		Operations []string `json:"operations"`
	}
	if e := decode(c, 1<<20, &b); e != nil {
		return e
	}
	if e := checkName(b.Name); e != nil {
		return invalid("%s", e)
	}
	if _, e := checkOperations(b.Operations); e != nil {
		return invalid("%s", e)
	}
	conn, token, e := m.AddConnection(c.Request().Context(), b.Name, b.Operations)
	if errors.Is(e, ErrNameTaken) {
		return conflict("duplicate", "an active connection has this name")
	}
	if e != nil {
		return e
	}
	return c.JSON(201, map[string]any{"connection": conn, "token": token, "prompt": prompt(m.serverOrigin(c)+"/api/agent/v1", m.reviewBase(c), token)})
}
func (m *Module) revokeConnection(c *echo.Context) error {
	id, e := idParam(c, "id")
	if e != nil {
		return e
	}
	e = m.RevokeConnection(c.Request().Context(), id)
	if errors.Is(e, ErrConnectionNotFound) {
		return notFound("connection")
	}
	if e != nil && !errors.Is(e, ErrAlreadyRevoked) {
		return e
	}
	return c.JSON(200, map[string]any{"revoked": true})
}
func (m *Module) setup(c *echo.Context) error {
	base := m.serverOrigin(c) + "/api/agent/v1"
	ops := []map[string]string{}
	for _, o := range Operations {
		ops = append(ops, map[string]string{"name": o.Name, "summary": o.Summary})
	}
	return c.JSON(200, map[string]any{"api_base": base, "review_base": m.reviewBase(c), "operations": ops, "prompt": prompt(base, m.reviewBase(c), "")})
}
func (m *Module) description(c *echo.Context) error {
	endpoints := []map[string]string{}
	for _, r := range []struct{ method, path, op, summary string }{
		{"POST", "/runs", "", "Start a fresh run; returns run_id, shared memory without context bodies, learning progress, this connection's outstanding work, limits and usage"},
		{"GET", "/albums?limit=100&offset=0&search=&filter=", "library.read", "Search current albums; filter=issues lists albums with check issues"},
		{"GET", "/albums/:id", "library.read", "Read track IDs, base revisions, tags and check issues"},
		{"GET", "/albums/:id/suggestions?external=false", "library.read", "Local suggestions; external=true also requires library.lookup"},
		{"GET", "/history?after=0&limit=50", "history.read", "Existing tagging history with authors, undo state and coverage gaps"},
		{"POST", "/covers", "covers.upload", "Upload raw JPEG or PNG bytes; returns the cover id"},
		{"POST", "/covers/offers/:offer", "library.lookup", "Materialize a cover offer from suggestions; returns the cover id"},
		{"GET", "/proposals?state=all&after=0", "", "List proposals"},
		{"GET", "/proposals/:id", "", "Read a proposal: head, current revision, events, and acceptances with their outcome"},
		{"POST", "/proposals", "proposals.write", "Create a proposal for one album"},
		{"POST", "/proposals/:id/revisions", "proposals.write", "Send a full new revision"},
		{"POST", "/proposals/:id/comments", "proposals.write", "Comment on a proposal"},
		{"POST", "/proposals/:id/withdraw", "proposals.write", "Withdraw the pending ops"},
		{"GET", "/events?after=0&limit=100", "", "Replay durable feedback; stop when next equals highest"},
		{"GET", "/memory?status=all&after=0", "", "List shared rules, preferences, context and tombstones"},
		{"GET", "/memory/:key", "", "Read one memory entry with its body"},
		{"GET", "/learning", "", "Read the library-wide learning checkpoint"},
		{"POST", "/memory/commit", "memory.write", "Atomically update memory and the learning checkpoint"},
	} {
		endpoints = append(endpoints, map[string]string{"method": r.method, "path": r.path, "operation": r.op, "summary": r.summary})
	}
	fields := []libraryport.Field{}
	if lib, e := m.lib(); e == nil {
		fields = lib.Fields()
	}
	return c.JSON(200, map[string]any{"protocol": 1, "endpoints": endpoints, "fields": fields,
		"bodies": map[string]map[string]string{
			"POST /runs": {"client": "your name and model, up to 200 bytes"},
			"POST /proposals": {
				"run_id": "from POST /runs", "album_id": "the album", "title": "required, up to 200 characters", "reason": "required, up to 4000 characters, saying what changes and why",
				"uncertainties": "what you could not confirm, up to 2000 characters", "confidence": "0 to 100, or omit", "evidence": "up to 20 proposal evidence items",
				"base": "[{track_id, revision}] for every track an op touches, every album track for a cover op", "ops": "1 to 30 ops",
				"group":        "optional label shared by the proposals of one pass, up to 100 characters",
				"follow_up_of": "optional ID of the proposal whose declined op this repeats", "follow_up_reason": "required with follow_up_of: the new evidence",
			},
			"POST /proposals/:id/revisions": {
				"run_id": "from POST /runs", "parent_revision": "the proposal's head", "title": "required", "reason": "required", "uncertainties": "optional",
				"confidence": "optional", "evidence": "optional", "base": "required, as in POST /proposals",
				"ops": "the full op list; decided ops unchanged, a dropped op's ID never reused",
			},
			"POST /proposals/:id/comments": {"run_id": "from POST /runs", "body": "the comment, up to 4000 characters"},
			"POST /proposals/:id/withdraw": {"run_id": "from POST /runs", "revision": "the proposal's head", "reason": "why"},
			"POST /memory/commit":          {"run_id": "from POST /runs", "writes": "up to 100 memory writes", "learning": "optional learning write"},
			"op": {
				"id": "unique in the revision, matching ^[a-z0-9][a-z0-9._-]{0,47}$", "type": "tag or cover",
				"field": "a tag op: a name from fields", "tag_key": "a tag op: a raw tag key, instead of field",
				"tracks": "a tag op: [{track_id, values}]; values is the track's full new list, [] removes the tag",
				"anchor": "a tag op: the MusicBrainz release ID the value came from", "action": "a cover op: replace or remove",
				"image":       "a cover op with replace: the id from POST /covers or an offer",
				"every_track": "a cover op: true also embeds the cover in tracks that have none", "requires": "IDs of ops the user must accept with this one",
			},
			"proposal evidence": {"kind": "suggestion, memory, change, event, note or link", "source": "optional", "field": "optional", "anchor": "optional",
				"key": "optional memory key", "change": "optional history change ID", "event": "optional event ID", "text": "optional", "url": "optional link"},
			"memory write": {
				"op": "put or delete; an agent deletes only context an agent wrote", "key": "matching ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,119}$",
				"expected_version": "0 to create, else the entry's current version", "layer": "rule, preference or context; fixed once created",
				"title": "required, up to 200 bytes", "scope": "optional, up to 500 bytes", "body": "up to 64 KiB", "confidence": "optional, 0 to 100",
				"evidence":   "up to 20 memory evidence items",
				"reconsider": "{reason, evidence_events}, required for an entry with corrected_event; cite a user event newer than it",
			},
			"memory evidence": {"event": "optional event ID", "change": "optional history change ID", "proposal": "optional proposal ID", "note": "optional, up to 4000 bytes"},
			"learning write": {
				"expected_version": "learning.version", "events_cursor": "the last event you read", "history_cursor": "the last history change you read; send it back unchanged otherwise",
				"checkpoint": "your JSON state, up to 64 KiB; an omitted one becomes {}",
			},
		},
		"operation_examples": []Op{
			{ID: "genre", Type: "tag", Field: "genre", Tracks: []OpTrack{{TrackID: 1, Values: []string{"Art Rock"}}}},
			{ID: "custom", Type: "tag", TagKey: "MOOD", Tracks: []OpTrack{{TrackID: 1, Values: []string{}}}},
			{ID: "artwork", Type: "cover", Action: "replace", Image: "sha256 returned by upload or materialized offer"},
			{ID: "remove-artwork", Type: "cover", Action: "remove"},
		},
		"rules": []string{
			"The API refuses unknown JSON fields.",
			"Use actual track IDs and revisions returned by GET /albums/:id. Replace example IDs and hashes.",
			"An empty values array removes a tag. Values are independent literal strings.",
			"A revision has at most one cover op, and two ops may not set the same field of one track.",
			"A proposal takes one anchor. Shelf refuses an anchored op that would leave a track with another musicbrainz_albumid.",
			"requires lists operation IDs that must be reviewed together. Cycles are refused.",
			"An op that repeats a pending op of another proposal is refused. One that repeats a declined op needs follow_up_of and follow_up_reason.",
			"POST /proposals/:id/revisions sends the full operation list; decided operations must remain unchanged.",
			"An undo arrives as an outcome event with undone true. GET /proposals/:id also shows it in acceptances[].outcome.undone.",
			"GET /events never returns an empty next. Stop paging when next equals highest.",
			"An agent may propose a rule but only a user can activate it. Active rules cannot be changed by an agent.",
			"All memory writes need expected_version, using 0 to create. To reconsider a corrected or deleted preference, cite a user event newer than the correction.",
			"POST /runs leaves out context bodies. GET /memory/:key returns a full document. Follow pagination cursors on list endpoints.",
			"Only Shelf users may accept. Return proposal URLs to the user for review.",
		}, "proposal_example": map[string]any{"run_id": 1, "album_id": 1, "title": "Correct genre", "reason": "Explain evidence", "base": []BaseTrack{{TrackID: 1, Revision: "from album read"}}, "ops": []Op{{ID: "genre", Type: "tag", Field: "genre", Tracks: []OpTrack{{TrackID: 1, Values: []string{"Art Rock"}}}}}}, "memory_commit_example": json.RawMessage(`{"run_id":1,"writes":[{"op":"put","key":"genres","expected_version":0,"layer":"preference","title":"Genre preference","body":"Prefer specific genres","evidence":[{"event":12,"note":"The user declined Rock for Art Rock"}]}],"learning":{"expected_version":0,"events_cursor":12,"history_cursor":0,"checkpoint":{}}}`)})
}
func (m *Module) bootstrap(c *echo.Context) error {
	var b struct {
		Client string `json:"client"`
	}
	if e := decode(c, 1<<20, &b); e != nil {
		return e
	}
	if len(b.Client) > maxClient {
		return invalid("client too long")
	}
	ctx := c.Request().Context()
	conn := current(c)
	var run int64
	e := m.db.Write(ctx, func(tx *sqlx.Tx) error {
		if e := m.spend(ctx, tx, conn.ID, "run", 1); e != nil {
			return e
		}
		r, e := tx.ExecContext(ctx, `INSERT INTO agents_runs(connection_id,client,started_at,last_seen_at) VALUES(?,?,?,?)`, conn.ID, b.Client, m.now(), m.now())
		if e == nil {
			run, e = r.LastInsertId()
		}
		return e
	})
	if e != nil {
		return e
	}
	l, e := learning(ctx, m.db.R)
	if e != nil {
		return e
	}
	docs := []memoryRow{}
	if e = m.db.R.SelectContext(ctx, &docs, `SELECT * FROM agents_memory ORDER BY id`); e != nil {
		return e
	}
	memory := []map[string]any{}
	for _, d := range docs {
		v := d.json()
		if d.Layer == "context" && d.Status != "deleted" {
			delete(v, "body")
		}
		memory = append(memory, v)
	}
	out, e := m.outstanding(ctx, conn.ID, l.Events, m.reviewBase(c))
	if e != nil {
		return e
	}
	lj := l.json()
	var unread int64
	if e = m.db.R.GetContext(ctx, &unread, `SELECT count(*) FROM agents_events WHERE id>?`, l.Events); e != nil {
		return e
	}
	lj["unprocessed_events"] = unread
	u, e := m.usage(ctx, conn.ID)
	if e != nil {
		return e
	}
	return c.JSON(201, map[string]any{"protocol": 1, "run_id": run, "connection": conn, "library": map[string]any{"name": "Music Shelf", "review_base": m.reviewBase(c)}, "memory": memory, "learning": lj, "outstanding": out, "feedback_after": l.Events, "limits": map[string]any{"ops_per_proposal": maxOps, "pending_per_connection": pendingPerConnection, "daily": daily}, "usage": u})
}
func (m *Module) albums(c *echo.Context) error {
	lib, e := m.lib()
	if e != nil {
		return e
	}
	limit, e := intQuery(c, "limit", 100, 1, 100)
	if e != nil {
		return e
	}
	offset, e := intQuery(c, "offset", 0, 0, 1<<31)
	if e != nil {
		return e
	}
	q := libraryport.AlbumQuery{Search: c.QueryParam("search"), Filter: c.QueryParam("filter"), Folder: c.QueryParam("folder"), Source: c.QueryParam("source"), Sort: c.QueryParam("sort"), Desc: c.QueryParam("desc") == "true", Limit: int(limit), Offset: int(offset)}
	p, e := lib.Albums(c.Request().Context(), q)
	if e != nil {
		return m.libraryError(e)
	}
	return c.JSON(200, p)
}
func (m *Module) album(c *echo.Context) error {
	id, e := idParam(c, "id")
	if e != nil {
		return e
	}
	lib, e := m.lib()
	if e != nil {
		return e
	}
	a, e := lib.Album(c.Request().Context(), id)
	if e != nil {
		return m.libraryError(e)
	}
	return c.JSON(200, a)
}
func (m *Module) albumCover(c *echo.Context) error {
	id, e := idParam(c, "id")
	if e != nil {
		return e
	}
	lib, e := m.lib()
	if e != nil {
		return e
	}
	b, mime, e := lib.AlbumCover(c.Request().Context(), id)
	if e != nil {
		return m.libraryError(e)
	}
	return c.Blob(200, mime, b)
}
func (m *Module) suggestions(c *echo.Context) error {
	id, e := idParam(c, "id")
	if e != nil {
		return e
	}
	external := c.QueryParam("external") == "true"
	conn := current(c)
	if external && !conn.can(OpLibraryLookup) {
		return fail(403, "forbidden_operation", "external lookups need library.lookup")
	}
	lib, e := m.lib()
	if e != nil {
		return e
	}
	var raw json.RawMessage
	work := func() error { var e error; raw, e = lib.Suggestions(c.Request().Context(), id, external); return e }
	if external {
		if e = m.spendNow(c.Request().Context(), conn.ID, "lookup", 1); e != nil {
			return e
		}
		e = m.lookup(c.Request().Context(), work)
	} else {
		e = work()
	}
	if e != nil {
		return m.libraryError(e)
	}
	return c.JSON(200, raw)
}
func (m *Module) history(c *echo.Context) error {
	after, e := intQuery(c, "after", 0, 0, 1<<62)
	if e != nil {
		return e
	}
	limit, e := intQuery(c, "limit", 50, 1, 50)
	if e != nil {
		return e
	}
	lib, e := m.lib()
	if e != nil {
		return e
	}
	ctx := c.Request().Context()
	p, e := lib.History(ctx, after, int(limit))
	if e != nil {
		return e
	}
	high := int64(0)
	for _, ch := range p.Changes {
		high = max(high, ch.ID)
	}
	high = max(high, p.TrimmedThrough)
	if e = m.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE agents_state SET value=max(value,?) WHERE key='history_high'`, high)
		return e
	}); e != nil {
		return e
	}
	return c.JSON(200, p)
}
func (m *Module) upload(c *echo.Context) error {
	ctx, conn := c.Request().Context(), current(c)
	lib, e := m.lib()
	if e != nil {
		return e
	}
	var img libraryport.Image
	// Read and decode one upload at a time across agents. Holding the gate
	// before reading the body bounds aggregate upload and image allocations.
	e = m.lookup(ctx, func() error {
		b, e := io.ReadAll(io.LimitReader(c.Request().Body, uploadLimit+1))
		if e != nil {
			return invalid("cannot read image")
		}
		if len(b) > uploadLimit {
			return fail(413, "quota", "cover over 32 MiB")
		}
		e = m.db.Write(ctx, func(tx *sqlx.Tx) error {
			if e := m.spend(ctx, tx, conn.ID, "upload", 1); e != nil {
				return e
			}
			return m.spend(ctx, tx, conn.ID, "upload_bytes", int64(len(b)))
		})
		if e != nil {
			return e
		}
		img, e = lib.PutCover(ctx, b)
		return e
	})
	if e != nil {
		return m.libraryError(e)
	}
	return c.JSON(201, img)
}
func (m *Module) offer(c *echo.Context) error {
	ctx := c.Request().Context()
	if e := m.spendNow(ctx, current(c).ID, "offer", 1); e != nil {
		return e
	}
	lib, e := m.lib()
	if e != nil {
		return e
	}
	var img libraryport.Image
	e = m.lookup(ctx, func() error { var e error; img, e = lib.MaterializeOffer(ctx, c.Param("offer")); return e })
	if e != nil {
		return m.libraryError(e)
	}
	return c.JSON(201, img)
}
func (m *Module) albumProposals(c *echo.Context) error {
	id, e := idParam(c, "id")
	if e != nil {
		return e
	}
	lib, e := m.lib()
	if e != nil {
		return e
	}
	ctx := c.Request().Context()
	a, e := lib.Album(ctx, id)
	if e != nil {
		return m.libraryError(e)
	}
	paths := []string{}
	for _, t := range a.Tracks {
		paths = append(paths, t.Path)
	}
	ids := []int64{}
	if len(paths) > 0 {
		q, args, e := sqlx.In(`SELECT DISTINCT p.id FROM agents_proposals p JOIN agents_targets t ON t.proposal_id=p.id WHERE p.pending>0 AND p.withdrawn_at IS NULL AND t.path IN (?) ORDER BY p.id DESC`, paths)
		if e != nil {
			return e
		}
		if e = m.db.R.SelectContext(ctx, &ids, q, args...); e != nil {
			return e
		}
	}
	out := []map[string]any{}
	for _, id := range ids {
		d, e := m.detail(ctx, id, m.reviewBase(c), false)
		if e != nil {
			return e
		}
		out = append(out, d)
	}
	return c.JSON(200, map[string]any{"proposals": out})
}
