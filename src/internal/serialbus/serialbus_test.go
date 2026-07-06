package serialbus

import (
	"testing"
	"time"

	"solis2mqtt/src/internal/config"
)

func TestParityChar(t *testing.T) {
	cases := []struct{ in, want string }{
		{"none", "N"},
		{"None", "N"},
		{"", "N"},
		{"even", "E"},
		{"Even", "E"},
		{"odd", "O"},
		{"ODD", "O"},
		{"bogus", "N"},
	}
	for _, c := range cases {
		if got := parityChar(c.in); got != c.want {
			t.Errorf("parityChar(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestManager_Bus(t *testing.T) {
	links := []config.Link{
		{LinkID: "link1", LinkType: "modbusRTU", LinkName: "/dev/ttyS0"},
		{LinkID: "link2", LinkType: "modbusRTU", LinkName: "/dev/ttyS1"},
	}
	mgr := NewManager(links, time.Second)

	if _, ok := mgr.Bus("link1"); !ok {
		t.Error("expected link1 to be found")
	}
	if _, ok := mgr.Bus("link2"); !ok {
		t.Error("expected link2 to be found")
	}
	if _, ok := mgr.Bus("nope"); ok {
		t.Error("expected unknown linkId to be not found")
	}

	// Buses were never connected; closing them must still be safe.
	mgr.CloseAll()
}
