package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
