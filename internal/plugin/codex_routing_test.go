package plugin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"cpa-key-billing/internal/billing"
)

func codexRoutingApp(t *testing.T) (*App, []SchedulerAuthCandidate) {
	t.Helper()
	a := newConfiguredApp(t)
	if err := a.store.SetCodexRouting(billing.CodexRouting{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	files := []hostAuthFile{}
	candidates := []SchedulerAuthCandidate{}
	for _, plan := range []string{"plus", "plus", "pro"} {
		id := fmt.Sprintf("codex-%d", len(files))
		files = append(files, hostAuthFile{ID: id, AuthIndex: id, Type: "codex", Provider: "codex", Source: "file", Path: "/dummy/" + id + ".json", AccountType: "oauth"})
		candidates = append(candidates, SchedulerAuthCandidate{ID: id, Provider: "codex", Attributes: map[string]string{"path": "/dummy/" + id + ".json", "plan_type": plan}})
	}
	a.SetHostCaller(func(method string, payload any) (json.RawMessage, error) {
		if method != hostAuthList {
			return nil, fmt.Errorf("unexpected host call %s", method)
		}
		return json.Marshal(hostAuthListResponse{Files: files})
	})
	a.syncCodexInventory()
	return a, candidates
}

func codexPick(t *testing.T, a *App, candidates []SchedulerAuthCandidate, model string) SchedulerPickResponse {
	t.Helper()
	req := SchedulerPickRequest{Model: model, Candidates: candidates}
	req.Options.Metadata = map[string]any{MetadataCallerScope: billing.CallerScope("dummy-codex-client")}
	raw, err := a.pickCredential(mustMarshal(t, req))
	if err != nil {
		t.Fatal(err)
	}
	var response SchedulerPickResponse
	decodeResult(t, raw, &response)
	return response
}

func TestCodexRouterOwnsUnrestrictedSelectionAndProtectsReserve(t *testing.T) {
	a, candidates := codexRoutingApp(t)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		pick := codexPick(t, a, candidates, "gpt-5.5")
		if !pick.Handled || pick.AuthID == "codex-2" {
			t.Fatalf("reserve spent while Plus eligible: %+v", pick)
		}
		seen[pick.AuthID] = true
	}
	if len(seen) != 2 {
		t.Fatal("primary pool did not distribute traffic", seen)
	}
	// Host retry filtering, cooldowns and model eligibility are authoritative.
	if pick := codexPick(t, a, candidates[2:], "gpt-5.5"); !pick.Handled || pick.AuthID != "codex-2" {
		t.Fatal(pick)
	}
	if err := a.store.SetCodexRouting(billing.CodexRouting{}); err != nil {
		t.Fatal(err)
	}
	if pick := codexPick(t, a, candidates, "gpt-5.5"); pick.Handled {
		t.Fatal("disabled policy took over", pick)
	}
}

func TestCodexRouterManualRolesAndScope(t *testing.T) {
	a, candidates := codexRoutingApp(t)
	roles := map[string]string{}
	for i, c := range candidates {
		roles[billing.CredentialFingerprint(c.ID)] = "reserve"
		if i == 2 {
			roles[billing.CredentialFingerprint(c.ID)] = "primary"
		}
	}
	if err := a.store.SetCodexRouting(billing.CodexRouting{Enabled: true, Roles: roles}); err != nil {
		t.Fatal(err)
	}
	if pick := codexPick(t, a, candidates, "gpt-5.5"); pick.AuthID != "codex-2" {
		t.Fatal(pick)
	}
	for _, test := range []struct {
		name   string
		change func([]SchedulerAuthCandidate)
	}{
		{"mixed", func(c []SchedulerAuthCandidate) { c[0].Provider = "claude" }},
		{"config", func(c []SchedulerAuthCandidate) { c[0].Attributes = map[string]string{"source_backend": "config"} }},
		{"apikey", func(c []SchedulerAuthCandidate) {
			c[0].Attributes = map[string]string{"path": "/dummy/key.json", "account_type": "api_key"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := append([]SchedulerAuthCandidate(nil), candidates...)
			test.change(copy)
			if pick := codexPick(t, a, copy, "gpt-5.5"); pick.Handled {
				t.Fatal("non-account route changed", pick)
			}
		})
	}
}

func TestCodexRouterDoesNotBypassCredentialRules(t *testing.T) {
	a, candidates := codexRoutingApp(t)
	scope := billing.CallerScope("dummy-codex-client")
	if _, err := a.store.SyncKeys([]string{"dummy-codex-client"}, false); err != nil {
		t.Fatal(err)
	}
	_, err := a.store.CreateRoute(billing.Route{ID: "reserve-only", Name: "Reserve only", Rule: billing.RouteRule{CredentialIDs: []string{billing.CredentialFingerprint("codex-2")}}}, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	if pick := codexPick(t, a, candidates, "gpt-5.5"); pick.AuthID != "codex-2" {
		t.Fatal("bypassed route", pick)
	}
}

func TestCodexQuotaWindowsAndMalformedData(t *testing.T) {
	now := time.Now().UTC()
	var object map[string]any
	json.Unmarshal([]byte(fmt.Sprintf(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":100,"reset_at":%d},"secondary_window":{"used_percent":100,"reset_at":%d}},"additional_rate_limits":[{"limit_name":"GPT-5.3-Codex-Spark","rate_limit":{"allowed":true,"primary_window":{"used_percent":20}}}],"code_review_rate_limit":{"limit_reached":true}}`, now.Add(time.Hour).Unix(), now.Add(48*time.Hour).Unix())), &object)
	limits := parseCodexRoutingQuota(object, now)
	if !limits["default"].blocked || limits["default"].reset.Unix() != now.Add(48*time.Hour).Unix() {
		t.Fatal("weekly exhaustion lost", limits)
	}
	if limits["spark"].blocked || !limits["review"].blocked {
		t.Fatal("quota scopes mixed", limits)
	}
	if len(parseCodexRoutingQuota(map[string]any{"rate_limit": map[string]any{"primary_window": map[string]any{"used_percent": "bad"}}}, now)) != 0 {
		t.Fatal("malformed quota treated as known")
	}
}

func TestCodexStaleExhaustionAndExclusiveProbe(t *testing.T) {
	a, candidates := codexRoutingApp(t)
	now := a.store.Now()
	for _, id := range []string{"codex-0", "codex-1"} {
		state := a.codexRouter.accounts[id]
		state.quotaAt = now
		state.quota = map[string]codexQuotaLimit{"default": {blocked: true, reset: now.Add(7 * 24 * time.Hour)}}
	}
	if pick := codexPick(t, a, candidates, "gpt-5.5"); pick.AuthID != "codex-2" {
		t.Fatal(pick)
	}
	for _, id := range []string{"codex-0", "codex-1"} {
		a.codexRouter.accounts[id].quotaAt = now.Add(-codexQuotaTTL - time.Second)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[string]int{}
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, handled := a.pickCodexCredential("test", "gpt-5.5", "pool", candidates)
			if !handled {
				t.Error("hook not handled")
			}
			mu.Lock()
			counts[id]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if counts["codex-0"] != 1 || counts["codex-1"] != 1 || counts["codex-2"] != 30 {
		t.Fatal("recovery probe stampede", counts)
	}
	// No callback or completion is required to release a lost probe lease.
	state := a.codexRouter.accounts["codex-0"]
	if ok, probe := state.availability("default", now.Add(codexProbeInterval+time.Second)); !ok || !probe {
		t.Fatal("probe stranded", ok, probe)
	}
}

func TestCodexUsageFailuresRecoveryAndOutOfOrderSuccess(t *testing.T) {
	a, candidates := codexRoutingApp(t)
	state := a.codexRouter.accounts["codex-0"]
	now := a.store.Now()
	state.quotaAt = now.Add(-time.Minute)
	state.quota = map[string]codexQuotaLimit{"default": {blocked: true, reset: now.Add(time.Hour)}}
	failure := UsageRecord{Provider: "codex", AuthIndex: "codex-0", Model: "gpt-5.5", RequestedAt: now.Add(-time.Second), Failed: true, Failure: UsageFailure{StatusCode: 429, Body: `{"error":{"type":"usage_limit_reached","resets_in_seconds":604800}}`}}
	a.observeCodexUsage(failure)
	old := failure
	old.Failed = false
	old.RequestedAt = now.Add(-2 * time.Second)
	a.observeCodexUsage(old)
	if ok, _ := state.availability("default", now); ok {
		t.Fatal("old success cleared new failure")
	}
	if ok, probe := state.availability("default", now.Add(codexQuotaTTL+time.Second)); !ok || !probe {
		t.Fatal("unexpected early reset cannot be probed")
	}
	fresh := old
	fresh.RequestedAt = now
	a.observeCodexUsage(fresh)
	if ok, probe := state.availability("default", now); !ok || probe {
		t.Fatal("live recovery did not reopen Plus", ok, probe)
	}
	if pick := codexPick(t, a, candidates[:1], "gpt-5.5"); pick.AuthID != "codex-0" {
		t.Fatal(pick)
	}
	// Account-wide auth failures affect all models, but generic 429s do not.
	failure.RequestedAt = now.Add(time.Millisecond)
	failure.Failure.StatusCode = 401
	a.observeCodexUsage(failure)
	if ok, _ := state.availability("spark", now); ok {
		t.Fatal("auth failure ignored")
	}
}

func TestCodexQuotaRefreshOrderingAndReplacement(t *testing.T) {
	a, _ := codexRoutingApp(t)
	file := a.codexRouter.accounts["codex-0"].file
	state, first := a.beginCodexQuota(file)
	_, second := a.beginCodexQuota(file)
	now := a.store.Now()
	positive := authQuotaResponse{FetchedAt: now, Plan: "plus", codexLimits: map[string]codexQuotaLimit{"default": {}}}
	negative := positive
	negative.codexLimits = map[string]codexQuotaLimit{"default": {blocked: true}}
	a.finishCodexQuota(state, second, positive)
	a.finishCodexQuota(state, first, negative)
	if state.quota["default"].blocked {
		t.Fatal("older refresh won")
	}
	_, third := a.beginCodexQuota(file)
	a.observeCodexUsage(UsageRecord{Provider: "codex", AuthIndex: file.AuthIndex, Model: "gpt-5.5", RequestedAt: now, Failed: true, Failure: UsageFailure{StatusCode: 429}})
	a.finishCodexQuota(state, third, positive)
	if !state.health["default"].failed {
		t.Fatal("in-flight quota hid live failure")
	}
	_, fourth := a.beginCodexQuota(file)
	a.finishCodexQuota(state, fourth, positive)
	if state.health["default"].failed {
		t.Fatal("fresh reset not honored")
	}
	a.observeCodexUsage(UsageRecord{Provider: "codex", AuthIndex: file.AuthIndex, Model: "gpt-5.5", RequestedAt: now.Add(-time.Minute), Failed: true, Failure: UsageFailure{StatusCode: 429}})
	if state.health["default"].failed {
		t.Fatal("old failure overwrote a fresh reset")
	}
	file.ModTime = now
	replacement, _ := a.beginCodexQuota(file)
	a.finishCodexQuota(state, fourth, negative)
	if replacement == state || !replacement.quotaAt.IsZero() {
		t.Fatal("replacement reused old quota")
	}
	file.ModTime = now.Add(-time.Hour)
	restored := a.codexRouter.accountLocked(file, true)
	if restored == replacement {
		t.Fatal("restoring a credential with an older mtime kept stale evidence")
	}
}

func TestCodexManagementQuotaFeedsSchedulerWithoutNetworkOnPick(t *testing.T) {
	a, candidates := codexRoutingApp(t)
	file := a.codexRouter.accounts["codex-0"].file
	blocked := true
	httpCalls := 0
	a.SetHostCaller(func(method string, payload any) (json.RawMessage, error) {
		switch method {
		case hostAuthGet:
			return json.Marshal(hostAuthGetResponse{JSON: json.RawMessage(`{"access_token":"dummy-test-token","account_id":"dummy-account"}`)})
		case hostHTTPDo:
			httpCalls++
			body, _ := json.Marshal(map[string]any{"plan_type": "plus", "rate_limit": map[string]any{"allowed": !blocked, "limit_reached": blocked}})
			return json.Marshal(hostHTTPResponse{StatusCode: 200, Body: body})
		default:
			return nil, fmt.Errorf("unexpected callback %s", method)
		}
	})
	if _, err := a.fetchAuthQuota("dummy-callback", file, "codex"); err != nil {
		t.Fatal(err)
	}
	pool := []SchedulerAuthCandidate{candidates[0], candidates[2]}
	if pick := codexPick(t, a, pool, "gpt-5.5"); pick.AuthID != "codex-2" {
		t.Fatal("quota query was not used", pick)
	}
	blocked = false
	if _, err := a.fetchAuthQuota("dummy-callback", file, "codex"); err != nil {
		t.Fatal(err)
	}
	if pick := codexPick(t, a, pool, "gpt-5.5"); pick.AuthID != "codex-0" {
		t.Fatal("fresh reset was ignored", pick)
	}
	if httpCalls != 2 {
		t.Fatal("scheduler performed provider HTTP", httpCalls)
	}
}

func TestCodexRoutingClockRollbackAndNoWeeklyAssumption(t *testing.T) {
	now := time.Now()
	state := &codexAccount{quotaAt: now.Add(time.Hour), quota: map[string]codexQuotaLimit{"default": {blocked: true}}, health: map[string]codexHealth{}, probeAfter: map[string]time.Time{}}
	if ok, probe := state.availability("default", now); !ok || !probe {
		t.Fatal("clock rollback stranded account")
	}
	a, candidates := codexRoutingApp(t)
	pro := a.codexRouter.accounts["codex-2"]
	pro.quotaAt = now
	pro.quota = map[string]codexQuotaLimit{"default": {blocked: true, reset: now.Add(7 * 24 * time.Hour)}}
	if id, handled := a.pickCodexCredential("test", "gpt-5.5", "pool", candidates[2:]); id != "" || !handled {
		t.Fatal("reported Pro quota ignored", id, handled)
	}
}

func TestCodexRoutingManagementAndUnknownCaller(t *testing.T) {
	a, candidates := codexRoutingApp(t)
	bad := a.putCodexRouting(ManagementRequest{Body: []byte(`{"enabled":true,"roles":{"plaintext":"reserve"}}`)})
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatal(bad)
	}
	if !a.store.CodexRouting().Enabled {
		t.Fatal("invalid edit changed settings")
	}
	req := SchedulerPickRequest{Model: "gpt-5.5", Candidates: candidates}
	raw, err := a.pickCredential(mustMarshal(t, req))
	if err != nil {
		t.Fatal(err)
	}
	var result SchedulerPickResponse
	decodeResult(t, raw, &result)
	if !result.Handled || result.AuthID == "codex-2" {
		t.Fatal("missing caller metadata bypassed reserve protection", result)
	}
}
