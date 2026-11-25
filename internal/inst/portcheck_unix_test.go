//go:build !windows

package inst

import (
	"slices"
	"testing"
)

func TestExtraBindHostsAndLoopbackConflict(t *testing.T) {
	cases := []struct {
		network, host string
		want          []string
	}{
		{"tcp", "", []string{"127.0.0.1", "::1"}},
		{"tcp4", "", []string{"127.0.0.1"}},
		{"tcp", "0.0.0.0", []string{"127.0.0.1"}},
		{"tcp", "::", []string{"::1"}},
		{"tcp", "127.0.0.1", nil},
	}
	for _, c := range cases {
		if got := extraBindHosts(c.network, c.host); !slices.Equal(got, c.want) {
			t.Errorf("extraBindHosts(%q, %q) = %v, want %v", c.network, c.host, got, c.want)
		}
	}

	_, port := occupy(t, "127.0.0.1")
	old := PortProbeHost
	t.Cleanup(func() { PortProbeHost = old })
	PortProbeHost = ""
	if err := checkBindPort(bindTarget{network: "tcp", host: "", port: port}); err == nil {
		t.Fatalf("Mac 上 127.0.0.1:%d 被占时，通配试绑也应判为占用", port)
	}
}
