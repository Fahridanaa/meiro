package youtube

import (
	"encoding/json"
	"testing"
)

func TestAccountChannelsReadBrandIDsFromIdentityTokens(t *testing.T) {
	const response = `{"accountSectionListRenderer":{"contents":[{"accountItemSectionRenderer":{"contents":[
		{"accountItemRenderer":{"accountName":{"simpleText":"Main channel"},"isSelected":true,"hasChannel":true}},
		{"accountItemRenderer":{"accountName":{"simpleText":"Brand channel"},"hasChannel":true,"serviceEndpoint":{"selectActiveIdentityEndpoint":{"supportedTokens":[
			{"accountSigninToken":{"signinUrl":"/signin?authuser=1"}},
			{"pageIdToken":{"pageId":"UC-brand"}}
		]}}}}
	]}}]}}`

	channels := extractAccountChannels(decodeResponse(json.RawMessage(response)))
	if len(channels) != 2 {
		t.Fatalf("got %d account channels, want 2: %+v", len(channels), channels)
	}
	if got := channels[0]; got.Name != "Main channel" || !got.Selected || got.ChannelID != "" {
		t.Errorf("primary account = %+v, want selected without a page ID", got)
	}
	if got := channels[1]; got.Name != "Brand channel" || got.Selected || got.ChannelID != "UC-brand" {
		t.Errorf("brand account = %+v, want page ID UC-brand", got)
	}
}
