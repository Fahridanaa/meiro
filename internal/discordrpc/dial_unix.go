//go:build darwin || linux

package discordrpc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// flatpakDiscordIDs are the Flatpak builds whose sandbox exports a Discord IPC
// socket. Discord has no discovery protocol, so the list stays explicit.
var flatpakDiscordIDs = []string{
	"com.discordapp.Discord",
	"com.discordapp.DiscordCanary",
}

type socketDialFunc func(ctx context.Context, path string) (net.Conn, error)

func dialDiscord(ctx context.Context) (net.Conn, error) {
	return dialDiscordCandidates(ctx, discordSocketDirs(
		os.Getenv("XDG_RUNTIME_DIR"),
		[]string{os.Getenv("TMPDIR"), os.Getenv("TMP"), os.Getenv("TEMP"), "/tmp"},
	), dialUnix)
}

// discordSocketDirs lists the directories that can hold a discord-ipc-N
// socket, in dial order. A native Discord and the Flathub wiki's symlink keep
// precedence because the runtime directory comes first.
func discordSocketDirs(runtimeDir string, fallbacks []string) []string {
	var dirs []string
	if runtimeDir != "" {
		dirs = append(dirs, runtimeDir)
		for _, appID := range flatpakDiscordIDs {
			dirs = append(dirs, filepath.Join(runtimeDir, "app", appID))
		}
	}
	for _, dir := range fallbacks {
		if dir == "" || slices.Contains(dirs, dir) {
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

// dialDiscordCandidates returns the first connection among the
// discord-ipc-0..9 sockets of each directory.
func dialDiscordCandidates(ctx context.Context, directories []string, dial socketDialFunc) (net.Conn, error) {
	var lastErr error
	for _, directory := range directories {
		for index := range 10 {
			path := filepath.Join(directory, "discord-ipc-"+string(rune('0'+index)))
			conn, err := dial(ctx, path)
			if err == nil {
				return conn, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
	}
	if lastErr == nil {
		return nil, net.ErrClosed
	}
	return nil, lastErr
}

func dialUnix(ctx context.Context, path string) (net.Conn, error) {
	return (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(ctx, "unix", path)
}
