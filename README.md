<div align="center">
  <h1>CPA Key Billing</h1>
  <p><strong>API-key billing and subscription quota plugin for <a href="https://github.com/router-for-me/CLIProxyAPI">CLIProxyAPI</a>.</strong></p>
  <p>
    <a href="https://github.com/4pii4/cpa-plugin-key-billing/releases/latest"><img src="https://img.shields.io/github/v/release/4pii4/cpa-plugin-key-billing?label=release" alt="Latest release"></a>
    <a href="https://github.com/4pii4/cpa-plugin-key-billing/actions/workflows/check.yml"><img src="https://github.com/4pii4/cpa-plugin-key-billing/actions/workflows/check.yml/badge.svg" alt="CI status"></a>
    <img src="https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-blue" alt="Platforms: Windows, macOS, and Linux">
    <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT License"></a>
  </p>
</div>
<img src="images/example.svg" alt="CPA Key Billing model-pricing interface" width="100%" />

## Features

- Enforces spend, token, and request quotas with either per-key periods or synchronized subscription-plan resets
- Supports long-context **tiered pricing** based on an input-token threshold
- Sets a **maximum concurrent request count** for each API key
- Binds **routing rules** to each API key to restrict model access and upstream credentials
- Prioritizes **Codex Plus accounts before a Pro 20x reserve**, with plugin-owned account pools and bounded quota-recovery probes
- Auto-starts opted-in Codex **5-hour and weekly windows** with one tiny request after regular or surprise gifted resets
- Can pause the exact downstream **CPA billing API key** after an upstream `cyber_policy` refusal, with persistent exponential backoff and administrator clearing
- Provides a global **Mask emails** control across administrator and account views
- Retrieves reference model prices from [models.dev](https://models.dev/)
- Ships fallback prices for common CPA model aliases so fresh installations can bill them immediately

## How it works

Before a request reaches an upstream provider, the plugin checks its cyber-policy cooldown, subscription quota, concurrency limit, and routing policy. After the upstream call finishes, CLIProxyAPI supplies usage and upstream failures through `usage.handle`. The plugin records the request event, calculates its cost, updates consumption for the active quota period, and applies any enabled cooldown.

```mermaid
---
config:
  themeVariables:
    fontSize: "13px"
  flowchart:
    diagramPadding: 4
    nodeSpacing: 20
    rankSpacing: 28
    padding: 3
---
flowchart TB
    A[Downstream request] --> B["request.intercept_before<br/>model · concurrency · quota"]
    B -- Rejected --> R[Return HTTP 403 / 429]
    B -- Admitted and slot acquired --> C["scheduler.pick<br/>select a route-permitted credential"]
    C -- No eligible credential --> S[Return HTTP 503]
    C -- Selected --> D[CLIProxyAPI calls upstream model]
    D --> E["request.complete<br/>release concurrency slot"]
    D --> F["usage.handle<br/>record request event and usage"]
    F --> G["normalize tokens and bill<br/>update period consumption"]
```

The plugin integrates with CLIProxyAPI's request path through synchronous RPC methods. Admission and credential scheduling perform local state lookups and rule evaluation; the optional Codex router also reads the local host credential inventory at most once every 30 seconds. Neither performs provider HTTP requests or parses model responses. Usage recording and billing happen through `usage.handle` after the upstream call. The plugin creates no background goroutines, timers, or asynchronous refresh jobs.

### Codex Plus-first / reserve routing

On the administrator **Auth file** page, enable **Plus first, Pro reserve**. The setting is off by default and lives in this plugin's database, not CPA's configuration. Automatic classification uses CPA's `plan_type` attribute and fresh quota-query plan information: Plus is primary, Pro/Pro 20x is reserve, and unknown/other plans sit between them. Use the per-account **Routing pool** selector to explicitly protect your reserve or assign a primary account. The plugin returns a concrete account selection even when API-key routing rules are unrestricted, overriding CPA's normal selector for eligible Codex-account-only pools. Accounts within a pool share traffic using their positive CPA weights; all usable primary quota is preferred over reserve quota.

- Only Codex authentication-file accounts participate. Other providers, configured API keys, mixed-provider pools, and nested plugin model calls retain their existing behavior. Existing credential access rules always apply.
- The policy works without an open browser. `usage.handle` supplies actual failures/successes, while **Refresh quotas** enriches the server's process-local evidence with 5-hour, weekly, and model-specific windows. Browser/session-storage values are never trusted for routing. Spark and code-review limits are kept separate. Reported limits apply to reserve accounts too; a missing weekly window is not invented.
- Exhausted quota evidence is rechecked by an actual model request after its reported reset or after two minutes, whichever comes first. A generic 429 uses a 30-second cooldown. Recovery probes are limited to one per account/window every 30 seconds and expire even if completion feedback is lost. Healthy Plus accounts take precedence again after fresh successful evidence. Failed quota refreshes do not erase evidence; overlapping responses, old request reports, and changed credential revisions cannot overwrite newer state. No background polling or automatic reset-credit spending occurs.
- **Host boundary:** CPA supplies only its eligible, highest-priority candidate tier. Keep participating accounts at the same CPA priority. The plugin cannot override disabled accounts, zero weights, model incompatibility, hard cooldowns, pinned credentials, an earlier scheduler plugin, or a host execution path that does not invoke `scheduler.pick` (including Home mode on CPA 7.2.143). The page reports whether a Codex scheduler call has actually been observed; refresh the page status after traffic. Stale plugin evidence cannot hold an account indefinitely, but a CPA cooldown still can prevent its recovery probe.
- Settings and fingerprint-based role overrides survive restarts. Quota snapshots and probe leases deliberately do not. Schema 15 adds the settings table; back up the database before upgrading, and restore that backup before downgrading.

### Codex window auto-start

Each Codex authentication-file card has an **Auto-start 5h / weekly windows** switch. It is off by default and independent of Plus-first routing. For an opted-in account, the plugin reads the ordinary Codex quota windows during host activity. Inactive windows report a reset timestamp that slides forward; active windows keep a fixed timestamp. A slide of at least 30 seconds, an exhausted window becoming available, or usage unexpectedly dropping to the beginning identifies a fresh regular or globally gifted reset.

Once the fresh state is confirmed and neither ordinary window is exhausted, the plugin sends `gpt-5.6-luna` the text `hi`, asks it to reply with `OK`, selects no reasoning, and drains the complete stream. Codex starts the window only after that stream finishes. The packet consumes a small amount of upstream quota, so the switch is per account. One account cannot receive another packet for 10 minutes, and a failed check or packet waits 15 minutes before retrying. Spark and code-review windows neither trigger nor block the ordinary-window packet.

The plugin creates no timer or background goroutine. Completed client requests and Codex-routing settings updates synchronously advance one due account, so the feature works without an open browser while CLIProxyAPI is active. A completely idle proxy performs no network work; the next completed request resumes the checks. Runtime observations are discarded on restart, requiring a new baseline before a packet can be sent. The plugin keeps polling accounts whose weekly window is exhausted, which lets an unannounced global reset override the previously advertised reset date immediately on the next observation.

Quota checks and packets use CLIProxyAPI's host HTTP transport and therefore honor its global proxy. CLIProxyAPI 7.2.143 does not expose a credential-bound HTTP client to RPC plugins, so authentication files with their own `proxy_url` are reported as unsupported and are not auto-started.

Administrator API: `GET /v0/management/plugins/cpa-key-billing/codex-routing` returns `settings`, `last_hook_at`, `last_pool`, and `auto_start_accounts`. `PUT` the settings object `{ "enabled": true, "roles": { "sha256:…": "reserve" }, "auto_start": { "sha256:…": true } }` to save. Roles may be `primary` or `reserve`; omitting a role restores automatic classification, while omitting an auto-start fingerprint disables packets for that account. Codex entries in the administrator `/auth-files` response expose their `routing_ref`. API-key self-service users cannot edit this policy.

### Cyber-policy cooldown

Settings includes an optional downstream-key cooldown for the provider response code `cyber_policy` with HTTP status 400. It is disabled by default and uses a 15-minute base delay until changed. When enabled, the plugin uses the API key on the exact `usage.handle` failure record, derives the same hashed caller scope used during request admission, and pauses only that CPA billing API key. Plaintext API keys are never added to cooldown state or logs.

Each consecutive detection doubles the delay, up to 30 days, including a new refusal after the previous cooldown expires. A different observed outcome breaks the streak without shortening a cooldown already protecting the key. State is written synchronously and survives restarts, so stale browser data or a restart cannot silently bypass an active pause. Requests rejected by the cooldown receive HTTP 429, `Retry-After`, and the `cyber_policy_cooldown` code before pricing, concurrency, quota, or upstream credential selection. In-flight requests can still report their final outcomes.

Administrators can view each active key and clear its cooldown immediately in Settings. Turning the feature off admits requests immediately and clears all active cooldown state. Schema 16 adds the persistent settings table; back up the database before upgrading, and restore that backup before downgrading.

Administrator API:

- `GET /v0/management/plugins/cpa-key-billing/cyber-policy` returns `settings` and active `bans`.
- `PUT /v0/management/plugins/cpa-key-billing/cyber-policy` saves `{ "enabled": true, "base_delay_seconds": 900 }`.
- `DELETE /v0/management/plugins/cpa-key-billing/cyber-policy?scope=<hashed-scope>` clears one key.

## Requirements

- CLIProxyAPI `7.2.143` or newer; the latest release is recommended
- A plugin-enabled CLIProxyAPI build, not a `no-plugin` build

## Installation

Run the installer from the CLIProxyAPI root directory. On macOS and Linux:

```sh
curl -LsSf https://raw.githubusercontent.com/4pii4/cpa-plugin-key-billing/main/install.sh | sh
```

On Windows, stop CLIProxyAPI first, then run this command in PowerShell:

```powershell
irm https://raw.githubusercontent.com/4pii4/cpa-plugin-key-billing/main/install.ps1 | iex
```

The installer places the plugin in the current directory's `plugins/` folder. Restart CLIProxyAPI after installing or upgrading.

Alternatively, download the package for your platform from [Releases](../../releases/latest), extract it, and place the dynamic library in CLIProxyAPI's `plugins/` directory:

```text
plugins/cpa-key-billing.so       # Linux
plugins/cpa-key-billing.dylib    # macOS
plugins/cpa-key-billing.dll      # Windows
```

## Configuration

Add the following to the CLIProxyAPI configuration file:

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    cpa-key-billing:
      enabled: true
      debug: false # Log debug details such as routing and reference-price matches
      codex_fast_mode_billing: false # Bill Codex priority requests at 2.5x when enabled
      state_file: "plugins/cpa-key-billing-state-v1.db"
```

When `codex_fast_mode_billing` is enabled, Codex upstream requests containing `service_tier=priority` are billed at **2.5 times** the standard cost.

> [!WARNING]
> Back up the data file before upgrading.
>
> - Database files from v1.0.0 through the latest version are migrated automatically.
> - JSON or SQLite data files from v0.8.4 and earlier cannot be migrated. Point `state_file` to a new file instead.

After restarting CLIProxyAPI, open **API Key Billing** in the management center. Review the model prices, create a subscription plan, and bind the API keys that should be limited.

## Accessing the UI

Administrators can open **API Key Billing** from the CLIProxyAPI management center or use this URL directly:

```text
http(s)://<CLIProxyAPI-address>/v0/resource/plugins/cpa-key-billing/ui
```

Users can view their own subscription quota and usage with their API key at:

```text
http(s)://<CLIProxyAPI-address>/v0/resource/plugins/cpa-key-billing/ui#account
```

## Billing and subscription rules

- An API key without a subscription plan is metered but has no quota limit.
- A subscription plan can contain multiple custom quota windows. Each window can limit spend, tokens, requests, or any combination of the three.
- Each API key is billed independently. An independent period starts on the key's first admitted request. A synchronized period gives each window a fixed next-start time and resets all bound keys on that schedule.
- A manual reset keeps synchronized reset times unchanged. An independent period starts again on its next admitted request.
- Price precedence is custom, built-in, then models.dev reference. A request is rejected if no price is available.
- Request events are retained for 365 days.

### Built-in model prices

The plugin includes fallback prices for the CPA identifiers shown below. Rates are USD per million tokens and were verified on September 14, 2026. Custom prices override these defaults. Google advertises the `gemini-3.6-flash-high`, `gemini-3.7-flash-high`, and `gemini-3.8-flash-high` rates through December 31, 2026; recheck them before 2027.

| Model | Input | Output | Cache read | Cache write | Long context |
| --- | ---: | ---: | ---: | ---: | --- |
| `claude-opus-4-6-thinking` | $5 | $25 | $0.50 | $6.25 | — |
| `claude-sonnet-4-6` | $3 | $15 | $0.30 | $3.75 | — |
| `codex-auto-review` | $2.50 | $15 | $0.25 | input rate | >272K: $5 / $22.50 / $0.50 |
| `gemini-3-flash` | $0.50 | $3 | $0.05 | input rate | — |
| `gemini-3.1-flash-image` | $0.50 | $60 | input rate | input rate | — |
| `gemini-3.1-flash-lite` | $0.25 | $1.50 | $0.025 | input rate | — |
| `gemini-3.1-pro-low` | $2 | $12 | $0.20 | input rate | >200K: $4 / $18 / $0.40 |
| `gemini-3.6-flash-high` | $0.75 | $3.75 | $0.075 | input rate | — |
| `gemini-3.7-flash-high` | $0.75 | $3.75 | $0.075 | input rate | — |
| `gemini-3.8-flash-high` | $0.75 | $3.75 | $0.075 | input rate | — |
| `gemini-pro-agent` | $2 | $12 | $0.20 | $0.375 | — |
| `gpt-5.3-codex-spark` | $1.75 | $14 | $0.175 | input rate | — |
| `gpt-5.5` | $5 | $30 | $0.50 | input rate | >272K: $10 / $45 / $1 |
| `gpt-5.6-luna` | $0.20 | $1.20 | $0.02 | $0.25 | >272K: 2x input/cache, 1.5x output |
| `gpt-5.6-sol` | $4 | $20 | $0.40 | $5 | >272K: 2x input/cache, 1.5x output |
| `gpt-5.6-terra` | $2 | $12 | $0.20 | $2.50 | >272K: 2x input/cache, 1.5x output |
| `gpt-6-astra` | $10 | $50 | $1 | $12.50 | >272K: 2x input/cache, 1.5x output |
| `gpt-image-1.5` | $5 | $32 | $1.25 | input rate | — |
| `gpt-image-2` | $2.50 | $15 | $0.625 | input rate | — |
| `gpt-image-2.5` | $5 | $30 | $1.25 | input rate | — |
| `gpt-image-2.5-flare` | $5 | $30 | $1.25 | input rate | — |
| `gpt-image-2.5-sunburst` | $5 | $30 | $1.25 | input rate | — |
| `gpt-oss-120b-medium` | $0.15 | $0.60 | input rate | input rate | — |

Sources: [Anthropic pricing](https://platform.claude.com/docs/en/about-claude/pricing), [Gemini API pricing](https://ai.google.dev/gemini-api/docs/pricing), and [OpenAI API pricing](https://developers.openai.com/api/docs/pricing). For image models, the generic input and cache fields use text-token rates while output uses the image-token rate. `gemini-pro-agent` and `gpt-oss-120b-medium` are CPA route aliases rather than vendor API model IDs; their supplied route rates are documented in code.

## Routing rules

Bind routing rules on the API Key page, or select models, entire credential categories, and individual credentials directly. Click a model or credential checkbox to cycle between unselected, allowlisted (check mark), and denylisted (cross). A credential category also covers credentials added to that category later; individual credentials can still be excluded by the denylist. Model and credential permissions independently merge every bound rule with direct selections: allowlists are unioned, denylists are unioned, and deny always wins. An empty allowlist permits everything except denylisted entries.

```mermaid
---
config:
  themeVariables:
    fontSize: "13px"
  flowchart:
    diagramPadding: 4
    nodeSpacing: 20
    rankSpacing: 28
    padding: 3
---
flowchart TB
    A["All bound routes<br/>plus direct key selections"] --> M["Union model allowlists and denylists<br/>an empty allowlist is unrestricted, then deny"]
    A --> C["Union credential allowlists and denylists<br/>categories plus individual credentials<br/>an empty allowlist is unrestricted, then deny"]
    M --> D{Is the requested model allowed?}
    D -- No --> R[Return HTTP 403]
    D -- Yes --> P["Select an upstream from eligible CPA candidates<br/>according to credential permissions"]
    C --> P
    P -- Eligible credential --> U[Call upstream]
    P -- Unavailable --> S[Return HTTP 503]
```

## Intercepted-request responses

| Scenario | Status | `type` | `code` |
| --- | --- | --- | --- |
| API key concurrency limit reached | `429` | `rate_limit_error` | `rate_limit_exceeded` |
| Subscription quota exhausted | `429` | `rate_limit_error` | `rate_limit_exceeded` |
| Model access denied | `403` | `permission_error` | `insufficient_quota` |
| No eligible, available credential | `503` | `server_error` | `internal_server_error` |
| A bound routing rule is missing or corrupt | `503` | `server_error` | `routing_configuration_error` |
| Model has no price | `503` | `cpa_key_billing_error` | `model_price_error` |

## Acknowledgements

- [LINUX DO](https://linux.do/) — a community for developers and technology enthusiasts
