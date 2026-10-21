package main

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/elianiva/meiro/youtube"
)

// playlistSortField is the value the playlist page orders its tracks by.
type playlistSortField uint8

const (
	playlistSortOriginal playlistSortField = iota
	playlistSortTitle
	playlistSortArtist
	playlistSortDuration
)

// playlistSort is one playlist page's ordering choice. Original is the zero
// value, so an untouched page keeps YouTube Music's order.
type playlistSort struct {
	field playlistSortField
	desc  bool
}

func (s playlistSort) active() bool { return s.field != playlistSortOriginal }

// ordered reports whether the page shows its tracks in an order other than the
// one YouTube Music gave: a field is sorted by, or the original order is read
// backwards.
func (s playlistSort) ordered() bool { return s.active() || s.desc }

func (s playlistSort) label() string {
	switch s.field {
	case playlistSortOriginal:
		return "Original order"
	case playlistSortTitle:
		return "Title"
	case playlistSortArtist:
		return "Artist"
	case playlistSortDuration:
		return "Duration"
	}
	return "Original order"
}

// choose picks the field the page orders its tracks by. The direction belongs
// to the button beside the control, so picking a field keeps it.
func (s *playlistSort) choose(field playlistSortField) {
	s.field = field
}

// flip reverses the direction, of the original order as well as a field's.
func (s *playlistSort) flip() {
	s.desc = !s.desc
}

// sortPlaylistSections sorts track slots independently within each section.
// Section metadata and non-track items stay where YouTube placed them.
func sortPlaylistSections(sections []youtube.MusicSection, s playlistSort) []youtube.MusicSection {
	if !s.ordered() {
		return sections
	}
	sorted := make([]youtube.MusicSection, len(sections))
	for i, section := range sections {
		sorted[i] = section
		sorted[i].Items = append([]youtube.MusicItem(nil), section.Items...)
	}
	// The original order has nothing to compare, so its descending direction
	// reads the whole playlist backwards: the tracks reverse across the
	// sections, and the headings and the non-track items keep their places.
	if !s.active() {
		tracks := make([]youtube.MusicItem, 0)
		for _, section := range sorted {
			for _, item := range section.Items {
				if item.IsTrack() {
					tracks = append(tracks, item)
				}
			}
		}
		slices.Reverse(tracks)
		track := 0
		for i := range sorted {
			for j, item := range sorted[i].Items {
				if item.IsTrack() {
					sorted[i].Items[j] = tracks[track]
					track++
				}
			}
		}
		return sorted
	}
	// A field sorts the tracks within their own section: a section is a shelf
	// YouTube Music placed, and its heading names what is in it. The keys are
	// worked out once per track rather than once per comparison, which keeps
	// a long playlist's sort within a frame.
	var keys []sortKey
	for i, section := range sections {
		keys = keys[:0]
		for j, item := range section.Items {
			if item.IsTrack() {
				keys = append(keys, newSortKey(item, s.field, j))
			}
		}
		slices.SortStableFunc(keys, func(x, y sortKey) int {
			if s.desc {
				return y.compare(x)
			}
			return x.compare(y)
		})
		track := 0
		for j, item := range section.Items {
			if item.IsTrack() {
				sorted[i].Items[j] = section.Items[keys[track].at]
				track++
			}
		}
	}
	return sorted
}

// sortKey is what a track is ordered by, worked out once: by the field, then
// by title, whatever their case. at is the track's place in its section.
type sortKey struct {
	field   string
	seconds time.Duration
	title   string
	at      int
}

func newSortKey(item youtube.MusicItem, field playlistSortField, at int) sortKey {
	k := sortKey{title: strings.ToLower(item.Title), at: at}
	switch field {
	case playlistSortArtist:
		k.field = strings.ToLower(playlistArtist(item))
	case playlistSortDuration:
		k.seconds = parseDuration(item.Duration)
	case playlistSortOriginal, playlistSortTitle:
	}
	return k
}

func (k sortKey) compare(other sortKey) int {
	return cmp.Or(
		strings.Compare(k.field, other.field),
		cmp.Compare(k.seconds, other.seconds),
		strings.Compare(k.title, other.title),
	)
}

// playlistArtist is the artist a track is sorted under: the start of its
// byline, up to the first " • ", so that a track whose byline links no artist
// still sorts by the name it shows.
func playlistArtist(item youtube.MusicItem) string {
	artist, _, _ := strings.Cut(item.Subtitle, " • ")
	return strings.TrimSpace(artist)
}
