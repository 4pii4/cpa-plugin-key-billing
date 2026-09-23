package plugin

import (
	"net/http"
	"strings"

	"cpa-key-billing/internal/billing"
)

func (a *App) cyberPolicyStatus(_ ManagementRequest) ManagementResponse {
	return JSONResponse(http.StatusOK, a.store.CyberPolicyStatus(a.store.Now()))
}

func (a *App) putCyberPolicy(req ManagementRequest) ManagementResponse {
	var settings billing.CyberPolicySettings
	if err := decodeStrict(req.Body, &settings); err != nil {
		return errorResponse(err)
	}
	if err := a.store.SetCyberPolicySettings(settings); err != nil {
		return errorResponse(err)
	}
	return a.cyberPolicyStatus(req)
}

func (a *App) clearCyberPolicyBan(req ManagementRequest) ManagementResponse {
	scope := strings.TrimSpace(req.Query.Get("scope"))
	cleared, err := a.store.ClearCyberPolicyBan(scope)
	if err != nil {
		return errorResponse(err)
	}
	if cleared {
		preview := scope
		if len(preview) > 12 {
			preview = "…" + preview[len(preview)-12:]
		}
		a.store.AddPluginLog(billing.PluginLogInfo, "Cyber-policy cooldown cleared for API key scope %s", preview)
	}
	return JSONResponse(http.StatusOK, map[string]any{
		"cleared": cleared,
		"status":  a.store.CyberPolicyStatus(a.store.Now()),
	})
}
