package library

import (
	"slices"
	"testing"

	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"go.senan.xyz/taglib"
)

func writeTags(t *testing.T, path string, tags map[string][]string) {
	t.Helper()
	if err := taglib.WriteTags(path, tags, 0); err != nil {
		t.Fatal(err)
	}
}

func fileWith(path string, tags map[string][]string) tagio.File {
	return tagio.File{Path: path, Format: tagio.FormatOf(path), Tags: tags}
}

func TestReleaseDateEdits(t *testing.T) {
	cases := []struct {
		name string
		path string
		tags map[string][]string
		want []tagio.Edit
	}{
		{"has one", "a.flac", map[string][]string{"RELEASEDATE": {"1971"}, "DATE": {"1980"}}, nil},
		{"no date", "a.flac", map[string][]string{"TITLE": {"x"}}, nil},
		{"date alone", "a.flac", map[string][]string{"DATE": {"1971"}}, []tagio.Edit{{Key: "RELEASEDATE", Values: []string{"1971"}}}},
		{"legacy pair", "a.flac", map[string][]string{"DATE": {"2010"}, "ORIGINALDATE": {"1971"}},
			[]tagio.Edit{{Key: "RELEASEDATE", Values: []string{"2010"}}, {Key: "DATE", Values: []string{"1971"}}}},
		{"year feeds it", "a.flac", map[string][]string{"YEAR": {"1999"}, "DATE": {"2010"}, "ORIGINALDATE": {"1971"}},
			[]tagio.Edit{{Key: "RELEASEDATE", Values: []string{"1999"}}}},
		{"m4a date feeds it", "a.m4a", map[string][]string{"DATE": {"2010"}, "ORIGINALDATE": {"1971"}},
			[]tagio.Edit{{Key: "RELEASEDATE", Values: []string{"2010"}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := releaseDateEdits(fileWith(c.path, c.tags))
			if !slices.EqualFunc(got, c.want, func(a, b tagio.Edit) bool { return a.Key == b.Key && slices.Equal(a.Values, b.Values) }) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestCommonValues(t *testing.T) {
	tfs := func(labels ...string) []tagio.File {
		var out []tagio.File
		for _, l := range labels {
			tags := map[string][]string{}
			if l != "" {
				tags["LABEL"] = []string{l}
			}
			out = append(out, fileWith("a.flac", tags))
		}
		return out
	}
	if v := commonValues(tfs("A", "A", "B"), "recordlabel"); !slices.Equal(v, []string{"A"}) {
		t.Fatal(v)
	}
	// A value and none: the value is what most tracks that hold one hold.
	if v := commonValues(tfs("A", "", ""), "recordlabel"); !slices.Equal(v, []string{"A"}) {
		t.Fatal(v)
	}
	// A tie has no answer, so the run leaves the field alone.
	if v := commonValues(tfs("A", "B"), "recordlabel"); v != nil {
		t.Fatal(v)
	}
	if v := commonValues(tfs("", ""), "recordlabel"); v != nil {
		t.Fatal(v)
	}
}

func TestRunFixesBuildOnEachOther(t *testing.T) {
	r := newRig(t)
	// The two tracks differ on the label, which the most common value settles,
	// and one key disagrees with its alias. Various Artists wants the flag.
	p := r.album("Blue",
		map[string][]string{"ALBUMARTIST": {"Various Artists"}, "RELEASEDATE": {"1971"}, "LABEL": {"Reprise"}, "ORGANIZATION": {"Elektra"}},
		map[string][]string{"ALBUMARTIST": {"Various Artists"}, "RELEASEDATE": {"1971"}, "LABEL": {"Reprise"}},
		map[string][]string{"ALBUMARTIST": {"Various Artists"}, "RELEASEDATE": {"1971"}, "LABEL": {"Asylum"}})
	r.walk()
	r.startJobs()
	run := r.startRun(every(string(TagsDisagree), string(ValuesDiffer), string(VariousArtists)))
	r.drain()
	got := r.run(run.ID)
	if got.State != StateDone || got.Files != 3 || got.Log[0].Fixes < 2 {
		t.Fatalf("%+v", got)
	}
	for _, f := range p {
		tags := tagsOf(t, f)
		if !slices.Equal(tags["LABEL"], []string{"Reprise"}) || !slices.Equal(tags["COMPILATION"], []string{"1"}) {
			t.Fatalf("%s %v", f, tags)
		}
	}
	if tags := tagsOf(t, p[0]); tags["ORGANIZATION"] != nil && !slices.Equal(tags["ORGANIZATION"], []string{"Reprise"}) {
		t.Fatalf("%v", tags)
	}
}

func TestRunFixLeavesAgreeingTracksAlone(t *testing.T) {
	r := newRig(t)
	// The first track disagrees for real. The second holds the year Picard
	// writes beside the full date, which agrees.
	p := r.album("Hejira",
		map[string][]string{"ORIGINALDATE": {"1997-07-01"}, "ORIGINALYEAR": {"2004"}},
		map[string][]string{"ORIGINALDATE": {"1997-07-01"}, "ORIGINALYEAR": {"1997"}})
	r.walk()
	r.startJobs()
	run := r.startRun(every(string(TagsDisagree)))
	r.drain()
	if got := r.run(run.ID); got.State != StateDone {
		t.Fatalf("%+v", got)
	}
	if tags := tagsOf(t, p[0]); !slices.Equal(tags["ORIGINALYEAR"], []string{"1997-07-01"}) {
		t.Fatalf("the disagreeing track: %v", tags)
	}
	if tags := tagsOf(t, p[1]); !slices.Equal(tags["ORIGINALYEAR"], []string{"1997"}) || !slices.Equal(tags["ORIGINALDATE"], []string{"1997-07-01"}) {
		t.Fatalf("the agreeing track: %v", tags)
	}
}

func TestSpread(t *testing.T) {
	if got := spread([]int{1, 2}, 4); !slices.Equal(got, []int{1, 2}) {
		t.Fatal(got)
	}
	if got := spread([]int{0, 1, 2, 3, 4, 5, 6, 7}, 4); !slices.Equal(got, []int{0, 2, 4, 6}) {
		t.Fatal(got)
	}
}
