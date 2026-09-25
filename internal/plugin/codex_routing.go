package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"cpa-key-billing/internal/billing"
)

const (
	codexInventoryTTL  = 30 * time.Second
	codexQuotaTTL      = 2 * time.Minute
	codexProbeInterval = 30 * time.Second

	codexAutoPollInterval    = time.Minute
	codexAutoFailureCooldown = 15 * time.Minute
	codexAutoMinPingInterval = 10 * time.Minute
	codexAutoResetDrift      = 30 * time.Second
	codexAutoModel           = "gpt-5.6-luna"
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
	autoRunning bool
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
	auto         codexAutoState
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

type codexAutoWindow struct {
	present   bool
	usedKnown bool
	used      float64
	reset     time.Time
}

func (w codexAutoWindow) exhausted() bool {
	return w.present && w.usedKnown && w.used >= 100
}

type codexAutoQuota struct {
	valid    bool
	session  codexAutoWindow
	weekly   codexAutoWindow
	blocking bool
}

type codexAutoState struct {
	evidence  uint64
	checking  bool
	pinging   bool
	nextCheck time.Time
	checkedAt time.Time
	lastPing  time.Time
	session   codexAutoWindow
	weekly    codexAutoWindow
	status    string
	message   string
}

type reqCodexAutoStart struct {
	callbackID string
	token      string
	accountID  string
	quota      codexAutoQuota
	fetchedAt  time.Time
}

type codexAutoStartView struct {
	RoutingRef string `json:"routing_ref"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	CheckedAt  string `json:"checked_at,omitempty"`
	LastPingAt string `json:"last_ping_at,omitempty"`
	NextCheck  string `json:"next_check_at,omitempty"`
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
		Source: file.Source, Path: file.Path, AccountType: file.AccountType, RuntimeOnly: file.RuntimeOnly,
		Disabled: file.Disabled, Unavailable: file.Unavailable, ModTime: file.ModTime}
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
		state.file.Disabled, state.file.Unavailable = file.Disabled, file.Unavailable
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

// parseCodexAutoQuota keeps the two ordinary Codex windows separate. An
// inactive window advances its reset time on every usage query; an active one
// stays fixed. That distinction is the only reliable signal available without
// inventing state from request timestamps.
func parseCodexAutoQuota(object map[string]any, at time.Time) codexAutoQuota {
	info := objectMap(object, "rate_limit", "rateLimit")
	if info == nil {
		return codexAutoQuota{}
	}
	result := codexAutoQuota{valid: true}
	for index, key := range []string{"primary_window", "secondary_window"} {
		window := objectMap(info, key, camelKey(key))
		if window == nil {
			continue
		}
		parsed := codexAutoWindow{present: true}
		if used, ok := floatValue(window, "used_percent", "usedPercent"); ok && used >= 0 && used <= 100 {
			parsed.usedKnown, parsed.used = true, used
		}
		if seconds, ok := intValue(window, "reset_at", "resetAt"); ok && seconds > 0 {
			parsed.reset = time.Unix(seconds, 0).UTC()
		} else if seconds, ok := intValue(window, "reset_after_seconds", "resetAfterSeconds"); ok && seconds > 0 && seconds <= 31*86400 {
			parsed.reset = at.Add(time.Duration(seconds) * time.Second).UTC()
		}
		windowSeconds, _ := intValue(window, "limit_window_seconds", "limitWindowSeconds")
		switch {
		case windowSeconds == 5*60*60:
			result.session = parsed
		case windowSeconds == 7*24*60*60 || windowSeconds >= 28*24*60*60 && windowSeconds <= 31*24*60*60:
			result.weekly = parsed
		case index == 0 && !result.session.present:
			result.session = parsed
		case index == 1 && !result.weekly.present:
			result.weekly = parsed
		}
	}
	if result.weekly.exhausted() {
		result.blocking = true
	}
	allowed, hasAllowed := boolValue(info, "allowed")
	reached, hasReached := boolValue(info, "limit_reached", "limitReached")
	if (hasAllowed && !allowed || hasReached && reached) && !result.session.exhausted() {
		// A generic block not explained by the 5-hour window is conservatively
		// treated as a longer-term block. A later fresh usage response clears it.
		result.blocking = true
	}
	return result
}

func codexAutoWindowRestarted(previous, current codexAutoWindow) bool {
	if !previous.present || !current.present {
		return false
	}
	if previous.exhausted() && !current.exhausted() {
		return true
	}
	// Surprise global resets can arrive before a window is exhausted. A drop
	// back to the beginning is stronger evidence than schedule arithmetic.
	if previous.usedKnown && current.usedKnown && previous.used-current.used >= 1 && current.used <= 1 {
		return true
	}
	if previous.reset.IsZero() || current.reset.IsZero() {
		return false
	}
	return current.reset.Sub(previous.reset) >= codexAutoResetDrift
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

func (a *App) finishCodexAutoQuota(state *codexAccount, sequence uint64, req reqCodexAutoStart) {
	if state == nil {
		return
	}
	config := a.store.CodexRouting()
	ref := billing.CredentialFingerprint(state.file.ID)
	r := &a.codexRouter
	r.mu.Lock()
	if r.accounts[state.file.AuthIndex] != state || state.quotaRequest != sequence || state.auto.evidence > sequence {
		r.mu.Unlock()
		return
	}
	state.auto.checking = false
	if !config.AutoStart[ref] {
		state.auto = codexAutoState{}
		r.mu.Unlock()
		return
	}
	state.auto.evidence = sequence
	state.auto.checkedAt = req.fetchedAt
	previousSession, previousWeekly := state.auto.session, state.auto.weekly
	state.auto.session, state.auto.weekly = req.quota.session, req.quota.weekly
	if state.auto.nextCheck.Before(req.fetchedAt.Add(codexAutoPollInterval)) {
		state.auto.nextCheck = req.fetchedAt.Add(codexAutoPollInterval)
	}

	trigger := codexAutoWindowRestarted(previousSession, req.quota.session) ||
		codexAutoWindowRestarted(previousWeekly, req.quota.weekly)
	switch {
	case !req.quota.valid || !req.quota.session.present || req.quota.session.reset.IsZero():
		state.auto.status = "unavailable"
		state.auto.message = "The ordinary 5-hour window was not reported; no packet was sent."
	case req.quota.session.exhausted():
		state.auto.status = "blocked"
		state.auto.message = "The 5-hour window is exhausted; watching for its regular or gifted reset."
	case req.quota.blocking:
		state.auto.status = "blocked"
		state.auto.message = "The weekly window is exhausted; watching for its regular or gifted reset."
	case !previousSession.present:
		state.auto.status = "watching"
		state.auto.message = "Baseline captured; watching the 5-hour and weekly reset signals."
	case !trigger:
		state.auto.status = "watching"
		state.auto.message = "Windows are active or unchanged; no packet is needed."
	case state.auto.pinging:
		r.mu.Unlock()
		return
	case !state.auto.lastPing.IsZero() && req.fetchedAt.Sub(state.auto.lastPing) >= 0 && req.fetchedAt.Sub(state.auto.lastPing) < codexAutoMinPingInterval:
		state.auto.status = "cooldown"
		state.auto.message = "A fresh window signal was seen, but the 10-minute packet cooldown is active."
	case strings.TrimSpace(req.callbackID) == "":
		state.auto.status = "pending"
		state.auto.message = "A fresh window was detected; waiting for a host callback scope to send the packet."
	default:
		state.auto.pinging = true
		state.auto.status = "starting"
		state.auto.message = "Fresh 5-hour or weekly window detected; sending the tiny start packet."
		r.mu.Unlock()
		a.sendCodexAutoStart(state, ref, req)
		return
	}
	r.mu.Unlock()
}

func (a *App) sendCodexAutoStart(state *codexAccount, ref string, req reqCodexAutoStart) {
	body, errMarshal := json.Marshal(map[string]any{
		"model": codexAutoModel,
		"input": []any{map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": "hi"}},
		}},
		"instructions":      "Reply with OK.",
		"reasoning":         map[string]any{"effort": "none", "summary": "auto"},
		"max_output_tokens": 8,
		"store":             false,
		"stream":            true,
	})
	var errPing error
	if errMarshal != nil {
		errPing = fmt.Errorf("build tiny Codex packet: %w", errMarshal)
	} else if a.hostCaller == nil {
		errPing = fmt.Errorf("host callback is unavailable")
	} else {
		headers := http.Header{
			"Accept":        {"text/event-stream"},
			"Content-Type":  {"application/json"},
			"Originator":    {"codex_cli_rs"},
			"User-Agent":    {"codex_cli_rs/0.144.1"},
			"Authorization": {"Bearer " + req.token},
		}
		if strings.TrimSpace(req.accountID) != "" {
			headers.Set("Chatgpt-Account-Id", strings.TrimSpace(req.accountID))
		}
		raw, errCall := a.hostCaller(hostHTTPDo, hostHTTPRequest{
			HostCallbackID: req.callbackID,
			Method:         http.MethodPost,
			URL:            "https://chatgpt.com/backend-api/codex/responses",
			Headers:        headers,
			Body:           body,
		})
		if errCall != nil {
			errPing = fmt.Errorf("tiny Codex packet failed: %s", redactSecret(errCall.Error(), req.token))
		} else {
			var response hostHTTPResponse
			if errDecode := json.Unmarshal(raw, &response); errDecode != nil {
				errPing = fmt.Errorf("parse tiny Codex packet response: %w", errDecode)
			} else if response.StatusCode < 200 || response.StatusCode >= 300 {
				var object map[string]any
				_ = json.Unmarshal(response.Body, &object)
				message := redactSecret(upstreamErrorMessage(object), req.token)
				if message == "" {
					message = http.StatusText(response.StatusCode)
				}
				errPing = fmt.Errorf("tiny Codex packet returned HTTP %d: %s", response.StatusCode, message)
			} else if errStream := codexAutoStreamCompletion(response.Body); errStream != nil {
				errPing = fmt.Errorf("tiny Codex packet did not complete: %s", redactSecret(errStream.Error(), req.token))
			}
		}
	}

	now := a.store.Now()
	r := &a.codexRouter
	r.mu.Lock()
	current := r.accounts[state.file.AuthIndex]
	if current != state {
		r.mu.Unlock()
		return
	}
	state.auto.pinging = false
	if errPing != nil {
		state.auto.status = "failed"
		state.auto.message = errPing.Error()
		state.auto.nextCheck = now.Add(codexAutoFailureCooldown)
	} else {
		state.auto.status = "started"
		state.auto.message = "Tiny " + codexAutoModel + " stream completed; the fresh 5-hour and weekly windows were started."
		state.auto.lastPing = now
		state.auto.nextCheck = now.Add(codexAutoPollInterval)
	}
	r.mu.Unlock()
	if errPing != nil {
		a.store.AddPluginLog(billing.PluginLogError, "Codex window auto-start failed for credential %s: %v", ref, errPing)
		return
	}
	a.store.AddPluginLog(billing.PluginLogInfo, "Codex window auto-start completed for credential %s", ref)
}

func codexAutoStreamCompletion(body []byte) error {
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var event map[string]any
		if json.Unmarshal(data, &event) != nil {
			continue
		}
		eventType := strings.ToLower(firstString(event, "type"))
		if eventType == "response.completed" {
			return nil
		}
		if eventType == "error" || eventType == "response.failed" || eventType == "response.incomplete" {
			message := upstreamErrorMessage(event)
			if message == "" {
				message = eventType
			}
			return fmt.Errorf("%s", message)
		}
	}
	return fmt.Errorf("stream ended without response.completed")
}

// runCodexAutoStart advances one due account during a host-owned callback.
// It intentionally has no timer or goroutine: request.complete and settings
// updates provide safe activity ticks while the embedded Go runtime stays idle
// between host calls.
func (a *App) runCodexAutoStart(callbackID string) {
	if a == nil || a.store == nil || strings.TrimSpace(callbackID) == "" {
		return
	}
	config := a.store.CodexRouting()
	if len(config.AutoStart) == 0 {
		return
	}
	a.syncCodexInventory()
	now := a.store.Now()
	r := &a.codexRouter
	r.mu.Lock()
	if r.autoRunning {
		r.mu.Unlock()
		return
	}
	indexes := make([]string, 0, len(r.accounts))
	for index := range r.accounts {
		indexes = append(indexes, index)
	}
	sort.Strings(indexes)
	var selected *codexAccount
	for _, index := range indexes {
		state := r.accounts[index]
		ref := billing.CredentialFingerprint(state.file.ID)
		if !config.AutoStart[ref] || state.file.Disabled || state.file.Unavailable || state.auto.checking || state.auto.pinging {
			continue
		}
		if !state.auto.nextCheck.IsZero() && now.Before(state.auto.nextCheck) {
			continue
		}
		selected = state
		break
	}
	if selected == nil {
		r.mu.Unlock()
		return
	}
	r.autoRunning = true
	selected.auto.checking = true
	selected.auto.status = "checking"
	selected.auto.message = "Reading the current 5-hour and weekly windows."
	selected.auto.nextCheck = now.Add(codexAutoPollInterval)
	file := selected.file
	r.mu.Unlock()

	_, errFetch := a.fetchAuthQuota(callbackID, file, "codex")
	r.mu.Lock()
	if current := r.accounts[file.AuthIndex]; current == selected {
		selected.auto.checking = false
		if errFetch != nil {
			selected.auto.status = "failed"
			selected.auto.message = errFetch.Error()
			selected.auto.nextCheck = now.Add(codexAutoFailureCooldown)
		}
	}
	r.autoRunning = false
	r.mu.Unlock()
	if errFetch != nil {
		a.store.AddPluginLog(billing.PluginLogError, "Codex window check failed for credential %s: %v",
			billing.CredentialFingerprint(file.ID), errFetch)
	}
}

func (a *App) codexAutoStartViews(config billing.CodexRouting) []codexAutoStartView {
	r := &a.codexRouter
	r.mu.Lock()
	defer r.mu.Unlock()
	views := make([]codexAutoStartView, 0, len(config.AutoStart))
	for _, state := range r.accounts {
		ref := billing.CredentialFingerprint(state.file.ID)
		if !config.AutoStart[ref] {
			continue
		}
		status := state.auto.status
		if status == "" {
			status = "idle"
		}
		view := codexAutoStartView{RoutingRef: ref, Status: status, Message: state.auto.message}
		if !state.auto.checkedAt.IsZero() {
			view.CheckedAt = state.auto.checkedAt.UTC().Format(time.RFC3339)
		}
		if !state.auto.lastPing.IsZero() {
			view.LastPingAt = state.auto.lastPing.UTC().Format(time.RFC3339)
		}
		if !state.auto.nextCheck.IsZero() {
			view.NextCheck = state.auto.nextCheck.UTC().Format(time.RFC3339)
		}
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].RoutingRef < views[j].RoutingRef })
	return views
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
	return JSONResponse(http.StatusOK, map[string]any{
		"settings": config, "last_hook_at": hookAt, "last_pool": lastPool,
		"auto_start_accounts": a.codexAutoStartViews(config),
	})
}

func (a *App) putCodexRouting(req ManagementRequest) ManagementResponse {
	var config billing.CodexRouting
	if err := json.Unmarshal(req.Body, &config); err != nil {
		return JSONError(http.StatusBadRequest, "invalid", "Invalid Codex routing settings")
	}
	previous := a.store.CodexRouting()
	if err := a.store.SetCodexRouting(config); err != nil {
		return errorResponse(err)
	}
	a.resetCodexAutoStartSelections(previous, config)
	a.runCodexAutoStart(req.HostCallbackID)
	return a.codexRoutingStatus(req)
}

func (a *App) resetCodexAutoStartSelections(previous, current billing.CodexRouting) {
	r := &a.codexRouter
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, state := range r.accounts {
		ref := billing.CredentialFingerprint(state.file.ID)
		if previous.AutoStart[ref] != current.AutoStart[ref] {
			state.auto = codexAutoState{}
		}
	}
}
