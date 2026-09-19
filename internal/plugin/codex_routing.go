package plugin

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"cpa-key-billing/internal/billing"
)

const (
	codexInventoryTTL  = 30 * time.Second
	codexQuotaTTL      = 2 * time.Minute
	codexProbeInterval = 30 * time.Second
)

// Runtime evidence is deliberately not persisted. On restart, CPA's current
// candidate list is authoritative, not yesterday's cached quota or cooldown.
type codexRouter struct {
	inventoryMu sync.Mutex
	mu          sync.Mutex
	inventoryAt time.Time
	files       map[string]hostAuthFile
	accounts    map[string]*codexAccount
	sequence    uint64
	hookAt      time.Time
	lastPool    string
}

type codexAccount struct {
	file         hostAuthFile
	plan         string
	quotaAt      time.Time
	quota        map[string]codexQuotaLimit
	quotaRequest uint64
	evidence     uint64
	health       map[string]codexHealth
	probeAfter   map[string]time.Time
}

type codexQuotaLimit struct {
	blocked bool
	reset   time.Time
}

type codexHealth struct {
	started  time.Time
	observed time.Time
	failed   bool
	retry    time.Time
}

func (r *codexRouter) reset() {
	r.inventoryMu.Lock()
	defer r.inventoryMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files, r.accounts = nil, nil
	r.inventoryAt, r.hookAt = time.Time{}, time.Time{}
	r.lastPool = ""
}

func isCodexAccount(file hostAuthFile) bool {
	return strings.EqualFold(firstNonEmptyString(file.Provider, file.Type), "codex") &&
		!strings.EqualFold(file.AccountType, "api_key") && !file.RuntimeOnly &&
		credentialSourceFromHost(file) == billing.CredentialSourceAuthFiles
}

func codexIdentity(file hostAuthFile) hostAuthFile {
	// Routing needs identity and revision, never a cached email or token preview.
	return hostAuthFile{ID: file.ID, AuthIndex: file.AuthIndex, Type: file.Type, Provider: file.Provider,
		Source: file.Source, Path: file.Path, AccountType: file.AccountType, RuntimeOnly: file.RuntimeOnly, ModTime: file.ModTime}
}

func (r *codexRouter) accountLocked(file hostAuthFile, inventory bool) *codexAccount {
	if !isCodexAccount(file) || file.AuthIndex == "" || file.ID == "" {
		return nil
	}
	if r.accounts == nil {
		r.accounts = make(map[string]*codexAccount)
	}
	state := r.accounts[file.AuthIndex]
	if state != nil && state.file.ID == file.ID && authFileRevision(state.file) == authFileRevision(file) {
		return state
	}
	// An older management read must not undo a newer inventory revision.
	if !inventory && state != nil && state.file.ModTime.After(file.ModTime) {
		return nil
	}
	state = &codexAccount{file: codexIdentity(file), health: make(map[string]codexHealth), probeAfter: make(map[string]time.Time)}
	r.accounts[file.AuthIndex] = state
	return state
}

// host.auth.list is a local host call; no provider HTTP runs inside scheduler.pick.
func (a *App) syncCodexInventory() {
	r := &a.codexRouter
	r.inventoryMu.Lock()
	defer r.inventoryMu.Unlock()
	now := a.store.Now()
	r.mu.Lock()
	fresh := !r.inventoryAt.IsZero() && now.Sub(r.inventoryAt) >= 0 && now.Sub(r.inventoryAt) < codexInventoryTTL
	r.mu.Unlock()
	if fresh {
		return
	}
	files, err := a.listHostAuthFiles()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inventoryAt = now
	if err != nil {
		return
	}
	r.files = make(map[string]hostAuthFile)
	seen := make(map[string]bool)
	for _, file := range files {
		r.files[file.ID] = codexIdentity(file)
		if !isCodexAccount(file) {
			continue
		}
		seen[file.AuthIndex] = true
		r.accountLocked(file, true)
	}
	for index := range r.accounts {
		if !seen[index] {
			delete(r.accounts, index)
		}
	}
}

func codexQuotaScope(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(model, "codex-spark") {
		return "spark"
	}
	if strings.HasPrefix(model, "codex-auto-review") {
		return "review"
	}
	return "default"
}

// Separate Spark/review windows must never exhaust ordinary Codex models.
func parseCodexRoutingQuota(object map[string]any, at time.Time) map[string]codexQuotaLimit {
	result := make(map[string]codexQuotaLimit)
	add := func(scope string, info map[string]any) {
		if info == nil {
			return
		}
		limit := codexQuotaLimit{}
		valid := false
		if allowed, ok := boolValue(info, "allowed"); ok {
			valid = true
			limit.blocked = !allowed
		}
		if reached, ok := boolValue(info, "limit_reached", "limitReached"); ok {
			valid = true
			limit.blocked = limit.blocked || reached
		}
		for _, key := range []string{"primary_window", "secondary_window"} {
			window := objectMap(info, key, camelKey(key))
			used, ok := floatValue(window, "used_percent", "usedPercent")
			if !ok || used < 0 || used > 100 {
				continue
			}
			valid = true
			if used < 100 {
				continue
			}
			limit.blocked = true
			reset := time.Time{}
			if seconds, ok := intValue(window, "reset_at", "resetAt"); ok && seconds > 0 {
				reset = time.Unix(seconds, 0)
			}
			if reset.IsZero() {
				if seconds, ok := intValue(window, "reset_after_seconds", "resetAfterSeconds"); ok && seconds > 0 && seconds <= 31*86400 {
					reset = at.Add(time.Duration(seconds) * time.Second)
				}
			}
			// Both exhausted windows must reset; a 5h reset cannot clear a weekly block.
			if reset.After(limit.reset) {
				limit.reset = reset
			}
		}
		if valid {
			result[scope] = limit
		}
	}
	add("default", objectMap(object, "rate_limit", "rateLimit"))
	add("review", objectMap(object, "code_review_rate_limit", "codeReviewRateLimit"))
	for _, raw := range objectSlice(object, "additional_rate_limits", "additionalRateLimits") {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := strings.ToLower(firstString(item, "limit_name", "limitName"))
		if strings.Contains(name, "codex-spark") {
			add("spark", objectMap(item, "rate_limit", "rateLimit"))
		}
	}
	return result
}

func (a *App) beginCodexQuota(file hostAuthFile) (*codexAccount, uint64) {
	r := &a.codexRouter
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.accountLocked(file, false)
	if state == nil {
		return nil, 0
	}
	r.sequence++
	state.quotaRequest = r.sequence
	return state, r.sequence
}

func (a *App) finishCodexQuota(state *codexAccount, sequence uint64, result authQuotaResponse) {
	if state == nil || len(result.codexLimits) == 0 {
		return
	}
	r := &a.codexRouter
	r.mu.Lock()
	defer r.mu.Unlock()
	// Reject replaced credentials, overlapping older refreshes, and polls that
	// started before newer usage evidence. An HTTP failure never erases a snapshot.
	if r.accounts[state.file.AuthIndex] != state || state.quotaRequest != sequence || state.evidence > sequence {
		return
	}
	state.quotaAt, state.quota, state.plan = result.FetchedAt, result.codexLimits, result.Plan
	for scope, limit := range state.quota {
		if !limit.blocked {
			delete(state.health, scope)
			delete(state.probeAfter, scope)
		}
	}
}

func (a *App) observeCodexUsage(record UsageRecord) {
	if !strings.EqualFold(record.Provider, "codex") || strings.EqualFold(record.AuthType, "apikey") || record.AuthIndex == "" {
		return
	}
	r := &a.codexRouter
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.accounts[record.AuthIndex]
	if state == nil {
		return
	}
	now := a.store.Now()
	scope := codexQuotaScope(record.Model)
	if record.Failed && (record.Failure.StatusCode == 401 || record.Failure.StatusCode == 403) {
		scope = "account"
	}
	if record.Failed && record.Failure.StatusCode != 429 && scope != "account" {
		return
	}
	// Use the actual record's start time, never inferred request correlations.
	// Delayed success from an older request must not clear a newer failure.
	started := record.RequestedAt
	if !started.IsZero() && !state.file.ModTime.IsZero() && started.Before(state.file.ModTime) {
		return
	}
	if started.IsZero() || started.After(now.Add(5*time.Second)) {
		if !record.Failed {
			return
		}
		started = now
	}
	if !state.quotaAt.IsZero() && started.Before(state.quotaAt) {
		return
	}
	if previous := state.health[scope]; !previous.started.IsZero() && !started.After(previous.started) {
		return
	}
	r.sequence++
	state.evidence = r.sequence
	health := codexHealth{started: started, observed: now, failed: record.Failed}
	if record.Failed {
		health.retry = now.Add(codexProbeInterval)
		var object map[string]any
		if json.Unmarshal([]byte(record.Failure.Body), &object) == nil {
			info := objectMap(object, "error")
			if firstString(info, "type") == "usage_limit_reached" {
				health.retry = now.Add(codexQuotaTTL)
				if seconds, ok := intValue(info, "resets_at"); ok {
					reset := time.Unix(seconds, 0)
					if reset.After(now) && reset.Before(health.retry) {
						health.retry = reset
					}
				} else if seconds, ok := intValue(info, "resets_in_seconds"); ok && seconds > 0 && seconds < int64(codexQuotaTTL/time.Second) {
					health.retry = now.Add(time.Duration(seconds) * time.Second)
				}
			}
		}
	} else {
		delete(state.probeAfter, scope)
		if account := state.health["account"]; started.After(account.started) {
			delete(state.health, "account")
			delete(state.probeAfter, "account")
		}
	}
	state.health[scope] = health
}

// availability returns whether a candidate can be picked and whether that pick
// needs an exclusive, expiring recovery probe. No timers or goroutines are used.
func (state *codexAccount) availability(scope string, now time.Time) (bool, bool) {
	if state == nil {
		return true, false
	}
	probe := false
	for _, key := range []string{"account", scope} {
		health := state.health[key]
		if health.failed {
			if !now.Before(health.observed.Add(-5*time.Second)) && now.Before(health.retry) {
				return false, false
			}
			probe = true
		}
		if now.Before(state.probeAfter[key]) && !state.probeAfter[key].After(now.Add(codexProbeInterval)) {
			return false, false
		}
	}
	limit, exists := state.quota[scope]
	// Unknown model-specific windows remain unknown, not assumed exhausted by
	// an unrelated window (notably Spark vs the ordinary Codex weekly quota).
	if exists && limit.blocked && !state.health[scope].started.After(state.quotaAt) {
		recheck := state.quotaAt.Add(codexQuotaTTL)
		if !limit.reset.IsZero() && limit.reset.Before(recheck) {
			recheck = limit.reset
		}
		if !now.Before(state.quotaAt.Add(-5*time.Second)) && now.Before(recheck) {
			return false, false
		}
		probe = true
	}
	return true, probe
}

func codexPool(role, plan string) int {
	if role == "primary" {
		return 0
	}
	if role == "reserve" {
		return 2
	}
	switch normalizeCodexPlan(plan) {
	case "plus":
		return 0
	case "pro-20x":
		return 2
	default:
		return 1
	}
}

func (a *App) pickCodexCredential(scope, model, pool string, candidates []SchedulerAuthCandidate) (string, bool) {
	config := a.store.CodexRouting()
	if !config.Enabled || len(candidates) == 0 {
		return "", false
	}
	// Do not hijack other providers or API-key routes, including mixed pools.
	for _, candidate := range candidates {
		if !strings.EqualFold(candidate.Provider, "codex") || credentialSourceFromCandidate(candidate) != billing.CredentialSourceAuthFiles || strings.EqualFold(candidate.Attributes["account_type"], "api_key") || strings.EqualFold(candidate.Attributes["auth_kind"], "apikey") {
			return "", false
		}
	}
	a.syncCodexInventory()
	r := &a.codexRouter
	r.mu.Lock()
	defer r.mu.Unlock()
	now := a.store.Now()
	r.hookAt = now
	for _, candidate := range candidates {
		if file, known := r.files[candidate.ID]; known && !isCodexAccount(file) {
			return "", false
		}
	}
	quotaScope := codexQuotaScope(model)
	best := 3
	eligible := make([]SchedulerAuthCandidate, 0, len(candidates))
	states := make(map[string]*codexAccount)
	probes := make(map[string]bool)
	for _, candidate := range candidates {
		if candidateWeight(candidate) == 0 || strings.EqualFold(candidate.Status, "disabled") {
			continue
		}
		file, known := r.files[candidate.ID]
		var state *codexAccount
		if known {
			state = r.accounts[file.AuthIndex]
		}
		ok, probe := state.availability(quotaScope, now)
		if !ok {
			continue
		}
		plan := candidate.Attributes["plan_type"]
		if state != nil && !state.quotaAt.IsZero() && now.Sub(state.quotaAt) >= 0 && now.Sub(state.quotaAt) < codexQuotaTTL && state.plan != "" {
			plan = state.plan
		}
		tier := codexPool(config.Roles[billing.CredentialFingerprint(candidate.ID)], plan)
		if tier > best {
			continue
		}
		if tier < best {
			best = tier
			eligible = eligible[:0]
		}
		eligible = append(eligible, candidate)
		states[candidate.ID], probes[candidate.ID] = state, probe
	}
	id := a.scheduler.pick(scope, "codex-priority:"+pool, eligible)
	if id == "" {
		r.lastPool = "waiting"
		return "", true
	}
	r.lastPool = []string{"primary", "other", "reserve"}[best]
	if state := states[id]; state != nil && probes[id] {
		state.probeAfter[quotaScope] = now.Add(codexProbeInterval)
		if state.health["account"].failed {
			state.probeAfter["account"] = now.Add(codexProbeInterval)
		}
	}
	return id, true
}

func (a *App) codexRoutingStatus(_ ManagementRequest) ManagementResponse {
	config := a.store.CodexRouting()
	r := &a.codexRouter
	r.mu.Lock()
	hookAt, lastPool := r.hookAt, r.lastPool
	r.mu.Unlock()
	return JSONResponse(http.StatusOK, map[string]any{"settings": config, "last_hook_at": hookAt, "last_pool": lastPool})
}

func (a *App) putCodexRouting(req ManagementRequest) ManagementResponse {
	var config billing.CodexRouting
	if err := json.Unmarshal(req.Body, &config); err != nil {
		return JSONError(http.StatusBadRequest, "invalid", "Invalid Codex routing settings")
	}
	if err := a.store.SetCodexRouting(config); err != nil {
		return errorResponse(err)
	}
	return a.codexRoutingStatus(req)
}
