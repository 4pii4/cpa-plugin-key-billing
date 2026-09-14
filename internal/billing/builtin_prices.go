package billing

import "math"

// Builtin prices are shipped with the plugin and are intentionally not persisted.
// Rates are USD per 1,000,000 tokens. Custom prices always take precedence.
//
// Vendor pricing was verified on 2026-09-14 against the official Anthropic,
// Google, and OpenAI pricing pages. CPA-only aliases use the corresponding
// vendor model's rates unless a note below says otherwise.
var builtinPrices = map[string]CustomPrice{
	NormalizeModelID("claude-opus-4-6-thinking"): {
		ModelID: "claude-opus-4-6-thinking",
		PriceRates: PriceRates{
			InputPer1M:      5,
			OutputPer1M:     25,
			CacheReadPer1M:  float64Ptr(0.5),
			CacheWritePer1M: float64Ptr(6.25),
		},
	},
	NormalizeModelID("claude-sonnet-4-6"): {
		ModelID: "claude-sonnet-4-6",
		PriceRates: PriceRates{
			InputPer1M:      3,
			OutputPer1M:     15,
			CacheReadPer1M:  float64Ptr(0.3),
			CacheWritePer1M: float64Ptr(3.75),
		},
	},
	NormalizeModelID("codex-auto-review"): {
		ModelID: "codex-auto-review",
		// Matches OpenAI GPT-5.4 standard pricing.
		PriceRates: PriceRates{
			InputPer1M:     2.5,
			OutputPer1M:    15,
			CacheReadPer1M: float64Ptr(0.25),
			LongContext: &LongContextPrice{
				ThresholdInputTokens: 272000,
				InputPer1M:           5,
				OutputPer1M:          22.5,
				CacheReadPer1M:       float64Ptr(0.5),
			},
		},
	},
	NormalizeModelID("gemini-3-flash"): {
		ModelID: "gemini-3-flash",
		// CPA alias for Google's gemini-3-flash-preview standard tier.
		PriceRates: PriceRates{
			InputPer1M:     0.5,
			OutputPer1M:    3,
			CacheReadPer1M: float64Ptr(0.05),
		},
	},
	NormalizeModelID("gemini-3.1-flash-image"): {
		ModelID:    "gemini-3.1-flash-image",
		PriceRates: PriceRates{InputPer1M: 0.5, OutputPer1M: 60},
	},
	NormalizeModelID("gemini-3.1-flash-lite"): {
		ModelID: "gemini-3.1-flash-lite",
		PriceRates: PriceRates{
			InputPer1M:     0.25,
			OutputPer1M:    1.5,
			CacheReadPer1M: float64Ptr(0.025),
		},
	},
	NormalizeModelID("gemini-3.1-pro-low"): {
		ModelID: "gemini-3.1-pro-low",
		// CPA reasoning-effort alias for Gemini 3.1 Pro Preview.
		PriceRates: PriceRates{
			InputPer1M:     2,
			OutputPer1M:    12,
			CacheReadPer1M: float64Ptr(0.2),
			LongContext: &LongContextPrice{
				ThresholdInputTokens: 200000,
				InputPer1M:           4,
				OutputPer1M:          18,
				CacheReadPer1M:       float64Ptr(0.4),
			},
		},
	},
	// Google advertises these Flash rates through 2026-12-31. Reverify them
	// before 2027-01-01, when the published standard rates are due to double.
	NormalizeModelID("gemini-3.6-flash-high"): {
		ModelID: "gemini-3.6-flash-high",
		PriceRates: PriceRates{
			InputPer1M:     0.75,
			OutputPer1M:    3.75,
			CacheReadPer1M: float64Ptr(0.075),
		},
	},
	NormalizeModelID("gemini-3.7-flash-high"): {
		ModelID: "gemini-3.7-flash-high",
		PriceRates: PriceRates{
			InputPer1M:     0.75,
			OutputPer1M:    3.75,
			CacheReadPer1M: float64Ptr(0.075),
		},
	},
	NormalizeModelID("gemini-3.8-flash-high"): {
		ModelID: "gemini-3.8-flash-high",
		PriceRates: PriceRates{
			InputPer1M:     0.75,
			OutputPer1M:    3.75,
			CacheReadPer1M: float64Ptr(0.075),
		},
	},
	NormalizeModelID("gemini-pro-agent"): {
		ModelID: "gemini-pro-agent",
		// CPA route alias. Cache-write pricing follows the rate supplied with
		// the requested inventory; Google publishes storage rather than a
		// directly equivalent per-token cache-write rate.
		PriceRates: PriceRates{
			InputPer1M:      2,
			OutputPer1M:     12,
			CacheReadPer1M:  float64Ptr(0.2),
			CacheWritePer1M: float64Ptr(0.375),
		},
	},
	NormalizeModelID("gpt-5.3-codex-spark"): {
		ModelID: "gpt-5.3-codex-spark",
		// The Spark research-preview alias uses GPT-5.3-Codex token rates.
		PriceRates: PriceRates{
			InputPer1M:     1.75,
			OutputPer1M:    14,
			CacheReadPer1M: float64Ptr(0.175),
		},
	},
	NormalizeModelID("gpt-5.5"): {
		ModelID: "gpt-5.5",
		PriceRates: PriceRates{
			InputPer1M:     5,
			OutputPer1M:    30,
			CacheReadPer1M: float64Ptr(0.5),
			LongContext: &LongContextPrice{
				ThresholdInputTokens: 272000,
				InputPer1M:           10,
				OutputPer1M:          45,
				CacheReadPer1M:       float64Ptr(1),
			},
		},
	},
	NormalizeModelID("gpt-5.6-luna"): {
		ModelID:    "gpt-5.6-luna",
		PriceRates: openAILongContextRates(0.2, 1.2, 0.02, 0.25),
	},
	NormalizeModelID("gpt-5.6-sol"): {
		ModelID:    "gpt-5.6-sol",
		PriceRates: openAILongContextRates(4, 20, 0.4, 5),
	},
	NormalizeModelID("gpt-5.6-terra"): {
		ModelID:    "gpt-5.6-terra",
		PriceRates: openAILongContextRates(2, 12, 0.2, 2.5),
	},
	NormalizeModelID("gpt-6-astra"): {
		ModelID:    "gpt-6-astra",
		PriceRates: openAILongContextRates(10, 50, 1, 12.5),
	},
	NormalizeModelID("gpt-image-1.5"): {
		ModelID: "gpt-image-1.5",
		PriceRates: PriceRates{
			InputPer1M:     5,
			OutputPer1M:    32,
			CacheReadPer1M: float64Ptr(1.25),
		},
	},
	NormalizeModelID("gpt-image-2"): {
		ModelID: "gpt-image-2",
		PriceRates: PriceRates{
			InputPer1M:     2.5,
			OutputPer1M:    15,
			CacheReadPer1M: float64Ptr(0.625),
		},
	},
	NormalizeModelID("gpt-image-2.5"): {
		ModelID: "gpt-image-2.5",
		// CPA family alias; both current GPT-Image-2.5 variants use these rates.
		PriceRates: openAIImageRates(30),
	},
	NormalizeModelID("gpt-image-2.5-flare"): {
		ModelID:    "gpt-image-2.5-flare",
		PriceRates: openAIImageRates(30),
	},
	NormalizeModelID("gpt-image-2.5-sunburst"): {
		ModelID:    "gpt-image-2.5-sunburst",
		PriceRates: openAIImageRates(30),
	},
	NormalizeModelID("gpt-oss-120b-medium"): {
		ModelID: "gpt-oss-120b-medium",
		// CPA hosted-route rate supplied with the requested model inventory.
		// OpenAI distributes gpt-oss-120b as open weights and has no API tariff.
		PriceRates: PriceRates{InputPer1M: 0.15, OutputPer1M: 0.6},
	},
}

func openAILongContextRates(input, output, cacheRead, cacheWrite float64) PriceRates {
	return PriceRates{
		InputPer1M:      input,
		OutputPer1M:     output,
		CacheReadPer1M:  float64Ptr(cacheRead),
		CacheWritePer1M: float64Ptr(cacheWrite),
		LongContext: &LongContextPrice{
			ThresholdInputTokens: 272000,
			InputPer1M:           input * 2,
			OutputPer1M:          roundPriceRate(output * 1.5),
			CacheReadPer1M:       float64Ptr(cacheRead * 2),
			CacheWritePer1M:      float64Ptr(cacheWrite * 2),
		},
	}
}

func roundPriceRate(value float64) float64 {
	return math.Round(value*1e12) / 1e12
}

func openAIImageRates(output float64) PriceRates {
	return PriceRates{
		InputPer1M:     5,
		OutputPer1M:    output,
		CacheReadPer1M: float64Ptr(1.25),
	}
}

func float64Ptr(value float64) *float64 {
	return &value
}

func resolveBuiltinRates(modelID string) (PriceRates, bool) {
	price, found := builtinPrices[NormalizeModelID(modelID)]
	return price.PriceRates, found
}

func ResolveBuiltinPrice(modelID string) Price {
	if rates, found := resolveBuiltinRates(modelID); found {
		return rates.resolve(PriceSourceBuiltin)
	}
	return Price{Source: PriceSourceNone}
}
