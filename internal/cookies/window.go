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

// errFirefoxNeedsYtDlp reports that a Firefox or Zen browser is installed,
// but yt-dlp is missing, so its session cannot be read.
var errFirefoxNeedsYtDlp = errors.New("signing in with Firefox or Zen needs yt-dlp")

// ErrWindowClosed reports that the user closed the sign-in window before the
// sign-in finished.
var ErrWindowClosed = errors.New("the sign-in window was closed before you finished signing in")

// errWindowGone marks a read that failed because the window is gone. A
// Chromium window reports it when its connection drops; a Firefox window
// is read from a file, and its process ending is what closes it.
var errWindowGone = errors.New("the sign-in window is gone")

// SignInTimeout is how long a sign-in window may stay open: time to type a
// password and a second factor.
const SignInTimeout = 5 * time.Minute

// firefoxRetry paces the Firefox profile read.
type firefoxRetry struct {
	// recheck is how long a read result of "not signed in" stays trusted
	// while the profile looks unchanged. A filesystem can record two writes
	// in one timestamp tick, so timestamps alone can miss a new cookie.
	recheck time.Duration
	// grace is how long a run of failed reads may last before the sign-in
	// reports the error. A profile that is briefly locked or slow recovers
	// on its own.
	grace time.Duration
}

const (
	firefoxReadRecheck = 5 * time.Second
	firefoxReadGrace   = 30 * time.Second
)

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
	// profileRoot is where the throwaway profile is made when Executable
	// names the browser, "" for this app's private temporary directory. A
	// test sets it to a directory it can check; a found candidate carries
	// its own.
	profileRoot string
	// read reads the Cookie header of a Firefox-family profile. It is nil
	// for the real sign-in, which reads the profile the way an import
	// does, through yt-dlp; tests replace it so they need no yt-dlp.
	read profileReader
	// retry paces the Firefox profile read. The zero value is the real
	// pacing; tests shorten it.
	retry firefoxRetry
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
	if options.retry.recheck == 0 {
		options.retry.recheck = firefoxReadRecheck
	}
	if options.retry.grace == 0 {
		options.retry.grace = firefoxReadGrace
	}
	read := options.read
	canReadFirefox := read != nil || ytDlpAvailable()
	b, ok := options.browser(ctx, canReadFirefox)
	if !ok {
		if !canReadFirefox && installedFirefox(ctx) {
			return "", errFirefoxNeedsYtDlp
		}
		return "", ErrNoBrowser
	}
	if b.family == firefoxFamily && read == nil {
		// A Firefox window is read the way an import is, through yt-dlp;
		// say so now, while there is no window to clean up yet.
		if _, err := proc.Tool("yt-dlp"); err != nil {
			return "", errFirefoxNeedsYtDlp
		}
		read = firefoxProfileCookie
	}
	profile, err := b.privateProfile()
	if err != nil {
		return "", err
	}
	defer removeAll(profile)

	if b.family == firefoxFamily {
		if err := writeFirefoxPreferences(profile, b.kind); err != nil {
			return "", err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, SignInTimeout)
	defer cancel()
	cmd := proc.Command(ctx, b.path, b.windowArgs(profile, options.URL)...)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start the browser: %w", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	// Whatever happens, the window does not outlive the sign-in. A Flatpak
	// wrapper execs the browser, so its sandbox stays in the process group
	// the deferred cancel kills.
	defer func() {
		cancel()
		<-exited
	}()

	w, err := b.open(ctx, profile, exited, read, options.retry)
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
// machine. canReadFirefox says whether a Firefox-family browser's profile can
// be read, which a found browser must be able to do.
func (o WindowOptions) browser(ctx context.Context, canReadFirefox bool) (candidate, bool) {
	if o.Executable != "" {
		return candidate{family: o.family, path: o.Executable, profileRoot: o.profileRoot}, true
	}
	return findBrowser(ctx, canReadFirefox)
}

// firefoxProfileCookie reads a profile through yt-dlp, as an import does.
func firefoxProfileCookie(ctx context.Context, profile string) (string, error) {
	return ytDlpCookie(ctx, "firefox:"+profile)
}

// ytDlpAvailable reports whether yt-dlp is on this machine.
func ytDlpAvailable() bool {
	_, err := proc.Tool("yt-dlp")
	return err == nil
}

// writeFirefoxPreferences turns off the first-run pages a fresh profile would
// open over the sign-in page. Chromium gets the same from --no-first-run and
// --no-default-browser-check. Zen shows a welcome screen of its own until its
// preference is set.
//
// The cookie write-ahead log is capped at one page. Firefox keeps a fresh
// cookie in cookies.sqlite-wal until the log passes
// network.cookie.db.maxWalBytes, and yt-dlp copies only cookies.sqlite, so
// the sign-in would miss the cookie it just made. Firefox checkpoints every
// maxWalBytes / page size pages and its page size is 32768, so one page
// checkpoints after every write.
func writeFirefoxPreferences(profile, kind string) error {
	prefs := `user_pref("browser.aboutwelcome.enabled", false);
user_pref("browser.shell.checkDefaultBrowser", false);
user_pref("browser.startup.homepage_override.mstone", "ignore");
user_pref("network.cookie.db.maxWalBytes", 32768);
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

// windowArgs are the flags that open url on a private profile.
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
// A native browser shares this app's private temporary directory. A Flatpak
// or Snap browser needs a profile under a directory its sandbox can see and
// reaches at the same path as the host.
func (b candidate) privateProfile() (string, error) {
	if b.profileRoot == "" {
		return proc.MkdirTemp(signinProfilePrefix)
	}
	if err := os.MkdirAll(b.profileRoot, 0o700); err != nil {
		return "", err
	}
	proc.SweepStale(b.profileRoot, browserHoldsProfile, signinProfilePrefix)
	return os.MkdirTemp(b.profileRoot, signinProfilePrefix)
}

// browserHoldsProfile reports whether a browser is still using the throwaway
// profile at path. Firefox writes a lock symlink and a Chromium browser
// writes SingletonLock; both go away when the browser exits. A profile a
// browser still holds must not be swept out from under it.
func browserHoldsProfile(path string) bool {
	for _, name := range []string{"lock", "SingletonLock"} {
		if _, err := os.Lstat(filepath.Join(path, name)); err == nil {
			return true
		}
	}
	return false
}

// open returns the way to read the session of a browser started on profile,
// once it is reachable. exited closes when the browser process ends. read is
// the Firefox-family profile reader; a Chromium window does not use it.
func (b candidate) open(ctx context.Context, profile string, exited <-chan struct{}, read profileReader, retry firefoxRetry) (*browserWindow, error) {
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
					return "", fmt.Errorf("%w: %w", errWindowGone, err)
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
		// The reader runs yt-dlp, which makes a request of its own, so it
		// runs when the profile's cookie store changes, or when no session
		// is cached and the recheck time has passed.
		var readAt time.Time
		var readTime time.Time
		var failedAt time.Time
		var cached string
		return &browserWindow{
			read: func(ctx context.Context) (string, error) {
				changedAt := cookieDatabaseModTime(profile)
				if changedAt.IsZero() {
					return "", nil
				}
				if !changedAt.After(readAt) && (cached != "" || time.Since(readTime) < retry.recheck) {
					return cached, nil
				}
				readTime = time.Now()
				// A window that cannot be read yet is not a closed window; its
				// process ending is what closes it. A failed read leaves readAt
				// alone so the next poll tries again, and only a run of failures
				// that outlasts the grace reports the error.
				header, err := readFirefox(ctx, read, profile, exited)
				if err != nil {
					if errors.Is(err, errWindowGone) {
						return "", err
					}
					if failedAt.IsZero() {
						failedAt = time.Now()
					}
					if time.Since(failedAt) >= retry.grace {
						return "", err
					}
					return "", nil
				}
				failedAt = time.Time{}
				readAt = changedAt
				cached = ""
				if strings.Contains(header, "SAPISID=") {
					cached = header
				}
				return cached, nil
			},
			quit: func(context.Context) {},
		}, nil
	default:
		panic("unknown browser family")
	}
}

// readFirefox runs the profile reader in the background while the poll loop
// keeps watching the window. yt-dlp makes a request of its own, so a slow
// one must not stop the loop from noticing a window the user closed.
func readFirefox(ctx context.Context, read profileReader, profile string, exited <-chan struct{}) (string, error) {
	type answer struct {
		header string
		err    error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan answer, 1)
	go func() {
		header, err := read(ctx, profile)
		done <- answer{header, err}
	}()
	select {
	case got := <-done:
		return got.header, got.err
	case <-exited:
		return "", errWindowGone
	case <-ctx.Done():
		return "", ctx.Err()
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
		case errors.Is(err, errWindowGone):
			return "", ErrWindowClosed
		case err != nil:
			return "", err
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

// browserWindow is a started sign-in window whose session can be read.
type browserWindow struct {
	// read returns the Cookie header of the session the window holds, ""
	// while the user is not signed in. A Chromium read fails when the
	// connection drops, which is how a window the user closed shows; a
	// Firefox read fails when the profile cannot be read.
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
	// profileRoot is the directory the throwaway profile is made under. ""
	// means this app's private temporary directory, which a native install
	// shares. A Flatpak or Snap browser needs a directory its sandbox sees:
	// Flatpak mounts ~/.var/app/<id> and Snap ~/snap/<name> at the same path
	// on both sides.
	profileRoot string
	// flatpakID and snapName name a packaged install, "" for a native one.
	flatpakID string
	snapName  string
	// desktopID is the .desktop file of this install on Linux, "" for a
	// native one. Flatpak exports <flatpakID>.desktop, Snap
	// <snapName>_<snapName>.desktop.
	desktopID string
}

// findBrowser is the browser to open the sign-in window in: the user's
// default browser when it is one of these, and otherwise the first installed.
// A Firefox-family browser is skipped unless canReadFirefox says its profile
// can be read.
func findBrowser(ctx context.Context, canReadFirefox bool) (candidate, bool) {
	id := defaultBrowserID(ctx)
	for _, c := range preferring(browserCandidates(), id, browserKind(id)) {
		if !canReadFirefox && c.family == firefoxFamily {
			continue
		}
		if c.installed(ctx) {
			return c, true
		}
	}
	return candidate{}, false
}

// installedFirefox reports whether any Firefox-family browser is installed.
func installedFirefox(ctx context.Context) bool {
	for _, c := range browserCandidates() {
		if c.family == firefoxFamily && c.installed(ctx) {
			return true
		}
	}
	return false
}

// browserCandidates lists the browsers of this system, Chromium-based ones
// first so a machine with no readable default keeps the browser it used
// before.
func browserCandidates() []candidate {
	table := browserTable()
	switch runtime.GOOS {
	case "darwin":
		return darwinCandidates(table)
	case "windows":
		return windowsCandidates(table)
	default:
		return linuxCandidates(table)
	}
}

// installed reports whether the browser is on this machine. An absolute path
// is checked as it is; anything else is a command on PATH. The package
// manager is asked as well, because an export or snap link can outlive an
// uninstall and must not win over a working native browser.
func (b candidate) installed(ctx context.Context) bool {
	if filepath.IsAbs(b.path) {
		info, err := os.Stat(b.path)
		if err != nil || info.IsDir() {
			return false
		}
	} else if _, err := exec.LookPath(b.path); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	switch {
	case b.flatpakID != "":
		return proc.Command(ctx, "flatpak", "info", b.flatpakID).Run() == nil
	case b.snapName != "":
		return proc.Command(ctx, "snap", "list", b.snapName).Run() == nil
	}
	return true
}

// preferring puts the browser the default names first: an exact .desktop
// match, then every candidate of the named kind, then the rest in table
// order. The named kind moves up whole so a native install, a macOS bundle ID
// and a Windows ProgID all resolve, and so a gone Flatpak or Snap install
// does not hide the native one behind it.
func preferring(all []candidate, desktopID, kind string) []candidate {
	named := func(c candidate) bool { return desktopID != "" && c.desktopID == desktopID }
	out := make([]candidate, 0, len(all))
	for _, c := range all {
		if named(c) {
			out = append(out, c)
		}
	}
	for _, c := range all {
		if !named(c) && c.kind == kind {
			out = append(out, c)
		}
	}
	for _, c := range all {
		if !named(c) && c.kind != kind {
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

// defaultBrowserID asks the system which browser the user chose to open
// links. It returns the lowercased identifier the system uses: the .desktop
// file on Linux, the bundle ID on macOS, the ProgID on Windows, or "" when
// it cannot be read.
func defaultBrowserID(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "linux":
		out, err := proc.Command(ctx, "xdg-settings", "get", "default-web-browser").Output()
		if err != nil {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(string(out)))
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
		return strings.ToLower(macHTTPSHandler(out))
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
		return strings.ToLower(fields[len(fields)-1])
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
		if h.Scheme == "https" && h.App != "" {
			return h.App
		}
	}
	return ""
}

// browserSpec is one browser the sign-in window can start and every
// install of it this app knows. One row per browser.
type browserSpec struct {
	kind      string
	family    browserFamily // the zero value is chromiumFamily
	commands  []string      // the PATH names of a native install
	flatpakID string        // the Flatpak application ID, "" when none
	snapName  string        // the Snap name, "" when none
	macApp    string        // the app bundle name on macOS
	macExec   string        // its executable, when that differs from macApp
	winPaths  []string      // paths under each Windows install root
}

// browserTable lists every browser the sign-in window can start, the
// Chromium-based rows first so the no-default fallback keeps the browser it
// used before.
func browserTable() []browserSpec {
	return []browserSpec{
		{
			kind:      "chrome",
			commands:  []string{"google-chrome", "google-chrome-stable"},
			flatpakID: "com.google.Chrome",
			macApp:    "Google Chrome",
			winPaths:  []string{filepath.Join("Google", "Chrome", "Application", "chrome.exe")},
		},
		{
			kind:      "chromium",
			commands:  []string{"chromium", "chromium-browser"},
			flatpakID: "org.chromium.Chromium",
			snapName:  "chromium",
			macApp:    "Chromium",
			winPaths:  []string{filepath.Join("Chromium", "Application", "chrome.exe")},
		},
		{
			kind:      "edge",
			commands:  []string{"microsoft-edge", "microsoft-edge-stable"},
			flatpakID: "com.microsoft.Edge",
			macApp:    "Microsoft Edge",
			winPaths:  []string{filepath.Join("Microsoft", "Edge", "Application", "msedge.exe")},
		},
		{
			kind:      "brave",
			commands:  []string{"brave-browser", "brave"},
			flatpakID: "com.brave.Browser",
			macApp:    "Brave Browser",
			winPaths:  []string{filepath.Join("BraveSoftware", "Brave-Browser", "Application", "brave.exe")},
		},
		{
			kind:      "vivaldi",
			commands:  []string{"vivaldi", "vivaldi-stable"},
			flatpakID: "com.vivaldi.Vivaldi",
			macApp:    "Vivaldi",
			winPaths:  []string{filepath.Join("Vivaldi", "Application", "vivaldi.exe")},
		},
		{
			kind:     "helium",
			commands: []string{"helium", "helium-browser"},
			macApp:   "Helium",
			// Helium's folder is a best guess; it is not checked on a
			// Windows machine.
			winPaths: []string{
				filepath.Join("imput", "Helium", "Application", "chrome.exe"),
				filepath.Join("Helium", "Application", "chrome.exe"),
			},
		},
		{
			kind:      "firefox",
			family:    firefoxFamily,
			commands:  []string{"firefox"},
			flatpakID: "org.mozilla.firefox",
			snapName:  "firefox",
			macApp:    "Firefox",
			macExec:   "firefox",
			winPaths:  []string{filepath.Join("Mozilla Firefox", "firefox.exe")},
		},
		{
			kind:      "zen",
			family:    firefoxFamily,
			commands:  []string{"zen", "zen-browser"},
			flatpakID: zenFlatpak,
			macApp:    "Zen",
			macExec:   "zen",
		},
	}
}

// linuxCandidates builds the native, Flatpak and Snap installs of every row.
// The Flatpak and Snap tools gate their candidates, so a machine without
// them, and a test with an empty PATH, sees neither install kind.
func linuxCandidates(table []browserSpec) []candidate {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	userExports := ""
	if home != "" {
		userExports = filepath.Join(userDataHome(home), "flatpak", "exports", "bin")
	}
	_, flatpakErr := exec.LookPath("flatpak")
	_, snapErr := exec.LookPath("snap")
	hasFlatpak := flatpakErr == nil
	hasSnap := snapErr == nil
	var out []candidate
	for _, spec := range table {
		for _, command := range spec.commands {
			out = append(out, candidate{kind: spec.kind, family: spec.family, path: command})
		}
		if home == "" {
			continue
		}
		if hasFlatpak && spec.flatpakID != "" {
			for _, root := range []string{userExports, "/var/lib/flatpak/exports/bin"} {
				out = append(out, candidate{
					kind:        spec.kind,
					family:      spec.family,
					path:        filepath.Join(root, spec.flatpakID),
					profileRoot: filepath.Join(home, ".var", "app", spec.flatpakID, "data"),
					desktopID:   strings.ToLower(spec.flatpakID) + ".desktop",
					flatpakID:   spec.flatpakID,
				})
			}
		}
		if hasSnap && spec.snapName != "" {
			out = append(out, candidate{
				kind:        spec.kind,
				family:      spec.family,
				path:        filepath.Join("/snap/bin", spec.snapName),
				profileRoot: filepath.Join(home, "snap", spec.snapName, "common"),
				desktopID:   strings.ToLower(spec.snapName+"_"+spec.snapName) + ".desktop",
				snapName:    spec.snapName,
			})
		}
	}
	return out
}

// userDataHome is the directory Flatpak names XDG_DATA_HOME: its own variable
// when set, and the ~/.local/share default otherwise.
func userDataHome(home string) string {
	if data := os.Getenv("XDG_DATA_HOME"); data != "" {
		return data
	}
	return filepath.Join(home, ".local", "share")
}

// darwinCandidates builds the app bundle install of every row under each
// Applications directory.
func darwinCandidates(table []browserSpec) []candidate {
	roots := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	var out []candidate
	for _, spec := range table {
		binary := spec.macExec
		if binary == "" {
			binary = spec.macApp
		}
		for _, root := range roots {
			out = append(out, candidate{
				kind:   spec.kind,
				family: spec.family,
				path:   filepath.Join(root, spec.macApp+".app", "Contents", "MacOS", binary),
			})
		}
	}
	return out
}

// windowsCandidates builds every install of every row under each Windows
// install root.
func windowsCandidates(table []browserSpec) []candidate {
	var out []candidate
	for _, spec := range table {
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			root := os.Getenv(env)
			if root == "" {
				continue
			}
			for _, path := range spec.winPaths {
				out = append(out, candidate{
					kind:   spec.kind,
					family: spec.family,
					path:   filepath.Join(root, path),
				})
			}
		}
	}
	return out
}
