package ui

import (
	"testing"
)

// #311 — the startup notice reaches the UI as a one-time toast.
func TestStartupNotice(t *testing.T) {
	if cmd := (App{}).startupNoticeCmd(); cmd != nil {
		t.Fatal("no notice must produce no command")
	}
	cmd := App{}.WithStartupNotice("Recovered 1 interrupted build(s)").startupNoticeCmd()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg, ok := cmd().(startupNoticeMsg)
	if !ok || msg.text != "Recovered 1 interrupted build(s)" {
		t.Fatalf("got %#v", cmd())
	}
	a, tick := App{}.setToastFor("hello", startupNoticeDuration)
	if a.toast != "hello" || tick == nil {
		t.Fatalf("toast %q, tick %v", a.toast, tick)
	}
}
