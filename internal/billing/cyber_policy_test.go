package billing

import (
	"errors"
	"testing"
	"time"
)

func TestCyberPolicyCooldownBackoffResetAndClear(t *testing.T) {
	store, _ := newStoreWithRepository(t)
	const apiKey = "sk-cyber-policy-test-0001"
	scope := CallerScope(apiKey)
	if _, err := store.SyncKeys([]string{apiKey}, false); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCyberPolicySettings(CyberPolicySettings{Enabled: true, BaseDelaySeconds: 10}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	first := store.ObserveCyberPolicyOutcome(scope, true, now)
	if !first.Applied || first.Consecutive != 1 || !first.BlockedUntil.Equal(now.Add(10*time.Second)) {
		t.Fatalf("first observation = %+v", first)
	}
	second := store.ObserveCyberPolicyOutcome(scope, true, now.Add(time.Second))
	if second.Consecutive != 2 || !second.BlockedUntil.Equal(now.Add(21*time.Second)) {
		t.Fatalf("second observation = %+v", second)
	}
	if decision := store.CyberPolicyDecision(scope, now.Add(2*time.Second)); !decision.Blocked || decision.Consecutive != 2 {
		t.Fatalf("decision = %+v", decision)
	}

	// An in-flight non-cyber outcome breaks the streak but cannot shorten the
	// cooldown that is already protecting the key.
	store.ObserveCyberPolicyOutcome(scope, false, now.Add(3*time.Second))
	restarted := store.ObserveCyberPolicyOutcome(scope, true, now.Add(4*time.Second))
	if restarted.Consecutive != 1 || !restarted.BlockedUntil.Equal(second.BlockedUntil) {
		t.Fatalf("restarted observation = %+v", restarted)
	}

	// Waiting out a cooldown does not itself break a consecutive failure streak,
	// so the next refusal still doubles the delay.
	afterExpiry := store.ObserveCyberPolicyOutcome(scope, true, now.Add(22*time.Second))
	if afterExpiry.Consecutive != 2 || !afterExpiry.BlockedUntil.Equal(now.Add(42*time.Second)) {
		t.Fatalf("post-expiry observation = %+v", afterExpiry)
	}
	// A non-cyber result after expiry clears the stale streak; the next refusal
	// starts again at the configured base delay.
	store.ObserveCyberPolicyOutcome(scope, false, now.Add(43*time.Second))
	afterSuccess := store.ObserveCyberPolicyOutcome(scope, true, now.Add(44*time.Second))
	if afterSuccess.Consecutive != 1 || !afterSuccess.BlockedUntil.Equal(now.Add(54*time.Second)) {
		t.Fatalf("post-success observation = %+v", afterSuccess)
	}
	cleared, err := store.ClearCyberPolicyBan(scope)
	if err != nil || !cleared || store.CyberPolicyDecision(scope, now.Add(23*time.Second)).Blocked {
		t.Fatalf("clear = %v, %v", cleared, err)
	}
	if cleared, err = store.ClearCyberPolicyBan(scope); err != nil || cleared {
		t.Fatalf("idempotent clear = %v, %v", cleared, err)
	}

	store.ObserveCyberPolicyOutcome(scope, true, now.Add(40*time.Second))
	if err := store.SetCyberPolicySettings(CyberPolicySettings{BaseDelaySeconds: 10}); err != nil {
		t.Fatal(err)
	}
	if status := store.CyberPolicyStatus(now.Add(41 * time.Second)); status.Settings.Enabled || len(status.Bans) != 0 {
		t.Fatalf("disabling protection retained a stale cooldown: %+v", status)
	}
}

func TestCyberPolicySettingsAreValidatedAndAtomic(t *testing.T) {
	store, repo := newStoreWithRepository(t)
	if got := store.CyberPolicySettings(); got.Enabled || got.BaseDelaySeconds != defaultCyberPolicyDelaySeconds {
		t.Fatal(got)
	}
	for _, seconds := range []int64{0, maxCyberPolicyDelaySeconds + 1} {
		if err := store.SetCyberPolicySettings(CyberPolicySettings{Enabled: true, BaseDelaySeconds: seconds}); err == nil {
			t.Fatalf("accepted delay %d", seconds)
		}
	}
	if err := store.SetCyberPolicySettings(CyberPolicySettings{Enabled: true, BaseDelaySeconds: 30}); err != nil {
		t.Fatal(err)
	}
	repo.fail = errors.New("disk full")
	if err := store.SetCyberPolicySettings(CyberPolicySettings{BaseDelaySeconds: 60}); !errors.Is(err, repo.fail) {
		t.Fatal(err)
	}
	if got := store.CyberPolicySettings(); !got.Enabled || got.BaseDelaySeconds != 30 {
		t.Fatalf("failed save changed live settings: %+v", got)
	}
	if !(Changes{CyberPolicy: true}).merge(Changes{Routes: true}).CyberPolicy {
		t.Fatal("cyber-policy change was lost during merge")
	}
}

func TestCyberPolicyDisabledDoesNotTrackOrBlock(t *testing.T) {
	store := newStore(t)
	scope := CallerScope("sk-disabled-cyber-policy")
	if observation := store.ObserveCyberPolicyOutcome(scope, true, time.Now()); observation.Applied {
		t.Fatal(observation)
	}
	if status := store.CyberPolicyStatus(time.Now()); len(status.Bans) != 0 || status.Settings.Enabled {
		t.Fatal(status)
	}
}
