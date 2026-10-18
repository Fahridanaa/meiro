# Discord Rich Presence

When enabled and Discord is running, Meiro shares the current track, artist,
cover and elapsed time. Paused or stopped playback clears the activity. The
feature is off by default.

## Sub-features

- `discord-presence-settings` saves the opt-in switch. Meiro supplies its application ID in the app build.
- `discord-presence-playback` publishes the current track, artist, artwork and elapsed timestamp.
- `discord-presence-clear` clears the activity on pause, stop, disable or app exit.
- `discord-presence-reconnect` retries when Discord is not running or its IPC connection closes.

## How to get to it (user POV)

- Open Settings → Desktop.
- Enable `Discord Rich Presence`.
- Play a track while the Discord desktop client is running.

## Driving it with the headless tester

Preconditions:

- Use `newTestApp()` and navigate to `/settings`.
- The RPC protocol test uses an in-memory local pipe. It does not need Discord, credentials or network access.

- **Settings.** Enable `Discord Rich Presence`. The switch persists in `settings.json`; users do not enter an application ID.
- **Activity and keepalive.** `go test ./internal/discordrpc` runs the RPC handshake, verifies the `SET_ACTIVITY` payload, confirms pause sends a null activity, and checks that an idle session answers Discord's PING with PONG.
- **Native Discord.** Meiro's public application ID is bundled in `main.go`. Run Meiro and Discord on the same desktop, enable the setting, and play a public track. Confirm Discord shows the track and artist, then pause playback and confirm the activity clears.

## Gotchas

- Discord must be running on the same machine. The activity is opt-in because it shares the track and artwork with Discord.
- The Discord Application ID is public and bundled in Meiro. Users do not need their own Discord app or ID. Discord may restrict an unapproved app to its listed testers.
- The IPC path differs by platform: Unix-domain sockets on macOS and Linux, named pipes on Windows.
