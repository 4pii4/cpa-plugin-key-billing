package plugin

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"cpa-key-billing/internal/billing"
)

const cyberPolicyFailureBody = `{"error":{"message":"This content was flagged for possible cybersecurity risk. If this seems wrong, try rephrasing your request. To get authorized for security work, join the Trusted Access for Cyber program: https://chatgpt.com/cyber","type":"invalid_request","code":"cyber_policy","status":400}}`

func TestCyberPolicyFailureBlocksOnlyTheExactBillingKey(t *testing.T) {
	app := newConfiguredApp(t)
	if err := app.store.SetCyberPolicySettings(billing.CyberPolicySettings{Enabled: true, BaseDelaySeconds: 120}); err != nil {
		t.Fatal(err)
	}
	const blockedKey = "sk-cyber-blocked-0001"
	publishUsageRecord(t, app, UsageRecord{
		Provider: "openai", ExecutorType: "OpenAICompatExecutor", Model: "gpt-5.5", Alias: "gpt-5.5",
		APIKey: blockedKey, Failed: true, Failure: UsageFailure{StatusCode: 400, Body: cyberPolicyFailureBody},
	})

	intercept := func(key string) RequestInterceptResponse {
		raw, err := app.HandleMethod(MethodRequestInterceptBefore, mustMarshal(t, RequestInterceptRequest{
			SourceFormat: "openai", Model: "gpt-5.5", RequestedModel: "gpt-5.5",
			Metadata: map[string]any{MetadataCallerScope: billing.CallerScope(key)},
		}))
		if err != nil {
			t.Fatal(err)
		}
		var response RequestInterceptResponse
		decodeResult(t, raw, &response)
		return response
	}
	blocked := intercept(blockedKey)
	if !blocked.Terminate || blocked.StatusCode != http.StatusTooManyRequests || blocked.ResponseHeaders.Get("Retry-After") == "" ||
		!strings.Contains(string(blocked.ResponseBody), `"code":"cyber_policy_cooldown"`) {
		t.Fatalf("blocked response = %+v, body=%s", blocked, blocked.ResponseBody)
	}
	if other := intercept("sk-cyber-other-0002"); other.Terminate {
		t.Fatalf("unrelated key was blocked: %s", other.ResponseBody)
	}
}

func TestCyberPolicyDetectionUsesMachineFields(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure UsageFailure
		want    bool
	}{
		{"exact payload", UsageFailure{StatusCode: 400, Body: cyberPolicyFailureBody}, true},
		{"embedded status", UsageFailure{Body: cyberPolicyFailureBody}, true},
		{"wrapped payload", UsageFailure{StatusCode: 502, Body: "provider: " + cyberPolicyFailureBody}, true},
		{"message only", UsageFailure{StatusCode: 400, Body: `{"error":{"message":"cyber_policy"}}`}, false},
		{"wrong status", UsageFailure{StatusCode: 429, Body: `{"error":{"code":"cyber_policy","status":429}}`}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isCyberPolicyFailure(test.failure); got != test.want {
				t.Fatalf("isCyberPolicyFailure() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCyberPolicyManagementSettingsAndManualClear(t *testing.T) {
	app := newConfiguredApp(t)
	var status billing.CyberPolicyStatus
	callOK(t, app, http.MethodPut, routeCyberPolicy, nil,
		billing.CyberPolicySettings{Enabled: true, BaseDelaySeconds: 30}, http.StatusOK, &status)
	if !status.Settings.Enabled || status.Settings.BaseDelaySeconds != 30 {
		t.Fatal(status)
	}
	const apiKey = "sk-cyber-admin-0001"
	publishUsageRecord(t, app, UsageRecord{APIKey: apiKey, Failed: true, Failure: UsageFailure{StatusCode: 400, Body: cyberPolicyFailureBody}})
	callOK(t, app, http.MethodGet, routeCyberPolicy, nil, nil, http.StatusOK, &status)
	if len(status.Bans) != 1 || status.Bans[0].Scope != billing.CallerScope(apiKey) {
		t.Fatal(status)
	}
	var cleared struct {
		Cleared bool                      `json:"cleared"`
		Status  billing.CyberPolicyStatus `json:"status"`
	}
	callOK(t, app, http.MethodDelete, routeCyberPolicy,
		url.Values{"scope": {billing.CallerScope(apiKey)}}, nil, http.StatusOK, &cleared)
	if !cleared.Cleared || len(cleared.Status.Bans) != 0 {
		t.Fatal(cleared)
	}

	response := callManagement(t, app, http.MethodPut, routeCyberPolicy, nil, json.RawMessage(`{"enabled":true,"base_delay_seconds":0}`))
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid delay status = %d", response.StatusCode)
	}
}
