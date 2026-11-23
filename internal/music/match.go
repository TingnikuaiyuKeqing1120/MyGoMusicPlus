package music

import (
	"slices"
	"strings"
)

// SameRecording matches complete credits and compatible durations across sources.
// Version suffixes in titles and albums remain significant.
func SameRecording(a, b Track) bool {
	clean := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	if clean(a.Name) == "" || clean(a.Name) != clean(b.Name) || clean(a.Album.Name) == "" || clean(a.Album.Name) != clean(b.Album.Name) {
		return false
	}
	artists := func(track Track) []string {
		var names []string
		for _, artist := range track.Artists {
			if name := clean(artist.Name); name != "" {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		return slices.Compact(names)
	}
	x, y := artists(a), artists(b)
	if len(x) == 0 || !slices.Equal(x, y) {
		return false
	}
	difference := a.Duration - b.Duration
	return a.Duration <= 0 || b.Duration <= 0 || difference >= -2000 && difference <= 2000
}
