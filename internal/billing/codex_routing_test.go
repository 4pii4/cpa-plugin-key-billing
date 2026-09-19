package billing

import (
	"errors"
	"testing"
)

func TestCodexRoutingEditsAreValidatedIsolatedAndAtomic(t *testing.T) {
	store, repo := newStoreWithRepository(t)
	ref := CredentialFingerprint("dummy-reserve")
	roles := map[string]string{ref: "reserve"}
	if err := store.SetCodexRouting(CodexRouting{Enabled: true, Roles: roles}); err != nil {
		t.Fatal(err)
	}
	roles[ref] = "primary"
	config := store.CodexRouting()
	if !config.Enabled || config.Roles[ref] != "reserve" {
		t.Fatal(config)
	}
	config.Roles[ref] = "primary"
	if store.CodexRouting().Roles[ref] != "reserve" {
		t.Fatal("read exposed mutable state")
	}
	for _, bad := range []CodexRouting{{Roles: map[string]string{"plaintext": "reserve"}}, {Roles: map[string]string{ref: "unknown"}}} {
		if err := store.SetCodexRouting(bad); err == nil {
			t.Fatal("invalid role accepted")
		}
	}
	repo.fail = errors.New("disk full")
	if err := store.SetCodexRouting(CodexRouting{}); !errors.Is(err, repo.fail) {
		t.Fatal(err)
	}
	if !store.CodexRouting().Enabled {
		t.Fatal("failed save changed live policy")
	}
	if !store.dirty.empty() {
		t.Fatal("failed edit entered usage queue")
	}
	merged := (Changes{CodexRouting: true}).merge(Changes{Routes: true})
	if !merged.CodexRouting || !merged.Routes || merged.empty() {
		t.Fatal(merged)
	}
}
