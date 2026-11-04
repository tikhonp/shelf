// Package tagio reads and writes the tags of audio files in the music
// folder. It knows which keys Navidrome reads, and its Writer changes files
// only through a temp copy that is verified before it replaces the original.
package tagio

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Field is one of Navidrome's tags, with every key that feeds it.
type Field struct {
	// Name is Navidrome's name, such as "albumartist" or
	// "musicbrainz_trackid".
	Name string
	// Key is what shelf adds when a file holds none of the aliases.
	Key string
	// Aliases are the lowercase keys Navidrome reads into the field, in its
	// order.
	Aliases []string
	// Main fields are handled by Navidrome itself. The others are the
	// additional tags it keeps for smart playlists.
	Main bool
	// Album marks a field Navidrome reads per album.
	Album bool
	// Type is "", "date", "uuid" or "pair". A pair key carries a second
	// part after a colon, such as "LYRICS:ENG".
	Type string
	// Split holds the separators Navidrome splits a single value on.
	Split []string
	// MaxLength is where Navidrome cuts a value, or 0 for its default.
	MaxLength int
}

//go:embed mappings.yaml
var mappingsYAML []byte

// keys names the key shelf adds for the fields whose uppercased name isn't
// TagLib's. Picard writes these too, and each is one of the field's
// aliases.
var keys = map[string]string{
	"track":                   "TRACKNUMBER",
	"disc":                    "DISCNUMBER",
	"recordingdate":           "DATE",
	"recordlabel":             "LABEL",
	"explicitstatus":          "ITUNESADVISORY",
	"musicbrainz_recordingid": "MUSICBRAINZ_TRACKID",
	// Navidrome's track ID is the file's release track ID. The names swap.
	"musicbrainz_trackid": "MUSICBRAINZ_RELEASETRACKID",
}

// fields holds every field of mappings.yaml in its order, main first.
// artistSplit holds its separators for an artist tag with one value.
var fields, fieldByName, artistSplit = mustLoadFields(mappingsYAML)

// Fields lists every field Navidrome reads, main fields first.
func Fields() []Field { return slices.Clone(fields) }

// FieldByName finds a field by Navidrome's name.
func FieldByName(name string) (Field, bool) {
	f, ok := fieldByName[name]
	return f, ok
}

type mapping struct {
	Aliases   []string `yaml:"aliases"`
	Album     bool     `yaml:"album"`
	Type      string   `yaml:"type"`
	Split     []string `yaml:"split"`
	MaxLength int      `yaml:"maxLength"`
}

func mustLoadFields(b []byte) ([]Field, map[string]Field, []string) {
	fs, artists, err := loadFields(b)
	if err != nil {
		panic(err)
	}
	by := make(map[string]Field, len(fs))
	for _, f := range fs {
		by[f.Name] = f
	}
	return fs, by, artists
}

func loadFields(b []byte) ([]Field, []string, error) {
	var doc struct {
		Artists    mapping   `yaml:"artists"`
		Main       yaml.Node `yaml:"main"`
		Additional yaml.Node `yaml:"additional"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, nil, fmt.Errorf("mappings.yaml: %w", err)
	}
	var out []Field
	for _, part := range []struct {
		node *yaml.Node
		main bool
	}{{&doc.Main, true}, {&doc.Additional, false}} {
		// A mapping node holds keys and values in turn; the node keeps the
		// file's order, which a Go map would lose.
		n := part.node.Content
		if len(n)%2 != 0 {
			return nil, nil, fmt.Errorf("mappings.yaml: odd mapping")
		}
		for i := 0; i < len(n); i += 2 {
			var m mapping
			if err := n[i+1].Decode(&m); err != nil {
				return nil, nil, fmt.Errorf("mappings.yaml: %s: %w", n[i].Value, err)
			}
			name := n[i].Value
			key, ok := keys[name]
			if !ok {
				key = strings.ToUpper(name)
			}
			out = append(out, Field{
				Name: name, Key: key, Aliases: lower(m.Aliases), Main: part.main,
				Album: m.Album, Type: m.Type, Split: m.Split, MaxLength: m.MaxLength,
			})
		}
	}
	if len(out) == 0 {
		return nil, nil, fmt.Errorf("mappings.yaml: no fields")
	}
	return out, doc.Artists.Split, nil
}

func lower(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		s = strings.ToLower(s)
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// feeds reports whether key, as TagLib reads it from a file of format ft,
// reaches the field in Navidrome.
func (f Field) feeds(ft *Format, key string) bool {
	k := strings.ToLower(key)
	if slices.Contains(f.Aliases, k) {
		return true
	}
	if f.Type == "pair" {
		head, _, ok := strings.Cut(k, ":")
		if ok && slices.Contains(f.Aliases, head) {
			return true
		}
	}
	if ft != nil {
		if atom, ok := ft.atoms[strings.ToUpper(key)]; ok && slices.Contains(f.Aliases, atom) {
			return true
		}
	}
	return false
}

// FieldsOf names the fields a key feeds in a file of format ft. M4A's DATE
// feeds two, since Navidrome reads its atom as the release date as well. A
// key that feeds none is one Navidrome never reads.
func FieldsOf(ft *Format, key string) []Field {
	var out []Field
	for _, f := range fields {
		if f.feeds(ft, key) {
			out = append(out, f)
		}
	}
	return out
}
