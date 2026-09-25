package billing

import (
	"strings"
	"time"
)

type UsageEvent struct {
	Scope           string
	KeyPreview      string
	AuthIndex       string
	Provider        string
	ExecutorType    string
	AuthType        string
	Account         string
	ReasoningEffort string
	ServiceTier     string
	UpstreamModel   string
	RouteModel      string
	RequestedAt     time.Time
	Latency         time.Duration
	TTFT            time.Duration
	Breakdown       TokenBreakdown
	At              time.Time
}

func (s *Store) RecordUsage(event UsageEvent) {
	s.recordUsage(event, nil)
}

func (s *Store) RecordUsageError(event UsageEvent, failure RequestError) {
	s.recordUsage(event, &failure)
}

func (s *Store) recordUsage(event UsageEvent, failure *RequestError) {
	scope := strings.TrimSpace(event.Scope)
	provider := strings.TrimSpace(event.Provider)
	authType := strings.ToLower(strings.TrimSpace(event.AuthType))
	account := ""
	switch authType {
	case "apikey":
		account = PreviewKey(event.Account)
	case "oauth":
		// The host may fall back to the downstream API key when no account is available.
		if CallerScope(event.Account) != normalizeScope(scope) {
			account = strings.TrimSpace(event.Account)
		}
	}
	at := event.At
	if at.IsZero() {
		at = s.Now()
	}
	price, billingModel, priceErr := s.ResolveModelPrice(event.UpstreamModel, event.RouteModel, false)
	if priceErr != nil {
		s.AddPluginLog(PluginLogError, "Model pricing read failed, usage event preserved and billed at zero")
	}
	if priceErr != nil || price.Source == PriceSourceNone {
		// Usage has already happened. Keep all reported tokens and failure
		// details even if its price was deleted or reference prices are unavailable.
		price = Price{Source: PriceSourceNone}
	}

	cost := ComputeCost(price, event.Breakdown)
	missingCycleTime := false
	updateResult(s, func(state *State) (struct{}, Changes) {
		if price.Source != PriceSourceNone && event.Breakdown.Billable() &&
			strings.EqualFold(provider, "codex") && authType == "oauth" &&
			isCodexFastModeTier(event.ServiceTier) {
			cost.Multiplier = CodexFastModeMultiplier
			cost.UncachedInputUSD *= CodexFastModeMultiplier
			cost.CacheReadUSD *= CodexFastModeMultiplier
			cost.CacheWriteUSD *= CodexFastModeMultiplier
			cost.OutputUSD *= CodexFastModeMultiplier
			cost.TotalUSD = cost.UncachedInputUSD + cost.CacheReadUSD + cost.CacheWriteUSD + cost.OutputUSD
			cost.AppliedInputPer1M *= CodexFastModeMultiplier
			cost.AppliedOutputPer1M *= CodexFastModeMultiplier
			cost.AppliedCacheReadPer1M *= CodexFastModeMultiplier
			cost.AppliedCacheWritePer1M *= CodexFastModeMultiplier
		}
		upstreamModel := strings.TrimSpace(event.UpstreamModel)
		if upstreamModel == "" {
			upstreamModel = strings.TrimSpace(event.RouteModel)
		}
		failed := failure != nil
		entryAt := event.RequestedAt
		if entryAt.IsZero() {
			entryAt = at
		}
		entry := RequestEvent{
			At:                entryAt,
			Scope:             scope,
			AuthIndex:         event.AuthIndex,
			Provider:          provider,
			Account:           account,
			ExecutorType:      event.ExecutorType,
			ReasoningEffort:   event.ReasoningEffort,
			ServiceTier:       event.ServiceTier,
			UpstreamModel:     upstreamModel,
			BillingModel:      billingModel,
			Failed:            failed,
			LatencyMS:         event.Latency.Milliseconds(),
			TTFTMS:            event.TTFT.Milliseconds(),
			AccountingQuality: event.Breakdown.Quality,
			PriceSource:       price.Source,
			Cost:              cost,
			ReasoningTokens:   event.Breakdown.Output.ReasoningTokens,
		}
		var changedKeys []string
		if key := state.ensureKey(scope, event.KeyPreview); key != nil {
			usage := quotaUsage{AmountUSD: cost.TotalUSD}
			if !failed {
				usage.Requests = 1
			}
			if event.Breakdown.Valid() && event.Breakdown.Quality != TokenAccountingInconsistent {
				usage.Tokens = event.Breakdown.TotalTokens
			}
			missingCycleTime = event.RequestedAt.IsZero() && len(key.Cycles) > 0 && usage != (quotaUsage{})
			key.chargeCycles(event.RequestedAt, usage)
			if _, hasPlan := state.FindPlan(key.PlanID); hasPlan {
				settleExpiredCycles(key, at)
			}
			changedKeys = []string{scope}
		}
		changes := Changes{
			Keys:               changedKeys,
			RequestEventCutoff: at.Add(-RequestEventRetention),
		}
		if failure == nil {
			changes.NormalRequestEvents = []RequestEvent{entry}
		} else {
			changes.RequestErrorEvents = []RequestErrorEvent{{Event: entry, Error: *failure}}
		}
		return struct{}{}, changes
	})
	if missingCycleTime {
		s.AddPluginLog(PluginLogError, "Request time is missing; retained the usage event and skipped quota deduction")
	}
	if price.Source == PriceSourceReference {
		s.AddPluginLog(PluginLogDebug,
			"Reference-price billing: billing_model=%q, cost=$%.8f, unit prices per 1M tokens: input=$%g, output=$%g, cache read=$%g, cache write=$%g",
			billingModel, cost.TotalUSD, cost.AppliedInputPer1M, cost.AppliedOutputPer1M,
			cost.AppliedCacheReadPer1M, cost.AppliedCacheWritePer1M)
	}
}

func isCodexFastModeTier(serviceTier string) bool {
	switch strings.ToLower(strings.TrimSpace(serviceTier)) {
	case "priority", "fast":
		return true
	default:
		return false
	}
}
