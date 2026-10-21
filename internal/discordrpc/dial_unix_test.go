//go:build darwin || linux

package discordrpc

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDialDiscordFindsTheFlatpakSocket(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Flatpak Discord runs on Linux")
	}
	root, err := os.MkdirTemp("", "meiro-flatpak-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	flatpakDir := filepath.Join(root, "app", "com.discordapp.Discord")
	if err := os.MkdirAll(flatpakDir, 0o755); err != nil {
		t.Fatal(err)
	}
	socketPath := ipcPath(flatpakDir, 0)
	if len(socketPath) >= 100 {
		t.Fatalf("socket path is %d bytes, too long for a Unix socket: %q", len(socketPath), socketPath)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	missing := filepath.Join(root, "no-tmp")
	t.Setenv("XDG_RUNTIME_DIR", root)
	t.Setenv("TMPDIR", missing)
	t.Setenv("TMP", missing)
	t.Setenv("TEMP", missing)

	conn, err := dialDiscord(context.Background())
	if err != nil {
		t.Fatalf("dialDiscord: %v", err)
	}
	defer conn.Close()

	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	if _, err := conn.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 1)
	if _, err := io.ReadFull(server, payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "x" {
		t.Fatalf("payload = %q, want %q", payload, "x")
	}
	if got := conn.RemoteAddr().String(); got != socketPath {
		t.Fatalf("remote address = %q, want %q", got, socketPath)
	}
}

func TestDialDiscordPrefersTheNativeSocket(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Flatpak Discord runs on Linux")
	}
	root, err := os.MkdirTemp("", "meiro-flatpak-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	nativePath := ipcPath(root, 0)
	flatpakDir := filepath.Join(root, "app", "com.discordapp.Discord")
	if err := os.MkdirAll(flatpakDir, 0o755); err != nil {
		t.Fatal(err)
	}
	flatpakPath := ipcPath(flatpakDir, 0)
	for _, path := range []string{nativePath, flatpakPath} {
		if len(path) >= 100 {
			t.Fatalf("socket path is %d bytes, too long for a Unix socket: %q", len(path), path)
		}
	}

	nativeListener, err := net.Listen("unix", nativePath)
	if err != nil {
		t.Fatal(err)
	}
	defer nativeListener.Close()
	flatpakListener, err := net.Listen("unix", flatpakPath)
	if err != nil {
		t.Fatal(err)
	}
	defer flatpakListener.Close()

	missing := filepath.Join(root, "no-tmp")
	t.Setenv("XDG_RUNTIME_DIR", root)
	t.Setenv("TMPDIR", missing)
	t.Setenv("TMP", missing)
	t.Setenv("TEMP", missing)

	conn, err := dialDiscord(context.Background())
	if err != nil {
		t.Fatalf("dialDiscord: %v", err)
	}
	defer conn.Close()

	if got := conn.RemoteAddr().String(); got != nativePath {
		t.Fatalf("remote address = %q, want %q", got, nativePath)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		server, err := nativeListener.Accept()
		if err == nil {
			accepted <- server
		}
	}()
	select {
	case server := <-accepted:
		server.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("the dialled connection did not reach the native socket")
	}
}

func ipcPath(directory string, index int) string {
	return filepath.Join(directory, "discord-ipc-"+string(rune('0'+index)))
}
