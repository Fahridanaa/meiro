//go:build windows

package discordrpc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func dialDiscord(ctx context.Context) (net.Conn, error) {
	var lastErr error
	for index := range 10 {
		pipe := fmt.Sprintf("\\\\?\\pipe\\discord-ipc-%d", index)
		pipeCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		conn, err := winio.DialPipeContext(pipeCtx, pipe)
		cancel()
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}
