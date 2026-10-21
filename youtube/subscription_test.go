package youtube

import (
	"encoding/json"
	"testing"
)

func TestArtistPagesSaySubscribed(t *testing.T) {
	page := func(button string) string {
		return `{"header":{"musicImmersiveHeaderRenderer":{"title":{"runs":[{"text":"A"}]},` + button + `}}}`
	}
	for _, test := range []struct {
		name, page string
		want       Subscription
	}{
		{"followed", page(`"subscriptionButton":{"subscribeButtonRenderer":{"subscribed":true,"channelId":"UC1"}}`), Subscription{"UC1", true}},
		{"not followed", page(`"subscriptionButton":{"subscribeButtonRenderer":{"subscribed":false,"channelId":"UC1"}}`), Subscription{"UC1", false}},
		{"no channel to write", page(`"subscriptionButton":{"subscribeButtonRenderer":{"subscribed":true}}`), Subscription{}},
		{"no button", page(`"x":1`), Subscription{}},
		{"not in the header", `{"contents":{"subscribeButtonRenderer":{"subscribed":true,"channelId":"UC1"}}}`, Subscription{}},
	} {
		if got := browseResult(decodeResponse(json.RawMessage(test.page))).Subscription; got != test.want {
			t.Errorf("%s: subscription %+v, want %+v", test.name, got, test.want)
		}
	}
}
