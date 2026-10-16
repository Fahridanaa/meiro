package artwork

import (
	"net/http"
	"runtime"
	"testing"
	"time"
)

// A closed cache gives its workers back, so a program that makes many caches
// (the tests do) does not keep a goroutine, and all it holds, for each.
func TestCloseStopsTheWorkers(t *testing.T) {
	before := runtime.NumGoroutine()
	New(&http.Client{}, func() {}).Close()
	for deadline := time.Now().Add(5 * time.Second); runtime.NumGoroutine() > before; {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines after Close, %d before New", runtime.NumGoroutine(), before)
		}
		time.Sleep(time.Millisecond)
	}
}
