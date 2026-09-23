package billing

import (
	"sort"
	"strings"
	"time"
)

const (
	defaultCyberPolicyDelaySeconds = int64((15 * time.Minute) / time.Second)
	maxCyberPolicyDelaySeconds     = int64((30 * 24 * time.Hour) / time.Second)
)

// CyberPolicySettings controls the plugin-owned cooldown for downstream CPA
// billing keys whose provider response carries the cyber_policy error code.
type CyberPolicySettings struct {
	Enabled          bool  `json:"enabled"`
	BaseDelaySeconds int64 `json:"base_delay_seconds"`
}

// CyberPolicyBan stores only the hashed caller scope. Key previews and labels
// are resolved from KeyState when an operator reads the status page.
type CyberPolicyBan struct {
	Consecutive    int       `json:"consecutive"`
	BlockedUntil   time.Time `json:"blocked_until"`
	LastDetectedAt time.Time `json:"last_detected_at"`
	LastWasCyber   bool      `json:"last_was_cyber"`
}

type CyberPolicy struct {
	Settings CyberPolicySettings       `json:"settings"`
	Bans     map[string]CyberPolicyBan `json:"bans"`
}

type CyberPolicyDecision struct {
	Blocked      bool
	BlockedUntil time.Time
	Consecutive  int
}

type CyberPolicyObservation struct {
	Applied      bool
	Preview      string
	Consecutive  int
	BlockedUntil time.Time
}

type CyberPolicyBanView struct {
	Scope            string    `json:"scope"`
	Preview          string    `json:"preview,omitempty"`
	Label            string    `json:"label,omitempty"`
	Consecutive      int       `json:"consecutive"`
	BlockedUntil     time.Time `json:"blocked_until"`
	LastDetectedAt   time.Time `json:"last_detected_at"`
	RemainingSeconds int64     `json:"remaining_seconds"`
}

type CyberPolicyStatus struct {
	Settings CyberPolicySettings  `json:"settings"`
	Bans     []CyberPolicyBanView `json:"bans"`
}

func newCyberPolicy() CyberPolicy {
	return CyberPolicy{
		Settings: CyberPolicySettings{BaseDelaySeconds: defaultCyberPolicyDelaySeconds},
		Bans:     make(map[string]CyberPolicyBan),
	}
}

func normalizedCyberPolicySettings(settings CyberPolicySettings) CyberPolicySettings {
	if settings.BaseDelaySeconds == 0 {
		settings.BaseDelaySeconds = defaultCyberPolicyDelaySeconds
	}
	return settings
}

func cloneCyberPolicy(policy CyberPolicy) CyberPolicy {
	policy.Settings = normalizedCyberPolicySettings(policy.Settings)
	policy.Bans = cloneCyberPolicyBans(policy.Bans)
	return policy
}

func cloneCyberPolicyBans(bans map[string]CyberPolicyBan) map[string]CyberPolicyBan {
	cloned := make(map[string]CyberPolicyBan, len(bans))
	for scope, ban := range bans {
		cloned[scope] = ban
	}
	return cloned
}

func validateCyberPolicySettings(settings CyberPolicySettings) error {
	if settings.BaseDelaySeconds < 1 || settings.BaseDelaySeconds > maxCyberPolicyDelaySeconds {
		return invalidf("Cyber-policy base delay must be between 1 second and 30 days")
	}
	return nil
}

func (s *Store) CyberPolicySettings() CyberPolicySettings {
	var settings CyberPolicySettings
	s.read(func(state *State) { settings = normalizedCyberPolicySettings(state.CyberPolicy.Settings) })
	return settings
}

func (s *Store) SetCyberPolicySettings(settings CyberPolicySettings) error {
	if err := validateCyberPolicySettings(settings); err != nil {
		return err
	}
	_, err := editConfiguration(s, func(state *State) (struct{}, Changes, error) {
		wasEnabled := normalizedCyberPolicySettings(state.CyberPolicy.Settings).Enabled
		state.CyberPolicy.Settings = settings
		if wasEnabled && !settings.Enabled {
			state.CyberPolicy.Bans = make(map[string]CyberPolicyBan)
		} else if state.CyberPolicy.Bans == nil {
			state.CyberPolicy.Bans = make(map[string]CyberPolicyBan)
		}
		return struct{}{}, Changes{CyberPolicy: true}, nil
	})
	return err
}

// CyberPolicyDecision is read during request admission. Expired records are
// ignored synchronously and cleaned up by the next outcome or manual clear.
func (s *Store) CyberPolicyDecision(scope string, now time.Time) CyberPolicyDecision {
	scope = normalizeScope(scope)
	if scope == "" {
		return CyberPolicyDecision{}
	}
	var decision CyberPolicyDecision
	s.read(func(state *State) {
		settings := normalizedCyberPolicySettings(state.CyberPolicy.Settings)
		if !settings.Enabled {
			return
		}
		ban, ok := state.CyberPolicy.Bans[scope]
		if !ok || !ban.BlockedUntil.After(now) {
			return
		}
		decision = CyberPolicyDecision{Blocked: true, BlockedUntil: ban.BlockedUntil, Consecutive: ban.Consecutive}
	})
	return decision
}

// ObserveCyberPolicyOutcome advances a streak only for consecutive detected
// cyber-policy outcomes on the same exact caller scope, including the first
// retry after a cooldown expires. Any intervening result breaks the streak
// without shortening an already-active cooldown.
func (s *Store) ObserveCyberPolicyOutcome(scope string, cyber bool, at time.Time) CyberPolicyObservation {
	scope = normalizeScope(scope)
	if scope == "" {
		return CyberPolicyObservation{}
	}
	return updateResult(s, func(state *State) (CyberPolicyObservation, Changes) {
		settings := normalizedCyberPolicySettings(state.CyberPolicy.Settings)
		if !settings.Enabled {
			return CyberPolicyObservation{}, Changes{}
		}
		if state.CyberPolicy.Bans == nil {
			state.CyberPolicy.Bans = make(map[string]CyberPolicyBan)
		}
		ban, exists := state.CyberPolicy.Bans[scope]
		if !cyber {
			if !exists {
				return CyberPolicyObservation{}, Changes{}
			}
			if !ban.BlockedUntil.After(at) {
				delete(state.CyberPolicy.Bans, scope)
				return CyberPolicyObservation{}, Changes{CyberPolicy: true}
			}
			if !ban.LastWasCyber {
				return CyberPolicyObservation{}, Changes{}
			}
			ban.LastWasCyber = false
			state.CyberPolicy.Bans[scope] = ban
			return CyberPolicyObservation{}, Changes{CyberPolicy: true}
		}

		if !exists || !ban.LastWasCyber {
			ban.Consecutive = 1
		} else if ban.Consecutive < 63 {
			ban.Consecutive++
		}
		delay := cyberPolicyDelay(settings.BaseDelaySeconds, ban.Consecutive)
		blockedUntil := at.Add(delay)
		if ban.BlockedUntil.After(blockedUntil) {
			blockedUntil = ban.BlockedUntil
		}
		ban.BlockedUntil = blockedUntil.UTC()
		ban.LastDetectedAt = at.UTC()
		ban.LastWasCyber = true
		state.CyberPolicy.Bans[scope] = ban
		preview := UnknownKeyPreview
		if key := state.Keys[scope]; key != nil && strings.TrimSpace(key.Preview) != "" {
			preview = key.Preview
		}
		return CyberPolicyObservation{
			Applied: true, Preview: preview, Consecutive: ban.Consecutive, BlockedUntil: ban.BlockedUntil,
		}, Changes{CyberPolicy: true}
	})
}

func cyberPolicyDelay(baseSeconds int64, consecutive int) time.Duration {
	seconds := baseSeconds
	for step := 1; step < consecutive && seconds < maxCyberPolicyDelaySeconds; step++ {
		if seconds > maxCyberPolicyDelaySeconds/2 {
			seconds = maxCyberPolicyDelaySeconds
			break
		}
		seconds *= 2
	}
	if seconds > maxCyberPolicyDelaySeconds {
		seconds = maxCyberPolicyDelaySeconds
	}
	return time.Duration(seconds) * time.Second
}

func (s *Store) CyberPolicyStatus(now time.Time) CyberPolicyStatus {
	status := CyberPolicyStatus{Bans: []CyberPolicyBanView{}}
	s.read(func(state *State) {
		status.Settings = normalizedCyberPolicySettings(state.CyberPolicy.Settings)
		for scope, ban := range state.CyberPolicy.Bans {
			remaining := ban.BlockedUntil.Sub(now)
			if remaining <= 0 {
				continue
			}
			view := CyberPolicyBanView{
				Scope: scope, Consecutive: ban.Consecutive, BlockedUntil: ban.BlockedUntil,
				LastDetectedAt: ban.LastDetectedAt, RemainingSeconds: int64((remaining + time.Second - 1) / time.Second),
			}
			if key := state.Keys[scope]; key != nil {
				view.Preview, view.Label = key.Preview, key.Label
			}
			status.Bans = append(status.Bans, view)
		}
	})
	sort.Slice(status.Bans, func(i, j int) bool {
		if !status.Bans[i].BlockedUntil.Equal(status.Bans[j].BlockedUntil) {
			return status.Bans[i].BlockedUntil.After(status.Bans[j].BlockedUntil)
		}
		return status.Bans[i].Scope < status.Bans[j].Scope
	})
	return status
}

func (s *Store) ClearCyberPolicyBan(scope string) (bool, error) {
	scope = normalizeScope(scope)
	if scope == "" {
		return false, invalidf("API key scope cannot be empty")
	}
	return editConfiguration(s, func(state *State) (bool, Changes, error) {
		if _, exists := state.CyberPolicy.Bans[scope]; !exists {
			return false, Changes{}, nil
		}
		delete(state.CyberPolicy.Bans, scope)
		return true, Changes{CyberPolicy: true}, nil
	})
}
