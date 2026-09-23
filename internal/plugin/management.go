package plugin

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"cpa-key-billing/internal/billing"
)

const (
	managementBase = "/v0/management/plugins/" + PluginID
	resourceBase   = "/v0/resource/plugins/" + PluginID
	resourceUIPath = "/ui"
)

//go:embed ui.html
var uiHTML []byte

const (
	routeKeys                   = "/keys"
	routeCredentials            = "/credentials"
	routeProfile                = "/profile"
	routeSubscription           = "/subscription"
	routeRouting                = "/routing"
	routePrices                 = "/prices"
	routeReferencePrices        = "/prices/reference"
	routeReferencePricesStatus  = "/prices/reference/status"
	routeReferencePricesRefresh = "/prices/reference/refresh"
	routePlans                  = "/plans"
	routeRoutes                 = "/routes"
	routeKeysRoutes             = "/keys/routes"
	routeKeysBind               = "/keys/bind"
	routeKeysUnbind             = "/keys/unbind"
	routeKeysReset              = "/keys/reset"
	routeKeysLabel              = "/keys/label"
	routeKeysConcurrency        = "/keys/concurrency"
	routeKeysSync               = "/keys/sync"
	routeCredentialsSync        = "/credentials/sync"
	routeEvents                 = "/events"
	routeEventKeys              = "/events/keys"
	routeErrors                 = "/errors"
	routeAnalysis               = "/analysis"
	routePluginLogs             = "/plugin-logs"
	routeAuthFiles              = "/auth-files"
	routeAuthQuota              = "/auth-files/quota"
	routeCodexRouting           = "/codex-routing"
	routeCyberPolicy            = "/cyber-policy"
)

type managementEndpoint struct {
	method, path, description string
	handle                    func(*App, ManagementRequest) ManagementResponse
}

var managementEndpoints = []managementEndpoint{
	{http.MethodGet, routeCodexRouting, "View Codex priority routing", (*App).codexRoutingStatus},
	{http.MethodPut, routeCodexRouting, "Update Codex priority routing", (*App).putCodexRouting},
	{http.MethodGet, routeCyberPolicy, "View cyber-policy cooldowns", (*App).cyberPolicyStatus},
	{http.MethodPut, routeCyberPolicy, "Update cyber-policy cooldown settings", (*App).putCyberPolicy},
	{http.MethodDelete, routeCyberPolicy, "Clear an API key cyber-policy cooldown", (*App).clearCyberPolicyBan},
	{http.MethodGet, routeKeys, "View API key status", func(a *App, _ ManagementRequest) ManagementResponse {
		return JSONResponse(http.StatusOK, map[string]any{"keys": a.keyRows()})
	}},
	{http.MethodGet, routePlans, "View subscription plans", func(a *App, _ ManagementRequest) ManagementResponse {
		return JSONResponse(http.StatusOK, map[string]any{"plans": a.store.Plans()})
	}},
	{http.MethodGet, routeRoutes, "View routing rules", func(a *App, _ ManagementRequest) ManagementResponse {
		return JSONResponse(http.StatusOK, map[string]any{"routes": a.routeRows()})
	}},
	{http.MethodGet, routeCredentials, "View routing credential options", (*App).listCredentials},
	{http.MethodGet, routePrices, "View model pricing", func(a *App, req ManagementRequest) ManagementResponse { return a.listPrices(req, viewAccess{}) }},
	{http.MethodGet, routeReferencePrices, "Search model reference prices", (*App).searchReferencePrices},
	{http.MethodGet, routeReferencePricesStatus, "Check reference price status", func(a *App, _ ManagementRequest) ManagementResponse { return a.referencePriceStatus() }},
	{http.MethodPost, routeReferencePricesRefresh, "Refresh reference prices", func(a *App, _ ManagementRequest) ManagementResponse { return a.refreshReferencePrices() }},
	{http.MethodDelete, routePrices, "Delete custom model price", (*App).deletePrice},
	{http.MethodPut, routePrices, "Update model pricing", (*App).putPrices},
	{http.MethodPost, routePlans, "Create a subscription plan", (*App).createPlan},
	{http.MethodPatch, routePlans, "Update a subscription plan", (*App).updatePlan},
	{http.MethodDelete, routePlans, "Delete a subscription plan and unbind API keys", (*App).deletePlan},
	{http.MethodPost, routeRoutes, "Create a routing rule", (*App).createRoute},
	{http.MethodPatch, routeRoutes, "Update a routing rule", (*App).updateRoute},
	{http.MethodDelete, routeRoutes, "Delete a routing rule and remove its bindings", (*App).deleteRoute},
	{http.MethodPut, routeKeysRoutes, "Update API key routing bindings", (*App).setKeyRoutes},
	{http.MethodPost, routeKeysBind, "Bind an API key to a subscription plan", (*App).bindKey},
	{http.MethodPost, routeKeysUnbind, "Unbind an API key from a subscription plan", (*App).unbindKey},
	{http.MethodPost, routeKeysReset, "Reset selected API key quotas", (*App).resetKeys},
	{http.MethodPost, routeKeysLabel, "Set an API key note", (*App).labelKey},
	{http.MethodPost, routeKeysConcurrency, "Set an API key concurrency limit", (*App).setKeyConcurrency},
	{http.MethodPost, routeKeysSync, "Synchronize the CLIProxyAPI API key list", (*App).syncKeys},
	{http.MethodPost, routeCredentialsSync, "Synchronize configured credentials", (*App).syncConfiguredCredentials},
	{http.MethodGet, routeEventKeys, "View API keys in the event range", (*App).eventKeys},
	{http.MethodGet, routeEvents, "View paginated request events", func(a *App, req ManagementRequest) ManagementResponse {
		return a.listRequestEvents(req, viewAccess{})
	}},
	{http.MethodGet, routeErrors, "View paginated error events", func(a *App, req ManagementRequest) ManagementResponse {
		return a.listRequestErrors(req, viewAccess{})
	}},
	{http.MethodGet, routeAnalysis, "View usage distribution", func(a *App, req ManagementRequest) ManagementResponse { return a.analysis(req, viewAccess{}) }},
	{http.MethodGet, routePluginLogs, "View paginated plugin runtime logs", (*App).listPluginLogs},
	{http.MethodDelete, routePluginLogs, "Clear plugin runtime logs", func(a *App, _ ManagementRequest) ManagementResponse { return a.clearPluginLogs() }},
	{http.MethodGet, routeAuthFiles, "View auth files", func(a *App, _ ManagementRequest) ManagementResponse { return a.authFiles(viewAccess{}) }},
	{http.MethodGet, routeAuthQuota, "Query auth-file quotas", func(a *App, req ManagementRequest) ManagementResponse { return a.authQuota(req, viewAccess{}) }},
}

type resourceEndpoint struct {
	path   string
	handle func(*App, ManagementRequest, viewAccess) ManagementResponse
}

var resourceEndpoints = []resourceEndpoint{
	{routeProfile, func(a *App, _ ManagementRequest, access viewAccess) ManagementResponse {
		return a.accountProfile(access)
	}},
	{routeSubscription, func(a *App, _ ManagementRequest, access viewAccess) ManagementResponse {
		return a.accountSubscription(access)
	}},
	{routeRouting, func(a *App, _ ManagementRequest, access viewAccess) ManagementResponse {
		return a.accountRouting(access)
	}},
	{routePrices, (*App).listPrices},
	{routeAnalysis, (*App).analysis},
	{routeEvents, (*App).listRequestEvents},
	{routeErrors, (*App).listRequestErrors},
	{routeAuthFiles, func(a *App, _ ManagementRequest, access viewAccess) ManagementResponse { return a.authFiles(access) }},
	{routeAuthQuota, (*App).authQuota},
}

func managementRegistration() ManagementRegistrationResponse {
	registration := ManagementRegistrationResponse{
		Routes:    make([]ManagementRoute, 0, len(managementEndpoints)),
		Resources: make([]ResourceRoute, 1, len(resourceEndpoints)+1),
	}
	registration.Resources[0] = ResourceRoute{Path: resourceBase + resourceUIPath, Menu: MenuLabel, Description: MenuDescription}
	for _, endpoint := range managementEndpoints {
		registration.Routes = append(registration.Routes, ManagementRoute{
			Method: endpoint.method, Path: managementBase + endpoint.path, Description: endpoint.description,
		})
	}
	for _, endpoint := range resourceEndpoints {
		registration.Resources = append(registration.Resources, ResourceRoute{Path: resourceBase + endpoint.path})
	}
	return registration
}

func (a *App) handleManagement(raw []byte) ([]byte, error) {
	var req ManagementRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("parse management request: %w", errUnmarshal)
	}
	path := strings.TrimRight(req.Path, "/")
	if path == "" {
		path = req.Path
	}

	if req.Method == http.MethodGet && path == resourceBase+resourceUIPath {
		return OKEnvelope(ManagementResponse{
			StatusCode: http.StatusOK,
			Headers: http.Header{
				"Content-Type":           []string{"text/html; charset=utf-8"},
				"Cache-Control":          []string{"private, no-store"},
				"Pragma":                 []string{"no-cache"},
				"Referrer-Policy":        []string{"no-referrer"},
				"X-Content-Type-Options": []string{"nosniff"},
				"Content-Security-Policy": []string{
					"default-src 'none'; script-src 'unsafe-inline' https://cdn.jsdelivr.net; style-src 'unsafe-inline' https://cdn.jsdelivr.net; " +
						"font-src https://cdn.jsdelivr.net; connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'self'",
				},
			},
			Body: uiHTML,
		})
	}
	if path != resourceBase && strings.HasPrefix(path, resourceBase+"/") {
		return OKEnvelope(a.routeResource(req, strings.TrimPrefix(path, resourceBase)))
	}
	if path != managementBase && !strings.HasPrefix(path, managementBase+"/") {
		return OKEnvelope(JSONError(http.StatusNotFound, "not_found", "Management route does not exist: "+req.Method+" "+req.Path))
	}
	return OKEnvelope(a.routeManagement(req, strings.TrimPrefix(path, managementBase)))
}

func (a *App) routeManagement(req ManagementRequest, suffix string) ManagementResponse {
	for _, endpoint := range managementEndpoints {
		if req.Method == endpoint.method && suffix == endpoint.path {
			if req.Query.Get("view") == "1" && req.Method != http.MethodGet {
				return a.mutateWithView(req, suffix, endpoint.handle)
			}
			return endpoint.handle(a, req)
		}
	}
	return JSONError(http.StatusNotFound, "not_found", "Management route does not exist: "+req.Method+" "+req.Path)
}

func errorResponse(err error) ManagementResponse {
	switch billing.KindOf(err) {
	case billing.KindInvalid:
		return JSONError(http.StatusBadRequest, string(billing.KindInvalid), err.Error())
	case billing.KindNotFound:
		return JSONError(http.StatusNotFound, string(billing.KindNotFound), err.Error())
	case billing.KindConflict:
		return JSONError(http.StatusConflict, string(billing.KindConflict), err.Error())
	default:
		return JSONError(http.StatusInternalServerError, "internal_error", err.Error())
	}
}
