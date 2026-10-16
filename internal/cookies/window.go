package cookies

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/elianiva/meiro/internal/proc"
)

// ErrNoBrowser reports that no browser the sign-in window can use is
// installed.
var ErrNoBrowser = errors.New("no Chrome, Edge, Brave, Helium, Vivaldi, Chromium, Firefox or Zen browser was found")

// ErrWindowClosed reports that the user closed the sign-in window before the
// sign-in finished.
var ErrWindowClosed = errors.New("the sign-in window was closed before you finished signing in")

// SignInTimeout is how long a sign-in window may stay open: time to type a
// password and a second factor.
const SignInTimeout = 5 * time.Minute

// WindowOptions are the settings of a sign-in window. The zero value is the
// real sign-in; tests change the rest.
type WindowOptions struct {
	// Executable is the browser to start; "" finds one on this machine.
	Executable string
	// URL is the page the window opens.
	URL string
	// Poll is how often the window is asked for its cookies.
	Poll time.Duration
	// Settle is how long the window stays open after the sign-in shows, so the
	// page can set the rest of its cookies.
	Settle time.Duration

	// family is the family of the browser Executable names. It is
	// chromiumFamily, the zero value, for the Chromium browser a test
	// stands in with.
	family browserFamily
	// read reads the Cookie header of a Firefox-family profile. It is nil
	// for the real sign-in, which reads the profile the way an import
	// does, through yt-dlp; tests replace it so they need no yt-dlp.
	read profileReader
}

// SignInWindow has the user sign in to YouTube Music in a browser window of
// its own and returns the Cookie header of that session. The window is a new,
// empty profile, so it holds nothing of the user's own browsing, and it is
// closed and deleted as soon as the session is read.
//
// The session is taken from a window that is never used again, on purpose.
// YouTube replaces the cookies of a session as long as a browser keeps it
// open, and the old ones stop working; a session read from the user's
// everyday browser goes stale in hours, one read from a window that is then
// closed does not.
func SignInWindow(ctx context.Context, options WindowOptions) (string, error) {
	if options.URL == "" {
		options.URL = "https://music.youtube.com"
	}
	if options.Poll == 0 {
		options.Poll = time.Second
	}
	if options.Settle == 0 {
		options.Settle = 3 * time.Second
	}
	b, ok := options.browser(ctx)
	if !ok {
		return "", ErrNoBrowser
	}
	read := options.read
	if b.family == firefoxFamily && read == nil {
		// A Firefox window is read the way an import is, through yt-dlp;
		// say so now, while there is no window to clean up yet.
		if _, err := proc.Tool("yt-dlp"); err != nil {
			return "", errors.New("signing in with Firefox or Zen needs yt-dlp on PATH")
		}
		read = firefoxProfileCookie
	}
	host, arg, err := b.privateProfile()
	if err != nil {
		return "", err
	}
	defer removeAll(host)

	if b.family == firefoxFamily {
		if err := writeFirefoxPreferences(host, b.kind); err != nil {
			return "", err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, SignInTimeout)
	defer cancel()
	cmd := proc.Command(ctx, b.path, b.windowArgs(arg, options.URL)...)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start the browser: %w", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	// Whatever happens, the window does not outlive the sign-in.
	defer func() {
		cancel()
		<-exited
	}()

	w, err := b.open(ctx, host, exited, read)
	if err != nil {
		return "", err
	}
	header, err := waitSignIn(ctx, w.read, options, exited)
	if err != nil {
		return "", err
	}
	w.quit(ctx)
	return header, nil
}

// browser is the browser to open the sign-in window in: the executable the
// options name, which a test stands in with, or the one found on this
// machine.
func (o WindowOptions) browser(ctx context.Context) (candidate, bool) {
	if o.Executable != "" {
		return candidate{family: o.family, path: o.Executable}, true
	}
	return findBrowser(ctx)
}

// firefoxProfileCookie reads a profile through yt-dlp, as an import does.
func firefoxProfileCookie(ctx context.Context, profile string) (string, error) {
	return ytDlpCookie(ctx, "firefox:"+profile)
}

// writeFirefoxPreferences turns off the first-run pages a fresh profile would
// open over the sign-in page. Chromium gets the same from --no-first-run and
// --no-default-browser-check. Zen shows a welcome screen of its own until its
// preference is set.
func writeFirefoxPreferences(profile, kind string) error {
	prefs := `user_pref("browser.aboutwelcome.enabled", false);
user_pref("browser.shell.checkDefaultBrowser", false);
user_pref("browser.startup.homepage_override.mstone", "ignore");
`
	if kind == "zen" {
		prefs += "user_pref(\"zen.welcome-screen.seen\", true);\n"
	}
	return os.WriteFile(filepath.Join(profile, "user.js"), []byte(prefs), 0o600)
}

// cookieDatabaseModTime is the later modification time of a Firefox profile's
// cookie database and its write-ahead log. The zero time means the profile has
// neither yet.
func cookieDatabaseModTime(profile string) time.Time {
	var latest time.Time
	for _, name := range []string{"cookies.sqlite", "cookies.sqlite-wal"} {
		if info, err := os.Stat(filepath.Join(profile, name)); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

// windowArgs are the flags that open url on a private profile. profile is
// the path the browser sees, not the host path.
func (b candidate) windowArgs(profile, url string) []string {
	switch b.family {
	case chromiumFamily:
		return chromiumWindowArgs(profile, url)
	case firefoxFamily:
		return firefoxWindowArgs(profile, url)
	default:
		panic("unknown browser family")
	}
}

// chromiumWindowArgs are the browser's flags. The profile is new, so no
// first-run page, default-browser prompt or keyring prompt gets in the way of
// the sign-in. The window is not "incognito": a private window keeps its
// cookies in a separate store the debugging port does not read by default,
// and this profile is deleted just the same.
func chromiumWindowArgs(profile, url string) []string {
	args := []string{
		"--user-data-dir=" + profile,
		"--remote-debugging-port=0",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		// A browser with a debugging port reports navigator.webdriver as
		// true, which Google reads as software automation and answers with
		// "This browser or app may not be secure". Here a person signs in,
		// and the debugging port only reads the cookies afterwards, so the
		// page is told what a normal browser would say.
		"--disable-blink-features=AutomationControlled",
	}
	switch runtime.GOOS {
	case "linux":
		args = append(args, "--password-store=basic")
	case "darwin":
		args = append(args, "--use-mock-keychain")
	}
	return append(args, url)
}

// firefoxWindowArgs are the flags of a Firefox-family browser: a private
// profile, and a process of its own so a browser already running does not
// take the window over. Firefox writes no debugging port; its session is
// read from the cookie database the profile writes.
func firefoxWindowArgs(profile, url string) []string {
	return []string{"-profile", profile, "-no-remote", url}
}

// privateProfile creates the throwaway profile the sign-in window opens in.
// It returns the host path, which this app reads and deletes, and the path to
// give the browser, which differs only for a Flatpak browser: its sandbox
// cannot see this app's temporary directory.
func (b candidate) privateProfile() (host, arg string, err error) {
	if b.flatpak == nil {
		dir, err := proc.MkdirTemp(signinProfilePrefix)
		if err != nil {
			return "", "", err
		}
		return dir, dir, nil
	}
	if err := os.MkdirAll(b.flatpak.host, 0o700); err != nil {
		return "", "", err
	}
	proc.SweepStale(b.flatpak.host, signinProfilePrefix)
	dir, err := os.MkdirTemp(b.flatpak.host, signinProfilePrefix)
	if err != nil {
		return "", "", err
	}
	return dir, filepath.Join(b.flatpak.sandbox, filepath.Base(dir)), nil
}

// open returns the way to read the session of a browser started on profile,
// once it is reachable. exited closes when the browser process ends. read is
// the Firefox-family profile reader; a Chromium window does not use it.
func (b candidate) open(ctx context.Context, profile string, exited <-chan struct{}, read profileReader) (*browserWindow, error) {
	switch b.family {
	case chromiumFamily:
		port, path, err := waitDebugPort(ctx, profile, exited)
		if err != nil {
			return nil, err
		}
		ws, err := dialWS(ctx, net.JoinHostPort("127.0.0.1", port), path)
		if err != nil {
			return nil, err
		}
		tools := &devtools{ws: ws}
		return &browserWindow{
			read: func(ctx context.Context) (string, error) {
				all, err := tools.cookies(ctx)
				if err != nil {
					return "", err
				}
				if !hasSession(all) {
					return "", nil
				}
				return youtubeHeader(all, time.Now()), nil
			},
			quit: func(ctx context.Context) {
				// A polite quit lets the browser write what it holds; the
				// cookie is read already, so a slow one is stopped by the
				// deferred cancel.
				closeCtx, stop := context.WithTimeout(ctx, 2*time.Second)
				tools.closeBrowser(closeCtx)
				stop()
				_ = ws.Close()
			},
		}, nil
	case firefoxFamily:
		// The reader runs yt-dlp, which makes a request of its own, so ask
		// it again only after the profile's cookie store changes.
		var readAt time.Time
		var cached string
		return &browserWindow{
			read: func(ctx context.Context) (string, error) {
				changedAt := cookieDatabaseModTime(profile)
				if changedAt.IsZero() {
					return "", nil
				}
				if changedAt.After(readAt) {
					// A window that cannot be read yet is not a closed window;
					// its process ending is what closes it. A failed read leaves
					// readAt alone so the next poll tries again.
					if header, err := read(ctx, profile); err == nil {
						readAt = changedAt
						cached = ""
						if strings.Contains(header, "SAPISID=") {
							cached = header
						}
					}
				}
				return cached, nil
			},
			quit: func(context.Context) {},
		}, nil
	default:
		panic("unknown browser family")
	}
}

// waitDebugPort waits for the browser to say where its debugging port is.
// The browser writes DevToolsActivePort into the profile: the port, then the
// path of the browser's endpoint.
func waitDebugPort(ctx context.Context, profile string, exited <-chan struct{}) (port, path string, err error) {
	file := filepath.Join(profile, "DevToolsActivePort")
	for {
		if data, readErr := os.ReadFile(file); readErr == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) >= 2 {
				if _, convErr := strconv.Atoi(strings.TrimSpace(lines[0])); convErr == nil {
					return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), nil
				}
			}
		}
		select {
		case <-exited:
			return "", "", errors.New("the browser quit before it opened the sign-in window")
		case <-ctx.Done():
			return "", "", windowContextError(ctx)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// waitSignIn polls the window's session until a signed-in one shows and stays
// for the settle time, then returns that Cookie header.
func waitSignIn(ctx context.Context, read func(context.Context) (string, error), options WindowOptions, exited <-chan struct{}) (string, error) {
	var signedAt time.Time
	for {
		header, err := read(ctx)
		switch {
		case err != nil && ctx.Err() != nil:
			return "", windowContextError(ctx)
		case err != nil:
			// The connection drops when the user closes a Chromium window;
			// the Firefox reader reports no error.
			return "", ErrWindowClosed
		}
		if header != "" {
			if signedAt.IsZero() {
				signedAt = time.Now()
			}
			if time.Since(signedAt) >= options.Settle {
				return header, nil
			}
		}
		select {
		case <-exited:
			return "", ErrWindowClosed
		case <-ctx.Done():
			return "", windowContextError(ctx)
		case <-time.After(options.Poll):
		}
	}
}

// hasSession tells whether the cookies hold a signed-in YouTube session.
func hasSession(all []devtoolsCookie) bool {
	for _, c := range all {
		if c.Name == "SAPISID" && c.Value != "" && strings.HasSuffix(strings.TrimPrefix(c.Domain, "."), "youtube.com") {
			return true
		}
	}
	return false
}

func windowContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("the sign-in window stayed open too long; try again")
	}
	return ctx.Err()
}

// removeAll deletes a profile folder. A browser that is still quitting, and on
// Windows its file locks, can hold files for a moment, so it tries again.
func removeAll(dir string) {
	for range 20 {
		if os.RemoveAll(dir) == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// browserFamily tells how the sign-in window reads the session of a browser.
type browserFamily uint8

const (
	// chromiumFamily reads the session over the Chrome DevTools Protocol.
	chromiumFamily browserFamily = iota
	// firefoxFamily reads the session from the browser's cookie store, the
	// same way importing from Firefox does.
	firefoxFamily
)

// profileReader reads the Cookie header of a Firefox-family profile.
type profileReader func(ctx context.Context, profile string) (string, error)

// flatpakProfile is how a Flatpak browser's sandbox sees the data directory
// its private profiles are made in. Flatpak does not share this app's
// temporary directory, and it renames the persisted directory; both facts
// live here, where discovery sets them.
type flatpakProfile struct {
	// host is the host directory the private profile is created under.
	host string
	// sandbox is the directory the sandbox shows host at.
	sandbox string
}

// browserWindow is a started sign-in window whose session can be read.
type browserWindow struct {
	// read returns the Cookie header of the session the window holds, ""
	// while the user is not signed in. An error means the window cannot be
	// reached, which is how a Chromium window the user closed shows.
	read func(ctx context.Context) (string, error)
	// quit lets the browser close politely before the deferred cancel stops
	// it. A browser with no remote close does nothing here.
	quit func(ctx context.Context)
}

// candidate is a browser that may open the sign-in window.
type candidate struct {
	// kind names the browser for default-browser preference.
	kind string
	// family tells how its session is read.
	family browserFamily
	// path is an absolute path, or a command to find on PATH.
	path string
	// flatpak is set when the browser runs in a Flatpak sandbox. nil means
	// the browser sees the app's temporary directory, which is what a
	// normal install gets.
	flatpak *flatpakProfile
}

// findBrowser is the browser to open the sign-in window in: the user's
// default browser when it is one of these, and otherwise the first installed.
func findBrowser(ctx context.Context) (candidate, bool) {
	for _, c := range preferring(browserCandidates(), defaultBrowserKind(ctx)) {
		if c.installed() {
			return c, true
		}
	}
	return candidate{}, false
}

// browserCandidates lists the browsers of this system, Chromium-based ones
// first so a machine with no readable default keeps the browser it used
// before.
func browserCandidates() []candidate {
	return append(chromiumCandidates(), firefoxCandidates()...)
}

// installed reports whether the browser is on this machine. An absolute path
// is checked as it is; anything else is a command on PATH.
func (b candidate) installed() bool {
	if filepath.IsAbs(b.path) {
		info, err := os.Stat(b.path)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(b.path)
	return err == nil
}

// firefoxCandidates lists the Firefox-family browsers of this system. A
// Flatpak browser needs more than its path: its exported command is what this
// app runs, but its private profile goes under the data directory the sandbox
// exposes, and the browser is told the sandbox path.
func firefoxCandidates() []candidate {
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		home = ""
	}
	var out []candidate
	switch runtime.GOOS {
	case "darwin":
		apps := []string{"/Applications"}
		if home != "" {
			apps = append(apps, filepath.Join(home, "Applications"))
		}
		for _, dir := range apps {
			out = append(out,
				candidate{kind: "firefox", family: firefoxFamily, path: filepath.Join(dir, "Firefox.app", "Contents", "MacOS", "firefox")},
				candidate{kind: "zen", family: firefoxFamily, path: filepath.Join(dir, "Zen.app", "Contents", "MacOS", "zen")},
			)
		}
	case "windows":
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			root := os.Getenv(env)
			if root == "" {
				continue
			}
			out = append(out, candidate{kind: "firefox", family: firefoxFamily, path: filepath.Join(root, "Mozilla Firefox", "firefox.exe")})
		}
	default:
		out = append(out,
			candidate{kind: "firefox", family: firefoxFamily, path: "firefox"},
			candidate{kind: "zen", family: firefoxFamily, path: "zen"},
			candidate{kind: "zen", family: firefoxFamily, path: "zen-browser"},
		)
		if home == "" {
			break
		}
		// The exported wrapper runs flatpak, so it is only usable while the
		// CLI is installed.
		if _, err := exec.LookPath("flatpak"); err != nil {
			break
		}
		for _, exports := range []string{
			filepath.Join(home, ".local", "share", "flatpak", "exports", "bin"),
			"/var/lib/flatpak/exports/bin",
		} {
			out = append(out, candidate{
				kind: "zen", family: firefoxFamily,
				path: filepath.Join(exports, zenFlatpak),
				flatpak: &flatpakProfile{
					host:    filepath.Join(home, ".var", "app", zenFlatpak, ".zen"),
					sandbox: filepath.Join(home, ".zen"),
				},
			})
		}
	}
	return out
}

// preferring moves the candidates of one kind of browser to the front. The
// others keep their order, so a default browser that is not installed in a
// form this finds, or is not one of these, still leaves a browser to use.
func preferring(all []candidate, kind string) []candidate {
	if kind == "" {
		return all
	}
	out := make([]candidate, 0, len(all))
	for _, c := range all {
		if c.kind == kind {
			out = append(out, c)
		}
	}
	for _, c := range all {
		if c.kind != kind {
			out = append(out, c)
		}
	}
	return out
}

// browserKind names the browser an identifier belongs to, or "" for one this
// app cannot drive. The identifier is whatever the system calls its default
// browser: a desktop file on Linux ("google-chrome.desktop"), a bundle ID on
// macOS ("com.google.chrome") or a ProgID on Windows ("ChromeHTML").
func browserKind(id string) string {
	id = strings.ToLower(id)
	// Chromium goes before Chrome: its names, unlike Chrome's, hold both.
	for _, kind := range []struct{ kind, mark string }{
		{"chromium", "chromium"},
		{"chrome", "chrome"},
		{"edge", "edge"},
		{"edge", "msedge"},
		{"brave", "brave"},
		{"vivaldi", "vivaldi"},
		{"helium", "helium"},
		{"firefox", "firefox"},
		{"zen", "zen"},
	} {
		if strings.Contains(id, kind.mark) {
			return kind.kind
		}
	}
	return ""
}

// defaultBrowserKind asks the system which browser the user chose to open
// links, and names its kind when the sign-in window can use it. It gives ""
// when the default is another browser or cannot be read.
func defaultBrowserKind(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "linux":
		out, err := proc.Command(ctx, "xdg-settings", "get", "default-web-browser").Output()
		if err != nil {
			return ""
		}
		return browserKind(string(out))
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		plist := filepath.Join(home, "Library", "Preferences", "com.apple.LaunchServices", "com.apple.launchservices.secure.plist")
		out, err := proc.Command(ctx, "plutil", "-convert", "json", "-o", "-", "--", plist).Output()
		if err != nil {
			return ""
		}
		return browserKind(macHTTPSHandler(out))
	case "windows":
		out, err := proc.Command(ctx, "reg", "query", `HKCU\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice`, "/v", "ProgId").Output()
		if err != nil {
			return ""
		}
		// The answer is "    ProgId    REG_SZ    ChromeHTML": the ID is the
		// last word.
		fields := strings.Fields(string(out))
		if len(fields) == 0 {
			return ""
		}
		return browserKind(fields[len(fields)-1])
	}
	return ""
}

// macHTTPSHandler is the bundle ID of the app that opens https links, from
// the LaunchServices preferences converted to JSON. It is "" when the user
// never chose one, which leaves Safari.
func macHTTPSHandler(prefs []byte) string {
	var parsed struct {
		Handlers []struct {
			Scheme string `json:"LSHandlerURLScheme"`
			App    string `json:"LSHandlerRoleAll"`
		} `json:"LSHandlers"`
	}
	if json.Unmarshal(prefs, &parsed) != nil {
		return ""
	}
	for _, h := range parsed.Handlers {
		if h.Scheme == "https" {
			return h.App
		}
	}
	return ""
}

func chromiumCandidates() []candidate {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		var out []candidate
		for _, apps := range []string{"/Applications", filepath.Join(home, "Applications")} {
			for _, b := range []struct{ kind, name string }{
				{"chrome", "Google Chrome"},
				{"edge", "Microsoft Edge"},
				{"brave", "Brave Browser"},
				{"chromium", "Chromium"},
				{"vivaldi", "Vivaldi"},
				{"helium", "Helium"},
			} {
				out = append(out, candidate{kind: b.kind, path: filepath.Join(apps, b.name+".app", "Contents", "MacOS", b.name)})
			}
		}
		return out
	case "windows":
		var out []candidate
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			root := os.Getenv(env)
			if root == "" {
				continue
			}
			out = append(out,
				candidate{kind: "chrome", path: filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe")},
				candidate{kind: "edge", path: filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe")},
				candidate{kind: "brave", path: filepath.Join(root, "BraveSoftware", "Brave-Browser", "Application", "brave.exe")},
				candidate{kind: "chromium", path: filepath.Join(root, "Chromium", "Application", "chrome.exe")},
				candidate{kind: "vivaldi", path: filepath.Join(root, "Vivaldi", "Application", "vivaldi.exe")},
				// Helium's folder is a best guess; it is not checked on a
				// Windows machine.
				candidate{kind: "helium", path: filepath.Join(root, "imput", "Helium", "Application", "chrome.exe")},
				candidate{kind: "helium", path: filepath.Join(root, "Helium", "Application", "chrome.exe")})
		}
		return out
	default:
		return []candidate{
			{kind: "chrome", path: "google-chrome"},
			{kind: "chrome", path: "google-chrome-stable"},
			{kind: "chromium", path: "chromium"},
			{kind: "chromium", path: "chromium-browser"},
			{kind: "edge", path: "microsoft-edge"},
			{kind: "edge", path: "microsoft-edge-stable"},
			{kind: "brave", path: "brave-browser"},
			{kind: "brave", path: "brave"},
			{kind: "vivaldi", path: "vivaldi"},
			{kind: "vivaldi", path: "vivaldi-stable"},
			// Helium's own packages name the command helium, the AUR one
			// helium-browser.
			{kind: "helium", path: "helium"},
			{kind: "helium", path: "helium-browser"},
		}
	}
}
