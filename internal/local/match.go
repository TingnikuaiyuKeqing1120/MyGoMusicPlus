package local

import "mygo-music/internal/music"

// Match finds one readable library file with the same complete credits.
// Ambiguous metadata leaves the online source in charge of playback.
func (s *Source) Match(track music.Track) *music.Stream {
	var found *music.Stream
	for _, item := range s.cat.Load().items {
		if !music.SameRecording(track, item.public()) {
			continue
		}
		stream, err := s.Stream(item.id, "")
		if err != nil || stream.Size <= 0 {
			continue
		}
		if found != nil && found.URL != stream.URL {
			return nil
		}
		found = stream
	}
	return found
}
