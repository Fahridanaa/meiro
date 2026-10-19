package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A browse session against a fake InnerTube server: home, album, search,
// then a signed-in playlist write, all through the public client.
func TestClientSessionAgainstFakeInnerTube(t *testing.T) {
	var writes []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		raw, _ := json.Marshal(body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/search"):
			_, _ = w.Write([]byte(`{"contents":{"musicShelfRenderer":{"contents":[{"musicResponsiveListItemRenderer":{"videoId":"track-1","flexColumns":[{"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[{"text":"First track"}]}}},{"musicResponsiveListItemFlexColumnRenderer":{"text":{"runs":[{"text":"An artist"}]}}}]}}]}}}`))
		case strings.HasSuffix(r.URL.Path, "/browse/edit_playlist"):
			writes = append(writes, body)
			_, _ = w.Write([]byte(`{"status":"STATUS_SUCCEEDED"}`))
		case strings.Contains(string(raw), "FEmusic_home"):
			_, _ = w.Write([]byte(`{"contents":{"sectionListRenderer":{"contents":[{"musicCarouselShelfRenderer":{"header":{"musicCarouselShelfBasicHeaderRenderer":{"title":{"runs":[{"text":"Quick picks"}]}}},"contents":[{"musicTwoRowItemRenderer":{"title":{"runs":[{"text":"Ambient One"}]},"navigationEndpoint":{"watchEndpoint":{"videoId":"vid-1"}}}}]}}]}}}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path+" "+string(raw), http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	client := NewClient(Options{BaseURL: server.URL, APIKey: "k"})

	home, err := client.GetHomeFeed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Sections) == 0 || home.Sections[0].Title != "Quick picks" || len(home.Sections[0].Items) != 1 || home.Sections[0].Items[0].VideoID != "vid-1" {
		t.Fatalf("home = %#v", home)
	}

	found, err := client.Search(ctx, "ambient", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Items) != 1 || found.Items[0].Title != "First track" {
		t.Fatalf("search = %#v", found.Items)
	}

	auth, err := NewCookieAuth("SAPISID=abc", CookieOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Cookies only go to YouTube over HTTPS, so the signed-in client dials
	// the fake server from behind a YouTube URL.
	redirect := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		target, _ := url.Parse(server.URL)
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(r)
	})}
	signedIn := NewClient(Options{BaseURL: "https://music.youtube.com", APIKey: "k", CookieAuth: auth, HTTPClient: redirect})
	if err := signedIn.AddToPlaylist(ctx, "PLhype", "vid-1"); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 || writes[0]["playlistId"] != "PLhype" {
		t.Fatalf("writes = %#v", writes)
	}
	if err := signedIn.RemoveFromPlaylist(ctx, "VLPLhype", "set-video-1"); err != nil {
		t.Fatalf("removal = %v", err)
	}
	if len(writes) != 2 || writes[1]["playlistId"] != "PLhype" {
		t.Fatalf("removal writes = %#v", writes)
	}
	actions, ok := writes[1]["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("removal actions = %#v", writes[1]["actions"])
	}
	action, ok := actions[0].(map[string]any)
	if !ok || action["action"] != "ACTION_REMOVE_VIDEO" || action["setVideoId"] != "set-video-1" {
		t.Fatalf("removal action = %#v", actions[0])
	}
	if err := client.RemoveFromPlaylist(ctx, "PLhype", "set-video-1"); err == nil {
		t.Fatal("signed-out removal succeeded")
	}
	if err := signedIn.RemoveFromPlaylist(ctx, "", "set-video-1"); err == nil {
		t.Fatal("removal without a playlist succeeded")
	}
	if err := signedIn.RemoveFromPlaylist(ctx, "PLhype", " "); err == nil {
		t.Fatal("removal without an entry ID succeeded")
	}

	if _, err := client.GetAlbum(ctx, "MPREb_missing"); err == nil {
		t.Error("an HTTP failure from the server was not reported")
	}
}

func TestContinueBrowseReusesIssuedVisitorData(t *testing.T) {
	const visitorData = "visitor-issued-by-server"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/browse") {
			t.Errorf("request path = %q, want browse", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requestContext, ok := body["context"].(map[string]any)
		if !ok {
			t.Fatalf("request context = %#v", body["context"])
		}
		client, ok := requestContext["client"].(map[string]any)
		if !ok || client["clientName"] != defaultMusicContext {
			t.Errorf("request client = %#v, want %q", requestContext["client"], defaultMusicContext)
		}
		switch body["browseId"] {
		case "FEmusic_home":
			if r.Header.Get("X-Goog-Visitor-Id") != "" {
				t.Errorf("initial visitor header = %q, want none", r.Header.Get("X-Goog-Visitor-Id"))
			}
			_, _ = w.Write([]byte(`{"responseContext":{"visitorData":"visitor-issued-by-server"},"contents":{"sectionListRenderer":{"contents":[]}}}`))
		case nil:
			if body["continuation"] != "home-more" {
				t.Errorf("body continuation = %v, want home-more", body["continuation"])
			}
			if client["visitorData"] != visitorData {
				t.Errorf("continuation context visitorData = %v, want issued visitor", client["visitorData"])
			}
			if got := r.Header.Get("X-Goog-Visitor-Id"); got != visitorData {
				t.Errorf("continuation visitor header = %q, want issued visitor", got)
			}
			_, _ = w.Write([]byte(`{"continuationContents":{"sectionListContinuation":{"contents":[{"musicCarouselShelfRenderer":{"header":{"musicCarouselShelfBasicHeaderRenderer":{"title":{"runs":[{"text":"More picks"}]}}},"contents":[{"musicTwoRowItemRenderer":{"title":{"runs":[{"text":"More Ambient"}]},"subtitle":{"runs":[{"text":"Someone"}]},"navigationEndpoint":{"watchEndpoint":{"videoId":"vid-2"}}}}]}}]}}}`))
		default:
			t.Errorf("unexpected browse payload: %#v", body)
			http.Error(w, "unexpected browse", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := NewClient(Options{BaseURL: server.URL})
	if _, err := client.GetHomeFeed(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := client.ContinueBrowse(context.Background(), "home-more")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sections) != 1 || result.Sections[0].Title != "More picks" {
		t.Fatalf("sections = %#v, want More picks", result.Sections)
	}
	if len(result.Sections[0].Items) != 1 || result.Sections[0].Items[0].Title != "More Ambient" {
		t.Fatalf("items = %#v, want More Ambient", result.Sections[0].Items)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A request that gets no answer is told apart from YouTube answering badly,
// so the app can say the network is out of reach.
func TestUnreachableIsToldFromABadAnswer(t *testing.T) {
	status := http.StatusInternalServerError
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", status)
	}))
	client := NewClient(Options{BaseURL: server.URL, APIKey: "k"})
	if _, err := client.GetHomeFeed(context.Background()); err == nil || errors.Is(err, ErrUnreachable) {
		t.Fatalf("HTTP 500: err = %v, want a failure that is not ErrUnreachable", err)
	}
	server.Close()
	if _, err := client.GetHomeFeed(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("server gone: err = %v, want ErrUnreachable", err)
	}
}
