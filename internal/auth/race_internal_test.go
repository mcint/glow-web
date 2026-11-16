package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Security review finding (2026-11-16): a verify that started against the
// old users could insert into the cache *after* a reload cleared it, so a
// removed user kept access. This test forces that interleaving.
func TestCache_StaleInsertDoesNotOutliveRemoval(t *testing.T) {
	p := filepath.Join(t.TempDir(), "htpasswd")
	h, _ := bcrypt.GenerateFromPassword([]byte("b"), bcrypt.MinCost)
	keep, _ := bcrypt.GenerateFromPassword([]byte("a"), bcrypt.MinCost)
	write := func(body string, at time.Time) {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, at, at)
	}
	t0 := time.Now().Add(-time.Hour)
	write("ann:"+string(keep)+"\nbob:"+string(h)+"\n", t0)
	g := &Gate{Path: p}

	oldUsers, err := g.state() // request 1 reads users {ann, bob}
	if err != nil {
		t.Fatal(err)
	}
	write("ann:"+string(keep)+"\n", t0.Add(time.Minute)) // bob removed
	newUsers, _ := g.state()                             // reload clears the cache

	if !g.verify(oldUsers, "bob", "b") { // request 1 finishes late and caches bob
		t.Fatal("precondition: bob verified against the old users")
	}
	if g.verify(newUsers, "bob", "b") {
		t.Error("removed user accepted from a stale cache entry")
	}
}
