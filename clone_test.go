package httpclient

import (
	"net/http"
	"testing"
)

// TestSiblingClonesDoNotShareOptions: Clone used to append into the parent's
// backing array, so the second sibling overwrote the first one's option.
func TestSiblingClonesDoNotShareOptions(t *testing.T) {
	parent := New(BaseURL("http://parent.local"))

	first := parent.Clone(BaseURL("http://first.local"))
	second := parent.Clone(BaseURL("http://second.local"))

	if first.BaseURL != "http://first.local" {
		t.Fatalf("the first clone was overwritten by its sibling: %s", first.BaseURL)
	}
	if second.BaseURL != "http://second.local" {
		t.Fatalf("unexpected second clone base url: %s", second.BaseURL)
	}
	if parent.BaseURL != "http://parent.local" {
		t.Fatalf("the parent was mutated: %s", parent.BaseURL)
	}
}

// TestCloneOfACloneKeepsTheLaterOptions: options applied after NoOption were
// run but never recorded, so they vanished on the next Clone.
func TestCloneOfACloneKeepsTheLaterOptions(t *testing.T) {
	base := New(BaseURL("http://base.local"), Timeout(0))

	reset := base.Clone(NoOption, BaseURL("http://reset.local"))
	if reset.BaseURL != "http://reset.local" {
		t.Fatalf("unexpected base url after NoOption: %s", reset.BaseURL)
	}

	again := reset.Clone()
	if again.BaseURL != "http://reset.local" {
		t.Fatalf("the option applied after NoOption was lost on the next clone: %q", again.BaseURL)
	}
}

// TestCloneDoesNotShareTheRequestHandlerList guards the other slice-aliasing path.
func TestCloneDoesNotShareTheRequestHandlerList(t *testing.T) {
	parent := New()
	clone := parent.Clone()

	clone.RegisterRequestHandler(recordingHandler{onBegin: func(*http.Request) {}})

	if len(parent.requestHandlers) != 0 {
		t.Fatalf("registering on the clone touched the parent: %d handlers", len(parent.requestHandlers))
	}
}

// TestPersistentRequestOptionsAreNotSharedWithThePackageDefaults: New used to
// hand every client the same package-level slice, which the option appended to.
func TestPersistentRequestOptionsAreNotSharedWithThePackageDefaults(t *testing.T) {
	before := len(defaultRequestOptions)

	c := New(PersistentRequestOptions(func(*http.Request) error { return nil }))
	if len(c.PersistentRequestOptions) != before+1 {
		t.Fatalf("expected the client to carry one extra option, got %d", len(c.PersistentRequestOptions))
	}

	if len(defaultRequestOptions) != before {
		t.Fatalf("the package defaults were extended: %d, want %d", len(defaultRequestOptions), before)
	}

	fresh := New()
	if len(fresh.PersistentRequestOptions) != before {
		t.Fatalf("a new client inherited the other client's option: %d", len(fresh.PersistentRequestOptions))
	}
}
