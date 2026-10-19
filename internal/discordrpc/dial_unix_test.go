//go:build darwin || linux

package discordrpc

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

func TestDiscordSocketDirsOrder(t *testing.T) {
	runtimeDir := "/run/user/1000"
	stableDir := filepath.Join(runtimeDir, "app", "com.discordapp.Discord")
	canaryDir := filepath.Join(runtimeDir, "app", "com.discordapp.DiscordCanary")
	for _, tt := range []struct {
		name       string
		runtimeDir string
		fallbacks  []string
		want       []string
	}{
		{
			name:       "Flatpak apps follow the runtime directory",
			runtimeDir: runtimeDir,
			fallbacks:  []string{"", "/tmp", "/tmp"},
			want:       []string{runtimeDir, stableDir, canaryDir, "/tmp"},
		},
		{
			name:      "no runtime directory means no Flatpak apps",
			fallbacks: []string{"/tmp"},
			want:      []string{"/tmp"},
		},
		{
			name:       "a fallback equal to the runtime directory is skipped",
			runtimeDir: runtimeDir,
			fallbacks:  []string{runtimeDir, "/tmp"},
			want:       []string{runtimeDir, stableDir, canaryDir, "/tmp"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := discordSocketDirs(tt.runtimeDir, tt.fallbacks)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("discordSocketDirs(%q, %q) = %q, want %q", tt.runtimeDir, tt.fallbacks, got, tt.want)
			}
		})
	}
}

func TestDialDiscordCandidatesUsesTheFlatpakSocket(t *testing.T) {
	runtimeDir := t.TempDir()
	socketPath := ipcPath(filepath.Join(runtimeDir, "app", "com.discordapp.Discord"), 0)
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	dialer := &recordingDialer{conn: client, okPath: socketPath}

	conn, err := dialDiscordCandidates(context.Background(), discordSocketDirs(runtimeDir, []string{"/tmp"}), dialer.dial)
	if err != nil {
		t.Fatalf("dialDiscordCandidates: %v", err)
	}
	if conn != client {
		t.Fatalf("connection = %v, want the fake dialer's connection", conn)
	}
	want := append(ipcPaths(runtimeDir), socketPath)
	if !slices.Equal(dialer.paths, want) {
		t.Fatalf("attempts = %q, want %q", dialer.paths, want)
	}
	for index := range 10 {
		if slices.Contains(dialer.paths, ipcPath("/tmp", index)) {
			t.Fatalf("attempted fallback path %q before the Flatpak socket: %q", ipcPath("/tmp", index), dialer.paths)
		}
	}
}

func TestDialDiscordCandidatesPreferTheNativeSocket(t *testing.T) {
	runtimeDir := t.TempDir()
	nativePath := ipcPath(runtimeDir, 0)
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	dialer := &recordingDialer{conn: client, okPath: nativePath}

	conn, err := dialDiscordCandidates(context.Background(), discordSocketDirs(runtimeDir, []string{"/tmp"}), dialer.dial)
	if err != nil {
		t.Fatalf("dialDiscordCandidates: %v", err)
	}
	if conn != client {
		t.Fatalf("connection = %v, want the fake dialer's connection", conn)
	}
	if want := []string{nativePath}; !slices.Equal(dialer.paths, want) {
		t.Fatalf("attempts = %q, want %q", dialer.paths, want)
	}
}

func TestDialDiscordCandidatesReachCanary(t *testing.T) {
	runtimeDir := t.TempDir()
	stableDir := filepath.Join(runtimeDir, "app", "com.discordapp.Discord")
	canaryPath := ipcPath(filepath.Join(runtimeDir, "app", "com.discordapp.DiscordCanary"), 0)
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	dialer := &recordingDialer{conn: client, okPath: canaryPath}

	conn, err := dialDiscordCandidates(context.Background(), discordSocketDirs(runtimeDir, []string{"/tmp"}), dialer.dial)
	if err != nil {
		t.Fatalf("dialDiscordCandidates: %v", err)
	}
	if conn != client {
		t.Fatalf("connection = %v, want the fake dialer's connection", conn)
	}
	want := ipcPaths(runtimeDir)
	want = append(want, ipcPaths(stableDir)...)
	want = append(want, canaryPath)
	if !slices.Equal(dialer.paths, want) {
		t.Fatalf("attempts = %q, want %q", dialer.paths, want)
	}
}

func TestDialDiscordCandidatesFailWithoutASocket(t *testing.T) {
	root := t.TempDir()
	flatpakDir := filepath.Join(root, "app", "com.discordapp.Discord")
	if err := os.MkdirAll(flatpakDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dialer := &recordingDialer{}

	_, err := dialDiscordCandidates(context.Background(), discordSocketDirs(root, nil), dialer.dial)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error = %v, want fs.ErrNotExist", err)
	}
	flatpakPath := ipcPath(flatpakDir, 0)
	if !slices.Contains(dialer.paths, flatpakPath) {
		t.Fatalf("attempts = %q, want %q among them", dialer.paths, flatpakPath)
	}
}

func TestDialDiscordCandidatesStopOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dialer := &recordingDialer{err: fs.ErrNotExist}

	_, err := dialDiscordCandidates(ctx, []string{t.TempDir()}, dialer.dial)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(dialer.paths) != 1 {
		t.Fatalf("dialed %d paths, want exactly 1: %q", len(dialer.paths), dialer.paths)
	}
}

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

type recordingDialer struct {
	conn   net.Conn
	okPath string
	err    error
	paths  []string
}

func (d *recordingDialer) dial(_ context.Context, path string) (net.Conn, error) {
	d.paths = append(d.paths, path)
	if path == d.okPath {
		return d.conn, nil
	}
	if d.err != nil {
		return nil, d.err
	}
	return nil, fs.ErrNotExist
}

func ipcPath(directory string, index int) string {
	return filepath.Join(directory, "discord-ipc-"+string(rune('0'+index)))
}

func ipcPaths(directory string) []string {
	var paths []string
	for index := range 10 {
		paths = append(paths, ipcPath(directory, index))
	}
	return paths
}
