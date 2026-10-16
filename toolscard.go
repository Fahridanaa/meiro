package main

import (
	"context"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/elianiva/meiro/internal/report"
	"github.com/elianiva/meiro/m3"
)

// loadToolVersions reads the versions of yt-dlp and ffmpeg when Settings
// opens, once: they only change with the app. Reading them runs the tools, so
// it happens off the main thread.
func (a *app) loadToolVersions() {
	if a.toolsLoaded || a.toolsLookup.busy() {
		return
	}
	version := mygo.App.Version()
	startTask(a, &a.toolsLookup, reportTimeout, func(ctx context.Context) (report.Info, error) {
		return a.collectInfo(ctx, version), nil
	}, func(info report.Info, _ error) {
		a.tools, a.toolsLoaded = info, true
	})
}

// toolsCard shows the versions of the playback programs the app finds. yt-dlp
// is refreshed with every release in the full build, so its version tells
// whether a bug report comes from a stale bundle or system install.
func (a *app) toolsCard(c *ui.Context) {
	a.settingsCard(c, "Playback tools", m3.IconSettings, func() {
		yt, ff := "Looking up…", "Looking up…"
		if a.toolsLoaded {
			yt, ff = a.tools.YtDlp, a.tools.FFmpeg
		}
		settingLabel(c, "yt-dlp "+yt, "Finds the audio of each track. The full build includes it; the minimal build uses PATH and needs Node.js or Bun.")
		settingLabel(c, "ffmpeg "+ff, "Decodes audio for playback. The full build includes it; the minimal build uses the version on PATH.")
	})
}
