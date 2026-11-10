package library

import "testing"

// Agents learn valid field names from this list, so it must hold the fields
// no file has yet, such as albumartists.
func TestPortListsNavidromeFields(t *testing.T) {
	names := map[string]bool{}
	for _, f := range New().Port().Fields() {
		if f.Key == "" {
			t.Errorf("field %q has no key", f.Name)
		}
		names[f.Name] = true
	}
	for _, n := range []string{"genre", "albumartists", "releasetype", "musicbrainz_albumid"} {
		if !names[n] {
			t.Errorf("no field %q", n)
		}
	}
}
