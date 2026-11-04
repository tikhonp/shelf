package navidrome

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	// missingBatch keeps a delete's address short.
	missingBatch = 100
	// maxMissing bounds the list of missing tracks, which carries each
	// track's tags and lyrics.
	maxMissing = 32 << 20
)

// Missing is what RemoveMissing did with a folder's tracks.
type Missing struct {
	// Removed counts the tracks Navidrome forgot.
	Removed int
	// Purges says Navidrome forgets missing files in its own scans, so
	// shelf left them to it.
	Purges bool
}

// RemoveMissing has Navidrome forget the tracks of a deleted folder, given
// inside the music folder. Navidrome deletes only tracks its scan marked
// missing, so a folder its scan hasn't reached yet removes nothing.
func (c *Client) RemoveMissing(ctx context.Context, folder string) (Missing, error) {
	token, err := c.login(ctx)
	if err != nil {
		return Missing{}, err
	}
	conf, err := c.config(ctx, token)
	if err != nil {
		return Missing{}, err
	}
	if conf.Config.Scanner.PurgeMissing == "always" {
		return Missing{Purges: true}, nil
	}
	prefix := strings.Trim(folder, "/") + "/"
	// path is a prefix match in SQL LIKE, so % and _ in a folder's name match
	// more than the folder, and the answer is checked again here.
	u := c.base.JoinPath("api", "missing")
	u.RawQuery = url.Values{"path": {prefix}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Missing{}, fmt.Errorf("navidrome: missing files: %w", err)
	}
	req.Header.Set("X-ND-Authorization", "Bearer "+token)
	var files []struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := c.nativeUpTo(req, &files, maxMissing); err != nil {
		return Missing{}, fmt.Errorf("navidrome: missing files: %w", err)
	}
	var ids []string
	for _, f := range files {
		if f.ID != "" && strings.HasPrefix(f.Path, prefix) {
			ids = append(ids, f.ID)
		}
	}
	out := Missing{}
	for len(ids) > 0 {
		batch := ids[:min(missingBatch, len(ids))]
		ids = ids[len(batch):]
		u := c.base.JoinPath("api", "missing")
		u.Path += "/"
		u.RawQuery = url.Values{"id": batch}.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
		if err != nil {
			return out, fmt.Errorf("navidrome: remove missing files: %w", err)
		}
		req.Header.Set("X-ND-Authorization", "Bearer "+token)
		var ack map[string]any
		if err := c.native(req, &ack); err != nil {
			return out, fmt.Errorf("navidrome: remove missing files: %w", err)
		}
		out.Removed += len(batch)
	}
	return out, nil
}

// RemoveMissing has Navidrome forget a deleted folder's tracks. It is the
// library module's port.
func (m *Module) RemoveMissing(ctx context.Context, folder string) (Missing, error) {
	if m.client == nil {
		return Missing{}, ErrOff
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return m.client.RemoveMissing(ctx, folder)
}
