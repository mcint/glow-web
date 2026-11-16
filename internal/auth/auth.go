// Package auth is glow-web's one HTTP Basic gate: Apache-style htpasswd
// with bcrypt hashes, re-read when the file changes, failing closed on any
// misconfiguration (specs/unified-listener.md §6).
package auth

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/crypto/bcrypt"
)

// Realm is the one Basic realm for a whole glow-web port, so browsers
// prompt once rather than per project.
const Realm = "glow-web"

// dummy is compared against when the user is unknown, so a miss costs the
// same bcrypt time as a wrong password and timing doesn't reveal users.
// It is built on first use: at package init it would add a bcrypt's worth
// of startup time to every glow-web command.
var dummy = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("glow-web-dummy"), bcrypt.DefaultCost)
	return h
})

// Gate guards handlers with the htpasswd file at Path.
type Gate struct {
	Path string
	// Optional: a missing file means "no auth" (the state-dir default,
	// enforced from the moment the file appears). Otherwise a missing file
	// is a misconfiguration and every request fails closed.
	Optional bool

	mu     sync.Mutex
	stamp  string            // identity of the loaded file: mtime|size|mode, "" = absent
	users  map[string][]byte // user → bcrypt hash
	err    error             // load error: fail closed while set
	loaded bool
	ok     map[[32]byte]string // sha256(user:pass) → user, verified; cleared on reload
}

// Check loads the file now and reports any configuration problem. It is
// for startup, so a bad file is a loud error rather than a page of 500s.
func (g *Gate) Check() error {
	_, err := g.state()
	return err
}

// Enabled reports whether requests currently need credentials.
func (g *Gate) Enabled() bool {
	users, err := g.state()
	return err != nil || users != nil
}

// Wrap returns next behind the gate.
func (g *Gate) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		users, err := g.state()
		if err != nil {
			http.Error(w, "glow-web: auth is misconfigured; refusing all requests (see the server log)", http.StatusInternalServerError)
			return
		}
		if users == nil { // optional and absent
			next.ServeHTTP(w, r)
			return
		}
		user, pass, has := r.BasicAuth()
		if !has || !g.verify(users, user, pass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="`+Realm+`", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gate) verify(users map[string][]byte, user, pass string) bool {
	key := sha256.Sum256([]byte(user + "\x00" + pass))
	g.mu.Lock()
	if u, hit := g.ok[key]; hit && u == user {
		g.mu.Unlock()
		return true
	}
	g.mu.Unlock()
	hash, known := users[user]
	if !known {
		_ = bcrypt.CompareHashAndPassword(dummy(), []byte(pass))
		return false
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(pass)) != nil {
		return false
	}
	g.mu.Lock()
	if g.ok != nil {
		g.ok[key] = user
	}
	g.mu.Unlock()
	return true
}

// state returns the current users (nil when optional and absent),
// reloading when the file's identity changed.
func (g *Gate) state() (map[string][]byte, error) {
	fi, statErr := os.Stat(g.Path)
	stamp := ""
	if statErr == nil {
		stamp = fmt.Sprintf("%d|%d|%o", fi.ModTime().UnixNano(), fi.Size(), fi.Mode().Perm())
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.loaded && stamp == g.stamp {
		return g.users, g.err
	}
	g.loaded, g.stamp, g.ok = true, stamp, map[[32]byte]string{}
	g.users, g.err = g.load(fi, statErr)
	return g.users, g.err
}

func (g *Gate) load(fi os.FileInfo, statErr error) (map[string][]byte, error) {
	if errors.Is(statErr, os.ErrNotExist) {
		if g.Optional {
			return nil, nil
		}
		return nil, fmt.Errorf("auth: %s does not exist (create it with `glow-web passwd USER >> %s`)", g.Path, g.Path)
	}
	if statErr != nil {
		return nil, fmt.Errorf("auth: %w", statErr)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("auth: %s is group- or world-writable (%o); refusing it (chmod 600 %s)", g.Path, fi.Mode().Perm(), g.Path)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return nil, fmt.Errorf("auth: %s is owned by uid %d, not %d; refusing it", g.Path, st.Uid, os.Getuid())
	}
	b, err := os.ReadFile(g.Path)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	users := map[string][]byte{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for n := 1; sc.Scan(); n++ {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		user, hash, found := strings.Cut(l, ":")
		if !found || user == "" || hash == "" {
			return nil, fmt.Errorf("auth: %s:%d: want user:hash", g.Path, n)
		}
		if !isBcrypt(hash) {
			return nil, fmt.Errorf("auth: %s:%d: user %q: only bcrypt hashes ($2a$/$2b$/$2y$) are supported; make one with `glow-web passwd %s` or `htpasswd -nB %s`", g.Path, n, user, user, user)
		}
		users[user] = []byte(hash)
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("auth: %s has no users; refusing to run an empty gate (delete the file to turn auth off)", g.Path)
	}
	return users, nil
}

func isBcrypt(h string) bool {
	if len(h) < 4 {
		return false
	}
	switch h[:4] {
	case "$2a$", "$2b$", "$2y$":
		_, err := bcrypt.Cost([]byte(h))
		return err == nil
	}
	return false
}

// HashLine returns one htpasswd line, "user:<bcrypt>", at bcrypt's
// default cost.
func HashLine(user, pass string) (string, error) {
	if user == "" || strings.ContainsAny(user, ":\n\r") {
		return "", fmt.Errorf("invalid user name %q (no ':' or newlines)", user)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return user + ":" + string(h), nil
}
