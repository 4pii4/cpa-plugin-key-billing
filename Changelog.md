# Changelog

## v1.3.14

### Upgrade notes

- Back up the database before upgrading. Schema 16 adds persistent cyber-policy cooldown settings and per-key state; restore the backup before downgrading.
- Cyber-policy protection is off by default. Configure its base delay in Settings; disabling it clears active cooldowns.

### Backend

- Added an optional cooldown for the exact downstream CPA billing API key that receives an upstream HTTP 400 `cyber_policy` error.
- Persist cooldowns by hashed caller scope, double the delay for consecutive detections up to 30 days across cooldown expiry, reset only after a different outcome, and block new requests before they occupy an upstream credential.
- Added administrator endpoints to inspect settings and active cooldowns, change the base delay, and clear an individual key immediately.

### Frontend

- Added cyber-policy protection settings, active cooldown details, and per-key clear actions to Settings.
- Moved **Mask emails** to the persistent page header and apply it throughout administrator and account views, including auth cards, routing labels, analysis, events, errors, logs, tooltips, and CSV exports.

## v1.3.13

### Upgrade notes

- Back up the database before upgrading. Schema 15 adds persistent Codex routing settings; restore the backup before downgrading.
- Enable **Plus first, Pro reserve** on Auth file. Participating Codex accounts must share the same CPA priority; host cooldowns, pinned accounts, model eligibility, and scheduler-hook availability still apply.

### Backend

- Added plugin-owned Codex account selection: eligible Plus accounts first, other plans next, Pro 20x reserve last, with per-account pool overrides.
- Added separate ordinary, Spark, and code-review quota tracking, live failure feedback, bounded recovery probes, and protection against overlapping refreshes, old usage reports, credential replacement, and clock changes.
- Keep quota snapshots process-local and optional: routing works without an open browser, and stale exhaustion cannot permanently strand an eligible account.

### Frontend

- Added an administrator routing toggle, per-account primary/reserve selectors, and scheduler-hook status to Auth file.
- Moved Analysis to the first tab and made it the default landing page in administrator and account views, while preserving saved routes and deep links.
- Shortened navigation, headings, and controls, including Auth file, Requests, Errors, and Mask emails.

## v1.3.12

### Frontend

- Added an administrator action to consume an available Codex reset credit and immediately refresh the authentication file's quota.
- Added a persistent email-obfuscation toggle to administrator and account authentication-file views while preserving email search.
- Masked authentication-file hover text and kept the expanded filter controls usable on narrow screens.
- Normalized separator spacing in display labels and fixed route-tooltip sizing in Safari.

## v1.3.11

### Backend

- Added built-in fallback prices for 23 CPA model aliases, including cache and long-context tiers, while preserving custom-price precedence.
- Updated the billing integration harness for portable release-archive extraction.

### Frontend

- Translated the management UI, runtime messages, documentation, installers, and supporting scripts to English.
- Replaced the legacy Chinese interface screenshot with an English model-pricing overview.

## v1.3.10

### Upgrade notes

- Existing subscription plans retain their independent billing periods. Do not downgrade directly after enabling synchronized periods.

### Backend

- Subscription plans support synchronized periods. Each quota window can specify its own next start time, all bound keys reset at the same time, and quota usage is calculated independently for each key.
- Manually resetting a quota does not change the synchronized period's reset time.

### Frontend

- New and edited subscription plans can select a period mode, prefill the next start time from the reset interval, and report invalid input.
- Improved the time picker and narrow-screen layout, and show the next reset time on subscription-plan cards.

## v1.3.9

### Upgrade notes

- Existing routing rules remain compatible. Older versions cannot enforce denylist restrictions, so do not downgrade directly after using a denylist.

### Backend

- Routing rules support denylists for models, complete credential categories, and individual credentials. Denylists take precedence over allowlists.

### Frontend

- Models and credentials support three-state selection: unselected, allowlisted, and denylisted. Actions are available to allow all, deny all, and clear all selections.
- Standardized routing editor lists, checkboxes, and type icons; added collapsing and filtering; and improved the narrow-screen layout.
- API-key routing rules are displayed as pills with hover details, dialog-based editing, and confirmation before clearing.

## v1.3.8

### Backend

- Optimized analytics, request-event, error-event, and plugin-log queries for large datasets, making key filtering and record counting faster.

### Frontend

- Accelerated the API Keys and Setup pages, reduced duplicate requests, and stopped queries from continuing after logout.
- Standardized page spacing and card heights, and improved the narrow-screen display of time ranges, routing selection, and edit dialogs.
- Added copyright, version, repository, and license information to the page footer.

## v1.3.7

### Upgrade notes

- Back up the data before upgrading. Historical records are retained after the upgrade; restore the pre-upgrade backup if you downgrade.

### Backend

- Fixed API keys with directly assigned models bypassing the credential restrictions of bound routing rules.
- Removed the credential table; credential information is now recorded directly in the request-event table.
- Added a table for configured AI providers to persist the configured-provider list synchronized by the administrator UI.

### Frontend

- Removed automatic refresh and fixed incomplete loading of routing model and credential options.
- Fixed incorrect persistence of filters, time ranges, and chart options.

## v1.3.6

### Backend

- Subscription plans support spend, token, and request quotas, which can be configured individually or combined in each window.

### Frontend

- Standardized subscription-plan and routing-rule card styles, and improved plan editing, quota presentation, and concurrency-limit layouts.
- Fixed API-key sessions ending on page refresh when **Remember key** was not selected.
- Prevented confirmation dialogs from incorrectly triggering browser autofill and simplified reset prompts.

## v1.3.5

### Upgrade notes

- Back up the data before upgrading. Databases from v1.0.0 through v1.2.3 can be migrated; v0.8.4 and earlier require a new data file.
- Existing prices are retained as custom prices and can be deleted to use reference prices. Previous wildcard prices must be replaced with per-model-ID configuration.
- Existing subscription plans migrate to a single quota window while preserving used quota. **Never reset** plans become 365-day periods.

### Backend

- Unpriced requests participate in analytics aggregation at zero cost instead of hiding total cost and trend data.
- Changed the built-in `codex-auto-review` price from free to the models.dev GPT-5.4 price, including cache-read and long-context tiers.

### Frontend

- Unknown token counts are left blank rather than shown as zero in request-event CSV exports.

## v1.3.4

### Upgrade notes

- Back up the data before upgrading. Databases from v1.0.0 through v1.2.3 can be migrated; v0.8.4 and earlier require a new data file.
- Existing prices are retained as custom prices and can be deleted to use reference prices. Previous wildcard prices must be replaced with per-model-ID configuration.
- Existing subscription plans migrate to a single quota window while preserving used quota. **Never reset** plans become 365-day periods.

### Backend

- Added the `codex_fast_mode_billing` switch, disabled by default. When enabled, Codex requests containing `service_tier=priority` are billed at 2.5 times the standard rate.
- Request records retain their billing multiplier, so historical costs and multiplier markers remain unchanged after the switch is disabled.

### Frontend

- Request events combine the context tier and multiplier, for example **Standard · x2.5**; cost details note that unit prices and amounts include the multiplier.
- Added a `multiplier` column to request-event CSV exports to distinguish standard and 2.5x billing.

## v1.3.3

### Upgrade notes

- Back up the data before upgrading. Databases from v1.0.0 through v1.2.3 can be migrated; v0.8.4 and earlier require a new data file.
- Existing prices are retained as custom prices and can be deleted to use reference prices. Previous wildcard prices must be replaced with per-model-ID configuration.
- Existing subscription plans migrate to a single quota window while preserving used quota. **Never reset** plans become 365-day periods.

### Backend

- Split query endpoints by resource. Configuration mutations can return the latest view directly, reducing subsequent queries.
- Request and error events provide stable IDs and paginated snapshots, preventing duplicated or omitted records during infinite scrolling and export.

### Frontend

- Standardized on-demand loading, shared caching, and incremental rendering to reduce duplicate requests and interface reconstruction. Only the active page refreshes automatically, and analytics ranges longer than 24 hours do not auto-refresh.
- The interface updates immediately after successful configuration submissions. Forms remain locked during submission and retain edited content when a submission fails.
- Standardized success/failure and cost presentation for request events, simplified cost details, and improved event and plugin-log card heights.
- Request and error events can be exported to CSV with the active filters. Exports contain only displayed fields, use English column names, and retain unformatted values.
- Added one-click import of CPAMP API-key notes, skipping unchanged and unmatched records.

## v1.3.2

### Upgrade notes

- Back up the data before upgrading. Databases from v1.0.0 through v1.2.3 can be migrated; v0.8.4 and earlier require a new data file.
- Existing prices are retained as custom prices and can be deleted to use reference prices. Previous wildcard prices must be replaced with per-model-ID configuration.
- Existing subscription plans migrate to a single quota window while preserving used quota. **Never reset** plans become 365-day periods.

### Backend

- Added built-in prices for `codex-auto-review` and `gpt-image-1.5`. Their precedence is lower than custom prices and higher than reference prices.
- Added a debug-log switch, disabled by default. Info and error logs are unaffected.

### Frontend

- Reused existing chart instances when analytics charts refresh, reducing flicker.
- Request events hide the cache-write column when the cache-write value is zero.

## v1.3.1

### Upgrade notes

- Back up the data before upgrading. Databases from v1.0.0 through v1.2.3 can be migrated; v0.8.4 and earlier require a new data file.
- Existing prices are retained as custom prices and can be deleted to use reference prices. Previous wildcard prices must be replaced with per-model-ID configuration.
- Existing subscription plans migrate to a single quota window while preserving used quota. **Never reset** plans become 365-day periods.

### Frontend

- Added average daily/hourly request, token, and cost figures plus peak cache rate to analytics cards.
- Fixed plugin-log level counts, added automatic loading while scrolling, constrained the list height, and show loading progress.
- API-key subscription-plan dropdowns size themselves to their option content to avoid excessive width.

## v1.3.0

### Upgrade notes

- Back up the data before upgrading. Databases from v1.0.0 through v1.2.3 can be migrated; v0.8.4 and earlier require a new data file.
- Existing prices are retained as custom prices and can be deleted to use reference prices. Previous wildcard prices must be replaced with per-model-ID configuration.
- Existing subscription plans migrate to a single quota window while preserving used quota. **Never reset** plans become 365-day periods.

### Backend

- Reworked model pricing: custom prices take precedence, models.dev supplies reference prices, and requests without an available price are blocked. Reference-price maintenance no longer depends on opening the frontend.
- Reworked API-key management: a key is managed automatically when it first reports usage, historical associations remain after deletion, and re-added keys retain their configuration and quota.
- Reworked subscription plans: plans support multiple custom quota windows with independent periods beginning on first use; new requests pause when any window is exhausted.
- Extended request-event and error-event retention to 365 days.

### Frontend

- Added custom-price deletion, deleted-key management, and multi-window quota presentation.
- Added request-count and cost-share usage distributions. The time-range picker supports hourly, daily, and custom ranges.

## v1.2.3

### Upgrade notes

- Upgrading from v1.0.0 through v1.1.3 automatically migrates SQLite V10 or V11 databases to V12. Earlier formats are no longer supported; use a new database file and reconfigure the plugin.

### Backend

- Simplified and standardized parameter validation, credential states, and runtime log messages while retaining diagnostic detail.

### Frontend

- Fixed credential identifier calculation over HTTP, per-credential preference storage, and API-key copying, with compatibility handling for environments where clipboard access is restricted.
- Network failures and malformed responses show concise messages, filter proxy error pages, and limit error-message length.
- Standardized interface messages and accurately show unavailable upstream credentials.

## v1.2.2

### Upgrade notes

- Upgrading from v1.0.0 through v1.1.3 automatically migrates SQLite V10 or V11 databases to V12. Earlier formats are no longer supported; use a new database file and reconfigure the plugin.

### Backend

- Error events prefer the upstream JSON `code` as their error type and fall back to `type` when no code is provided.
- The analytics API returns masked API-key identifiers, allowing usage distribution to render without an additional access-data query.

### Frontend

- Global refresh now includes the active tab and top-navigation data dependencies; analytics no longer waits for the `access` endpoint before rendering.
- Fixed duplicate requests and stale-session response interference, stabilizing request-event pagination and export time ranges.
- Added an error-type filter with counts to error events, including filtering for empty error types, and improved the narrow-screen time, type, and clear-filter layout.
- Added a loading rotation effect to global refresh buttons in the navigation bar and standalone page.

## v1.2.1

### Upgrade notes

- Upgrading from v1.0.0 through v1.1.3 automatically migrates SQLite V10 or V11 databases to V12. Earlier formats are no longer supported; use a new database file and reconfigure the plugin.

### Backend

- Record every usage event reported by the host, including requests without an API-key owner and those with `generate=false`. Unowned usage appears only in analytics and is not charged to a subscription.
- After API-key login, auth-file and quota queries follow routing restrictions and exclude configured AI-provider credentials.

### Frontend

- Added per-tab refresh buttons so the current data can be updated without reloading the host page.
- Improved API-key identifiers, disabled credentials, model prices, error events, and responsive navigation.

## v1.2.0

### Upgrade notes

- Upgrading from v1.0.0 through v1.1.3 automatically migrates SQLite V10 or V11 databases to V12. Earlier formats are no longer supported; use a new database file and reconfigure the plugin.

### Backend

- Standardized subscription periods as seconds in storage and transport and removed period types. SQLite V10 and V11 databases migrate directly to V12.
- An empty binding now represents all routes. Removed the `system:all` entity and special branches, and standardized route-state updates.
- API-key subscription, usage, concurrency, and routing data now comes from `access`; removed redundant state endpoints and the retired-key management view.
- Standardized auth-quota responses, failed-request filters, and quota-reset contracts, and simplified analytics cost and model data structures.

### Frontend

- Routing selection uses **All routes** for empty bindings and allows selections to be cleared directly; `system:all` is no longer shown.
- Remembers filters, time ranges, analytics dimensions, credential toggles, and standalone-page theme, isolated by login credential.
- The API-key access page uses unified access data and no longer shows redundant runtime state.
- Fixed duplicate trend-chart rendering when switching usage-distribution dimensions and reduced trend-chart grid density and contrast.

## v1.1.3

### Upgrade notes

- Upgrading from v1.0.0 or v1.0.1 automatically migrates the database and converts existing model permissions into routing rules.
- Data from versions earlier than v1.0.0 cannot be migrated; use a new database file and reconfigure the plugin.

### Backend

- Added analytics buckets for request counts, token categories, cache rate, and cost trends. Ranges up to 24 hours aggregate hourly; longer ranges aggregate by calendar day in the browser's time zone. Removed RPM and TPM calculations.

### Frontend

- Added distinct colors, gradient backgrounds, and trend sparklines to analytics summary cards, and condensed token and cost details into single lines.
- Added a combined usage-trend chart with stacked token-category bars and lines for request count, total cost, and average token count.
- Improved mobile filter and action layouts and fixed content leaking through the CPAMC navigation bar's gradient edge.

## v1.1.2

### Upgrade notes

- Upgrading from v1.0.0 or v1.0.1 automatically migrates the database and converts existing model permissions into routing rules.
- Data from versions earlier than v1.0.0 cannot be migrated; use a new database file and reconfigure the plugin.

### Backend

- Synchronize secure fingerprints for configured upstream credentials so individual upstream API keys can be used for exact routing before their first request.

### Frontend

- The routing credential list directly shows masked configured upstream API keys and distinguishes between no credentials and no enabled credentials.
- Routing dialogs adapt their width to content and available space, improving layouts for long names, labels, and the **Enabled only** switch.

## v1.1.1

### Upgrade notes

- Upgrading from v1.0.0 or v1.0.1 automatically migrates the database and converts existing model permissions into routing rules.
- Data from versions earlier than v1.0.0 cannot be migrated; use a new database file and reconfigure the plugin.

### Backend

- Fixed routing logs failing to record the credential actually selected and clearly identify when no matching credential is available.
- Improved upstream API-key credential recognition, showing a secure masked value without recording the complete secret.

### Frontend

- Fixed configured AI providers that had not yet been used being absent from the routing credential list.
- Added an **Enabled only** filter to the routing credential list, distinguished auth files from AI providers, and standardized sorting, icons, and dropdown styles.

## v1.1.0

### Upgrade notes

- Upgrading from v1.0.0 or v1.0.1 automatically migrates the database and converts existing model permissions into routing rules.
- Data from versions earlier than v1.0.0 cannot be migrated; use a new database file and reconfigure the plugin.

### Backend

- Added routing rules that restrict models and upstream credentials per API key and schedule among available credentials matching those rules. Routing decisions are written to plugin logs. Unauthorized models return HTTP 403; unavailable credentials or invalid configuration return HTTP 503.

### Frontend

- Replaced allowed-model management with routing-rule management, added API-key routing selection, and changed subscription plans and routing rules to card layouts.
- Improved desktop and mobile rendering in standalone, CPAMC, and CPAMP contexts.

## v1.0.1

### Frontend

- Added an API-key copy button.
- Fixed an allowed-model selector bug.

## v1.0.0

### Upgrade notes

- v1.0.0 does not migrate legacy JSON or SQLite data. Use a new database and reconfigure the plugin after upgrading.
- The default database path changed to `plugins/cpa-key-billing-state-v1.db`. If `state_file` was set explicitly, change its path before upgrading and keep the old file as a backup.

### Backend

- Standardized backend APIs and data structures for management-key and API-key login.
- Reworked the database to store request events, error events, and plugin runtime logs separately.
- Removed cumulative usage data for API keys and models; analytics are now calculated from request events in real time.

### Frontend

- Redesigned the administration and API-key self-service pages and added **Analytics** and **Error events**.
- Added analytics for request counts, tokens, cache rate, cost, and usage distribution by time range, API key, model, and source.
- Consolidated subscription plans, model groups, model pricing, and plugin logs on the Setup page.
- Reworked refresh behavior: data loads immediately on first entry; request events, error events, and plugin logs pause automatic refresh after scrolling away from the top; analytics do not refresh automatically.
- Completed theme adaptation and narrow-screen layouts for standalone, CPAMC, and CPAMP contexts.
