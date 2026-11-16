package main

import "testing"

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
