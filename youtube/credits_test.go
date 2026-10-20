package youtube

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSearchRowCreditsArtistFromLinksNotTheSubtitle(t *testing.T) {
	result, err := searchServer(t, searchPageResponse, nil).Search(context.Background(), "ambient", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	song := searchItem(t, result, "First Light")
	if song.Artist != "Someone" {
		t.Errorf("Artist = %q, want Someone: the kind and the length are not names", song.Artist)
	}
	if song.Artists != "Someone" {
		t.Errorf("Artists = %q, want the one linked artist", song.Artists)
	}
	if song.Subtitle != "Song • Someone • 3:42" && song.Subtitle != "Song • Someone" {
		t.Errorf("the display subtitle changed: %q", song.Subtitle)
	}
}

func TestPanelItemsCreditArtistAndAlbum(t *testing.T) {
	const response = `{"contents":{"playlistPanelRenderer":{"contents":[
		{"playlistPanelVideoRenderer":{"videoId":"a","title":{"runs":[{"text":"Duet"}]},"longBylineText":{"runs":[
			{"text":"First","navigationEndpoint":{"browseEndpoint":{"browseId":"UCfirst"}}},
			{"text":" & "},
			{"text":"Second","navigationEndpoint":{"browseEndpoint":{"browseId":"UCsecond"}}},
			{"text":" & "},
			{"text":"Third","navigationEndpoint":{"browseEndpoint":{"browseId":"UCthird"}}},
			{"text":" • "},
			{"text":"The Album","navigationEndpoint":{"browseEndpoint":{"browseId":"MPREb_album"}}},
			{"text":" • "},
			{"text":"2020"}]}}},
		{"playlistPanelVideoRenderer":{"videoId":"b","title":{"runs":[{"text":"Unlinked"}]},"longBylineText":{"runs":[{"text":"Plain Text Artist"},{"text":" • "},{"text":"2020"}]}}},
		{"playlistPanelVideoRenderer":{"videoId":"c","title":{"runs":[{"text":"Bare"}]}}}
	]}}}`
	result := upNextResult(decodeResponse(json.RawMessage(response)))
	got := map[string]MusicItem{}
	for _, item := range result.Items {
		got[item.VideoID] = item
	}
	if a := got["a"]; a.Subtitle != "First & Second & Third • The Album • 2020" {
		t.Errorf("a queue track shows no byline: subtitle %q", a.Subtitle)
	}
	if a := got["a"]; a.Artist != "First" || a.Artists != "First, Second, Third" || a.Album != "The Album" {
		t.Errorf("a: artist %q artists %q album %q, want every linked artist in order and the album", a.Artist, a.Artists, a.Album)
	}
	if b := got["b"]; b.Artist != "" {
		t.Errorf("an artist that is only text was guessed: %q", b.Artist)
	}
	if c := got["c"]; c.Artist != "" || c.Album != "" {
		t.Errorf("a track with no byline credits %q %q", c.Artist, c.Album)
	}
}

func TestTwoRowSubtitleCreditsArtist(t *testing.T) {
	const renderer = `{"title":{"runs":[{"text":"Cover"}]},"subtitle":{"runs":[
		{"text":"Song"},{"text":" • "},{"text":"Singer","navigationEndpoint":{"browseEndpoint":{"browseId":"UCsinger"}}}]},
		"navigationEndpoint":{"watchEndpoint":{"videoId":"vid-9"}}}`
	// A home shelf's cards are two-row items that are not told apart from an
	// album's by their renderer, only by the video they carry.
	for _, kind := range []ItemKind{KindVideo, KindMusicItem} {
		item := parseMusicItem(kind, decodeResponse(json.RawMessage(renderer)).(*jsonObject))
		if item.Artist != "Singer" || item.Subtitle != "Song • Singer" {
			t.Errorf("%s: Artist = %q, subtitle %q", kind, item.Artist, item.Subtitle)
		}
	}
	album := `{"title":{"runs":[{"text":"Record"}]},"subtitle":{"runs":[{"text":"Album"},{"text":" • "},{"text":"Singer","navigationEndpoint":{"browseEndpoint":{"browseId":"UCsinger"}}}]},"navigationEndpoint":{"browseEndpoint":{"browseId":"MPREb_x"}}}`
	if item := parseMusicItem(KindMusicItem, decodeResponse(json.RawMessage(album)).(*jsonObject)); item.Artist != "" || item.Artists != "" {
		t.Errorf("an album is not a track to scrobble: artist %q artists %q", item.Artist, item.Artists)
	}
}
