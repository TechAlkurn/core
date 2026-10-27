package reqctx

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestContext_WithFrom(t *testing.T) {
	if _, ok := From(context.Background()); ok {
		t.Error("empty context must have no identity")
	}
	ctx := With(context.Background(), Identity{UserID: 7, Lang: "ar"})
	if id, ok := From(ctx); !ok || id.UserID != 7 || id.Lang != "ar" {
		t.Errorf("From: got %+v, %v", id, ok)
	}
}

func TestBind_ReleaseClears(t *testing.T) {
	release := Bind(Identity{UserID: 5, Lang: "fr"}, "req")
	if UserID() != 5 || Lang() != "fr" || Request() != "req" || !IsOwner(5) {
		t.Errorf("bound: got user=%d lang=%q req=%v", UserID(), Lang(), Request())
	}
	release()
	if UserID() != 0 || Lang() != "" || Request() != nil {
		t.Errorf("after release: got user=%d lang=%q req=%v", UserID(), Lang(), Request())
	}
}

func TestUnbound_IsAnonymous(t *testing.T) {
	if UserID() != 0 || IsOwner(1) {
		t.Error("an unbound goroutine must be anonymous")
	}
	SetUserID(9) // no-op when nothing is bound
	if UserID() != 0 {
		t.Error("SetUserID must not bind a new identity")
	}
}

func TestSetUserID_KeepsLangAndRequest(t *testing.T) {
	defer Bind(Identity{UserID: 5, Username: "old", Lang: "fr"}, "req")()
	SetUserID(0)
	if got := Current(); got.UserID != 0 || got.Username != "" || got.Lang != "fr" {
		t.Errorf("got %+v", got)
	}
	if Request() != "req" {
		t.Error("request must survive SetUserID")
	}
}

// The bug this package fixes: with one global map, concurrent requests read
// each other's user. Every goroutine must see only its own identity.
func TestBind_ConcurrentGoroutinesIsolated(t *testing.T) {
	var wg sync.WaitGroup
	for i := uint32(1); i <= 100; i++ {
		wg.Add(1)
		go func(user uint32) {
			defer wg.Done()
			defer Bind(Identity{UserID: user}, user)()
			for range 50 {
				if got := UserID(); got != user {
					t.Errorf("goroutine for user %d saw %d", user, got)
					return
				}
				if got := Request(); got != user {
					t.Errorf("goroutine for user %d saw request %v", user, got)
					return
				}
				time.Sleep(10 * time.Microsecond)
			}
		}(i)
	}
	wg.Wait()
}

func TestGo_PropagatesIdentity(t *testing.T) {
	defer Bind(Identity{UserID: 3, Lang: "ar"}, "req")()
	done := make(chan Identity)
	var req any
	Go(func() {
		req = Request()
		done <- Current()
	})
	got := <-done
	if got.UserID != 3 || got.Lang != "ar" || req != "req" {
		t.Errorf("child saw %+v req=%v", got, req)
	}
}

func TestGo_ReleasesAfterwards(t *testing.T) {
	// The child's binding must be removed when fn returns, or a reused
	// goroutine id could inherit it. Check the store is back to empty.
	defer Bind(Identity{UserID: 3}, nil)()
	before := storeLen()
	done := make(chan struct{})
	Go(func() { close(done) })
	<-done
	deadline := time.Now().Add(time.Second)
	for storeLen() != before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n := storeLen(); n != before {
		t.Errorf("store size: got %d want %d (child binding leaked)", n, before)
	}
}

func TestGo_WithoutIdentityIsAnonymous(t *testing.T) {
	done := make(chan uint32)
	Go(func() { done <- UserID() })
	if got := <-done; got != 0 {
		t.Errorf("got %d want 0", got)
	}
}

func TestGoid_DistinctPerGoroutine(t *testing.T) {
	main := goid()
	if main == 0 {
		t.Fatal("goid returned 0")
	}
	other := make(chan uint64)
	go func() { other <- goid() }()
	if o := <-other; o == main {
		t.Errorf("two goroutines share id %d", o)
	}
	if goid() != main {
		t.Error("goid must be stable within a goroutine")
	}
}

func storeLen() int {
	n := 0
	store.Range(func(_, _ any) bool { n++; return true })
	return n
}

// -----------------------------------------------------------------------------
// Guard: the process-wide identity map must not come back.
// -----------------------------------------------------------------------------

var banned = regexp.MustCompile(`lib\.(LoggedId|GetLoggedId|IsOwner|SetMuStorage|GetMuStorage|SetLoggedUser|UnSetLoggedUser|Has|Clear)\(`)

func TestNoGlobalIdentityCalls(t *testing.T) {
	root, err := filepath.Abs("../../..") // module root
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s", root)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".git", "node_modules", "gen":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if banned.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d uses the process-wide identity map; use reqctx instead:\n\t%s", rel, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
