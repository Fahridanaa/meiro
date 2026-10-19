# Discord Rich Presence

When enabled and Discord is running, Meiro shares the current track, artist,
cover and elapsed time. Paused or stopped playback clears the activity. The
feature is off by default.

## Sub-features

- `discord-presence-settings` saves the opt-in switch. Meiro supplies its application ID in the app build.
- `discord-presence-playback` publishes the current track, artist, artwork and elapsed timestamp.
- `discord-presence-clear` clears the activity on pause, stop, disable or app exit.
- `discord-presence-reconnect` retries when Discord is not running or its IPC connection closes.
- `discord-presence-flatpak` finds the socket a Flatpak Discord exports under `$XDG_RUNTIME_DIR/app/<id>/`.

## How to get to it (user POV)

- Open Settings → Desktop.
- Enable `Discord Rich Presence`.
- Play a track while the Discord desktop client is running.

## Driving it with the headless tester

Preconditions:

- Use `newTestApp(t)` and navigate to `/settings`.
- The RPC protocol test uses an in-memory local pipe. It does not need Discord, credentials or network access.

- **Settings.** Enable `Discord Rich Presence`. The switch persists in `settings.json`; users do not enter an application ID.
- **Activity and keepalive.** `go test ./internal/discordrpc` runs the RPC handshake, verifies the `SET_ACTIVITY` payload, confirms pause sends a null activity, and checks that an idle session answers Discord's PING with PONG.
- **Native Discord.** Meiro's public application ID is bundled in `main.go`. Run Meiro and Discord on the same desktop, enable the setting, and play a public track. Confirm Discord shows the track and artist, then pause playback and confirm the activity clears.
- **Flatpak socket.** Point `XDG_RUNTIME_DIR` at a temp directory that holds `app/com.discordapp.Discord/discord-ipc-0`, listen on it, and call `discordrpc.New`. The handshake and the `SET_ACTIVITY` frame arrive at that path. This needs a real Unix listener, so it is a scratch proof, not a committed test. Set `TMPDIR`, `TMP` and `TEMP` to an existing empty directory too, because `dialDiscord` still dials the real `/tmp` and a native Discord there would answer first.
- **Settings switch.** `tt.Click` on the label misses the switch, because the row label and the switch are separate elements. Find the row with `tt.Find("Discord Rich Presence")` and click `row.X+row.W+20` at the row's middle height. Read `a.settings.DiscordPresence` and the saved `settings.json` (`"discordPresence": true`, indented) as the second view.

## Gotchas

- Discord must be running on the same machine. The activity is opt-in because it shares the track and artwork with Discord.
- The Discord Application ID is public and bundled in Meiro. Users do not need their own Discord app or ID. Discord may restrict an unapproved app to its listed testers.
- The IPC path differs by platform: Unix-domain sockets on macOS and Linux, named pipes on Windows.
- A Flatpak Discord puts its socket at `$XDG_RUNTIME_DIR/app/com.discordapp.Discord/discord-ipc-0`, not directly in the runtime directory. Meiro also checks the Canary build. The app directory can stay behind after Discord stops.
- Discord hides the activity when its own **Activity Privacy → Share your detected activities with others** setting is off. The RPC socket still opens, Discord still answers every `SET_ACTIVITY` with the stored activity, and nothing appears on the profile. Check that setting before treating a missing card as a Meiro bug. Discord's `renderer_js.log` shows the evidence: `[RPCServer:IPC] Socket Opened: N 1561435721456296087`, then `Socket Emit` and `Socket Message` for that socket.
