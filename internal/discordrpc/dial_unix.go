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

func dialDiscord(ctx context.Context) (net.Conn, error) {
	directories := []string{
		os.Getenv("XDG_RUNTIME_DIR"),
		os.Getenv("TMPDIR"),
		os.Getenv("TMP"),
		os.Getenv("TEMP"),
		"/tmp",
	}
	directories = slices.Compact(directories)
	var lastErr error
	for _, directory := range directories {
		if directory == "" {
			continue
		}
		for index := range 10 {
			path := filepath.Join(directory, "discord-ipc-"+string(rune('0'+index)))
			conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(ctx, "unix", path)
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
