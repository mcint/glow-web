package main

import (
	"flag"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/policy"
)

// Rung 6: --hub=auto leases only when the hub runs this exact build (it
// would render with its own code); otherwise it proxies, which works for
// any hub on the same contract major.
func TestAutoMode(t *testing.T) {
	for _, c := range []struct {
		hubUp              bool
		hubVer, mine, want string
	}{
		{true, "v1.0.0", "v1.0.0", "lease"},
		{true, "v1.0.0", "v1.0.1", "proxy"},
		{true, "v1.0.0+dirty", "v1.0.0+dirty", "lease"},
		{false, "", "v1.0.0", "proxy"},
	} {
		if got := autoMode(c.hubUp, c.hubVer, c.mine); got != c.want {
			t.Errorf("autoMode(%v, %q, %q) = %q, want %q", c.hubUp, c.hubVer, c.mine, got, c.want)
		}
	}
}

// Security review (2026-11-16): auth must not depend on which process holds
// the hub role. A hub-joining session can't bring its own htpasswd (any
// successor would drop it), so the flag is refused rather than ignored.
func TestAuthFlagWithHubRefused(t *testing.T) {
	if err := checkAuthFlag("off", "/x/htpasswd"); err != nil {
		t.Errorf("standalone with --auth-htpasswd: %v", err)
	}
	if err := checkAuthFlag("lease", ""); err != nil {
		t.Errorf("hub mode without the flag: %v", err)
	}
	for _, m := range []string{"lease", "proxy", "auto"} {
		err := checkAuthFlag(m, "/x/htpasswd")
		if err == nil || !strings.Contains(err.Error(), "htpasswd") || !strings.Contains(err.Error(), "state") {
			t.Errorf("--hub=%s with --auth-htpasswd: err = %v, want refusal naming <state>/htpasswd", m, err)
		}
	}
}

// specs/write-policy.md §2: --writable=no|md|yes (alias --writeable),
// default no; --readonly is --writable=no and conflicts with anything else.
func TestWritableFlags(t *testing.T) {
	cases := []struct {
		args    []string
		want    policy.Write
		wantErr string
	}{
		{nil, policy.No, ""},
		{[]string{"--writable=md"}, policy.MD, ""},
		{[]string{"--writeable=yes"}, policy.Yes, ""},
		{[]string{"--readonly"}, policy.No, ""},
		{[]string{"--readonly", "--writable=no"}, policy.No, ""},
		{[]string{"--readonly", "--writable=md"}, policy.No, "--readonly"},
		{[]string{"--writable=md", "--writeable=yes"}, policy.No, "both"},
		{[]string{"--writable=bogus"}, policy.No, "no|md|yes"},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		sf := addServerFlags(fs)
		if err := fs.Parse(c.args); err != nil {
			t.Fatalf("%v: parse: %v", c.args, err)
		}
		got, err := sf.writePolicy()
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%v: err = %v, want containing %q", c.args, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%v: got %v, %v; want %v", c.args, got, err, c.want)
		}
	}
}
