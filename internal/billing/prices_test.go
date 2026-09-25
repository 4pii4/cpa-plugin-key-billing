package billing

import (
	"fmt"
	"testing"
)

func TestResolveCustomPrice(t *testing.T) {
	state := NewState()
	state.Prices = map[string]CustomPrice{
		"gpt-5.5":      {ModelID: "gpt-5.5", PriceRates: PriceRates{InputPer1M: 1, OutputPer1M: 2}},
		"team/gpt-5.5": {ModelID: "team/gpt-5.5", PriceRates: PriceRates{InputPer1M: 5}},
	}
	price := state.ResolveCustomPrice("team/gpt-5.5")
	if price.InputPer1M != 5 || price.CacheReadPer1M != 5 || price.CacheWritePer1M != 5 {
		t.Fatalf("billing model price or cache fallback is wrong: %+v", price)
	}
	price = state.ResolveCustomPrice("gpt-5.5")
	if price.InputPer1M != 1 {
		t.Fatalf("model price is wrong: %+v", price)
	}
	price = state.ResolveCustomPrice("unknown")
	if price.Source != PriceSourceNone {
		t.Fatalf("unknown model has a custom price: %+v", price)
	}
}

func TestResolveBuiltinPrice(t *testing.T) {
	tests := []struct {
		model                                string
		input, output, cacheRead, cacheWrite float64
		long                                 *ResolvedLongContextPrice
	}{
		{"claude-opus-4-6-thinking", 5, 25, 0.5, 6.25, nil},
		{"claude-sonnet-4-6", 3, 15, 0.3, 3.75, nil},
		{"codex-auto-review", 2.5, 15, 0.25, 2.5, &ResolvedLongContextPrice{272000, 5, 22.5, 0.5, 5}},
		{"gemini-3-flash", 0.5, 3, 0.05, 0.5, nil},
		{"gemini-3.1-flash-image", 0.5, 60, 0.5, 0.5, nil},
		{"gemini-3.1-flash-lite", 0.25, 1.5, 0.025, 0.25, nil},
		{"gemini-3.1-pro-low", 2, 12, 0.2, 2, &ResolvedLongContextPrice{200000, 4, 18, 0.4, 4}},
		{"gemini-3.6-flash-high", 0.75, 3.75, 0.075, 0.75, nil},
		{"gemini-3.7-flash-high", 0.75, 3.75, 0.075, 0.75, nil},
		{"gemini-3.8-flash-high", 0.75, 3.75, 0.075, 0.75, nil},
		{"gemini-pro-agent", 2, 12, 0.2, 0.375, nil},
		{"gpt-5.3-codex-spark", 1.75, 14, 0.175, 1.75, nil},
		{"gpt-5.5", 5, 30, 0.5, 5, &ResolvedLongContextPrice{272000, 10, 45, 1, 10}},
		{"gpt-5.6-luna", 0.2, 1.2, 0.02, 0.25, &ResolvedLongContextPrice{272000, 0.4, 1.8, 0.04, 0.5}},
		{"gpt-5.6-sol", 4, 20, 0.4, 5, &ResolvedLongContextPrice{272000, 8, 30, 0.8, 10}},
		{"gpt-5.6-terra", 2, 12, 0.2, 2.5, &ResolvedLongContextPrice{272000, 4, 18, 0.4, 5}},
		{"gpt-6-astra", 10, 50, 1, 12.5, &ResolvedLongContextPrice{272000, 20, 75, 2, 25}},
		{"gpt-6-sol", 2, 10, 0.2, 2.5, &ResolvedLongContextPrice{272000, 4, 15, 0.4, 5}},
		{"gpt-6-luna", 0.1, 0.5, 0.01, 0.125, &ResolvedLongContextPrice{272000, 0.2, 0.75, 0.02, 0.25}},
		{"gpt-image-1.5", 5, 32, 1.25, 5, nil},
		{"gpt-image-2", 5, 30, 1.25, 5, nil},
		{"gpt-image-2.5", 5, 30, 1.25, 5, nil},
		{"gpt-image-2.5-flare", 5, 30, 1.25, 5, nil},
		{"gpt-image-2.5-sunburst", 5, 30, 1.25, 5, nil},
		{"gpt-oss-120b-medium", 0.15, 0.6, 0.15, 0.15, nil},
	}
	if len(builtinPrices) != len(tests) {
		t.Fatalf("builtin price count = %d, want %d", len(builtinPrices), len(tests))
	}
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			price := ResolveBuiltinPrice(test.model)
			if price.Source != PriceSourceBuiltin || price.InputPer1M != test.input ||
				price.OutputPer1M != test.output || price.CacheReadPer1M != test.cacheRead ||
				price.CacheWritePer1M != test.cacheWrite {
				t.Fatalf("builtin price = %+v", price)
			}
			if test.long == nil {
				if price.LongContext != nil {
					t.Fatalf("unexpected long-context price = %+v", price.LongContext)
				}
				return
			}
			if price.LongContext == nil || *price.LongContext != *test.long {
				t.Fatalf("long-context price = %+v, want %+v", price.LongContext, test.long)
			}
		})
	}
	if price := ResolveBuiltinPrice("unknown"); price.Source != PriceSourceNone {
		t.Fatalf("unknown model has a builtin price: %+v", price)
	}
}

func TestCustomPriceOverridesBuiltinPrice(t *testing.T) {
	store := NewStore(nil, nil)
	if _, err := store.UpsertPrice(CustomPrice{
		ModelID:    "gpt-image-1.5",
		PriceRates: PriceRates{InputPer1M: 7, OutputPer1M: 8},
	}); err != nil {
		t.Fatal(err)
	}
	price, model, err := store.ResolveModelPrice("gpt-image-1.5", "", false)
	if err != nil || model != "gpt-image-1.5" || price.Source != PriceSourceCustom || price.InputPer1M != 7 || price.OutputPer1M != 8 {
		t.Fatalf("custom price did not override builtin: price=%+v model=%q err=%v", price, model, err)
	}
}

func TestResolveBillingModel(t *testing.T) {
	state := NewState()
	state.Prices = map[string]CustomPrice{
		"grok-4.5":              {ModelID: "grok-4.5"},
		"claude/deepseek-flash": {ModelID: "claude/deepseek-flash"},
		"configured(low)":       {ModelID: "configured(low)"},
	}
	tests := []struct {
		name     string
		upstream string
		route    string
		want     string
	}{
		{name: "model", upstream: "grok-4.5", route: "grok-4.5", want: "grok-4.5"},
		{name: "thinking", upstream: "grok-4.5", route: "grok-4.5(high)", want: "grok-4.5"},
		{name: "route", upstream: "deepseek-v4-flash", route: "claude/deepseek-flash", want: "claude/deepseek-flash"},
		{name: "route thinking", upstream: "deepseek-v4-flash", route: "claude/deepseek-flash(high)", want: "claude/deepseek-flash"},
		{name: "configured suffix", upstream: "upstream-low", route: "configured(low)", want: "configured(low)"},
		{name: "request suffix", upstream: "upstream-high", route: "configured(high)", want: "configured"},
		{name: "auto", upstream: "gpt-5.5", route: "auto(high)", want: "gpt-5.5"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := state.ResolveBillingModel(test.upstream, test.route); got != test.want {
				t.Fatalf("ResolveBillingModel(%q, %q) = %q, want %q", test.upstream, test.route, got, test.want)
			}
		})
	}
}

func TestModelWithoutThinkingSuffixPreservesModelIdentity(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{" codex/gpt-5.6-sol(xhigh) ", "codex/gpt-5.6-sol"},
		{"claude/model(32768)", "claude/model"},
		{"provider/model(custom)(high)", "provider/model(custom)"},
		{"model(high", "model(high"},
		{"model(high)-vision-exp", "model(high)-vision-exp"},
		{" MODEL ", "MODEL"},
		{"", ""},
	} {
		if got := ModelWithoutThinkingSuffix(test.input); got != test.want {
			t.Fatalf("ModelWithoutThinkingSuffix(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func BenchmarkCustomPriceLookup(b *testing.B) {
	for _, count := range []int{10, 1000, 100000} {
		b.Run(fmt.Sprintf("models=%d", count), func(b *testing.B) {
			store := NewStore(nil, nil)
			for i := range count {
				model := fmt.Sprintf("provider/model-%d", i)
				store.state.Prices[model] = CustomPrice{ModelID: model, PriceRates: PriceRates{InputPer1M: 1}}
			}
			requested := fmt.Sprintf("provider/model-%d(high)", count-1)
			b.ReportAllocs()
			for b.Loop() {
				price, _, err := store.ResolveModelPrice("upstream", requested, false)
				if err != nil || price.Source != PriceSourceCustom {
					b.Fatalf("lookup failed: %+v, %v", price, err)
				}
			}
		})
	}
}
