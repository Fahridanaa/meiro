package main

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/elianiva/meiro/youtube"
)

func TestAccountChannelSelection(t *testing.T) {
	tests := []struct {
		name               string
		channels           []youtube.AccountChannel
		hasSelectedChannel bool
		current            string
		want               []bool
	}{
		{
			name: "YouTube marks the selected channel when channel IDs are missing",
			channels: []youtube.AccountChannel{
				{Name: "Personal", Selected: true},
				{Name: "Brand"},
				{Name: "Music"},
			},
			hasSelectedChannel: true,
			want:               []bool{true, false, false},
		},
		{
			name: "unknown empty channel IDs do not select every channel",
			channels: []youtube.AccountChannel{
				{Name: "Personal"},
				{Name: "Brand"},
			},
			want: []bool{false, false},
		},
		{
			name: "a known channel ID is the fallback when YouTube omits its selection",
			channels: []youtube.AccountChannel{
				{Name: "Personal", ChannelID: "UC-personal"},
				{Name: "Brand", ChannelID: "UC-brand"},
			},
			current: "UC-brand",
			want:    []bool{false, true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for i, channel := range test.channels {
				if got := accountChannelSelected(channel, test.hasSelectedChannel, test.current); got != test.want[i] {
					t.Errorf("channel %q selected = %t, want %t", channel.Name, got, test.want[i])
				}
			}
		})
	}
}

func TestChannelMenuSwitchesToBrandChannel(t *testing.T) {
	resetFakes()
	fakePageID.Store("")
	a := signedInApp(t)
	list, err := a.account.session.client.GetAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a.account.session.channels = list.Items
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()
	if err := tt.Click("Account"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, name := range []string{"Main channel", "Brand channel", "Music channel", "Studio channel"} {
		if !tt.HasText(name) {
			t.Fatalf("account menu omitted %q: %q", name, tt.Texts())
		}
	}
	if err := tt.Click("Brand channel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.account.session.channel; got != "UC-brand" {
		t.Errorf("active channel = %q, want UC-brand", got)
	}
	if got, _ := fakePageID.Load().(string); got != "UC-brand" {
		t.Errorf("YouTube request used page ID %q, want UC-brand", got)
	}
}
