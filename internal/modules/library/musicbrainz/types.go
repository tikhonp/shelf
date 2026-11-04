package musicbrainz

import (
	"strings"
)

// The answers' shapes, with only what the suggestions use.

// Credit is one artist of an artist credit, with the text that joins it to
// the next.
type Credit struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
	Artist     struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		SortName string `json:"sort-name"`
	} `json:"artist"`
}

// Credits is an artist credit.
type Credits []Credit

// String is the credit as written, such as "Miles Davis & John Coltrane".
func (cs Credits) String() string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.Name)
		b.WriteString(c.JoinPhrase)
	}
	return b.String()
}

// Names are the credited names, one per artist.
func (cs Credits) Names() []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

// Sort is the credit with each artist's sort name.
func (cs Credits) Sort() string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.Artist.SortName)
		b.WriteString(c.JoinPhrase)
	}
	return b.String()
}

// IDs are the artists' IDs, in order.
func (cs Credits) IDs() []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Artist.ID)
	}
	return out
}

// Hit is a release in a search or an ISRC answer.
type Hit struct {
	ID           string  `json:"id"`
	Score        int     `json:"score"`
	Title        string  `json:"title"`
	TrackCount   int     `json:"track-count"`
	Date         string  `json:"date"`
	Country      string  `json:"country"`
	Barcode      string  `json:"barcode"`
	ArtistCredit Credits `json:"artist-credit"`
	ReleaseGroup Group   `json:"release-group"`
}

// Group is a release group.
type Group struct {
	ID               string   `json:"id"`
	PrimaryType      string   `json:"primary-type"`
	SecondaryTypes   []string `json:"secondary-types"`
	FirstReleaseDate string   `json:"first-release-date"`
	Genres           []Genre  `json:"genres"`
}

// Types are the group's types, lowercased as Picard writes them: the
// primary type first.
func (g Group) Types() []string {
	var out []string
	if g.PrimaryType != "" {
		out = append(out, strings.ToLower(g.PrimaryType))
	}
	for _, t := range g.SecondaryTypes {
		out = append(out, strings.ToLower(t))
	}
	return out
}

// Genre is a genre with its votes.
type Genre struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Release is a release with its media and tracks.
type Release struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Date     string `json:"date"`
	Country  string `json:"country"`
	Barcode  string `json:"barcode"`
	ASIN     string `json:"asin"`
	TextRepr struct {
		Language string `json:"language"`
		Script   string `json:"script"`
	} `json:"text-representation"`
	ArtistCredit Credits `json:"artist-credit"`
	LabelInfo    []struct {
		CatalogNumber string `json:"catalog-number"`
		Label         *struct {
			Name string `json:"name"`
		} `json:"label"`
	} `json:"label-info"`
	ReleaseGroup Group    `json:"release-group"`
	Genres       []Genre  `json:"genres"`
	Media        []Medium `json:"media"`
}

// Medium is one disc.
type Medium struct {
	Position int     `json:"position"`
	Format   string  `json:"format"`
	Title    string  `json:"title"`
	Tracks   []Track `json:"tracks"`
}

// Track is a track of a release. Its ID is the release track ID.
type Track struct {
	ID           string    `json:"id"`
	Position     int       `json:"position"`
	Number       string    `json:"number"`
	Title        string    `json:"title"`
	Length       int64     `json:"length"` // milliseconds
	ArtistCredit Credits   `json:"artist-credit"`
	Recording    Recording `json:"recording"`
}

// Recording is the recording behind a track.
type Recording struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Length    int64      `json:"length"`
	ISRCs     []string   `json:"isrcs"`
	Relations []Relation `json:"relations"`
}

// Relation is a relation of a recording or a work.
type Relation struct {
	Type   string `json:"type"`
	Work   *Work  `json:"work"`
	Artist *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

// Work is a composition.
type Work struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Relations []Relation `json:"relations"`
}

// Composers are the composers of the recording's works, in order and once
// each.
func (r Recording) Composers() []string {
	var out []string
	for _, rel := range r.Relations {
		if rel.Work == nil {
			continue
		}
		for _, wr := range rel.Work.Relations {
			if wr.Type == "composer" && wr.Artist != nil && !contains(out, wr.Artist.Name) {
				out = append(out, wr.Artist.Name)
			}
		}
	}
	return out
}

// Works are the titles of the recording's works.
func (r Recording) Works() []string {
	var out []string
	for _, rel := range r.Relations {
		if rel.Work != nil && rel.Work.Title != "" && !contains(out, rel.Work.Title) {
			out = append(out, rel.Work.Title)
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// Label is the first label's name.
func (r Release) Label() string {
	for _, li := range r.LabelInfo {
		if li.Label != nil && li.Label.Name != "" {
			return li.Label.Name
		}
	}
	return ""
}

// CatalogNumber is the first catalogue number.
func (r Release) CatalogNumber() string {
	for _, li := range r.LabelInfo {
		if li.CatalogNumber != "" && !strings.EqualFold(li.CatalogNumber, "[none]") {
			return li.CatalogNumber
		}
	}
	return ""
}

// TrackCount counts the tracks over every medium.
func (r Release) TrackCount() int {
	n := 0
	for _, m := range r.Media {
		n += len(m.Tracks)
	}
	return n
}

// Genre is the release's most voted genre, else its group's.
func (r Release) Genre() string {
	for _, gs := range [][]Genre{r.Genres, r.ReleaseGroup.Genres} {
		best := Genre{}
		for _, g := range gs {
			if g.Count > best.Count {
				best = g
			}
		}
		if best.Name != "" {
			return best.Name
		}
	}
	return ""
}

// Formats lists the media formats once each, such as "CD" or "Digital
// Media".
func (r Release) Formats() []string {
	var out []string
	for _, m := range r.Media {
		if m.Format != "" && !contains(out, m.Format) {
			out = append(out, m.Format)
		}
	}
	return out
}
