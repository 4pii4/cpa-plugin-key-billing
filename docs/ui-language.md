# UI language

The language selector lives under **Settings → Interface preferences** in both
the administrator and API-key self-service views, including embedded CPA pages.
The self-service Settings page contains only browser preferences, with no admin
configuration access. A selector remains on the sign-in page for use before login.
The global email-mask shortcut stays in the authenticated page header.

- **Follow CPA** (default): use the same-origin host document's standard HTML
  `lang` attribute. When opened independently, read CPA's saved language on the
  same origin, then fall back to the browser's preferred language.
- **English** / **简体中文**: explicitly override the host and browser language.

Chinese variants resolve to Simplified Chinese. Other languages resolve to
English. The selection is stored in `cpa-key-billing:language`, independently of
CPA's setting. It survives refreshes, new tabs, and sign-in mode changes. A blocked
localStorage falls back to a language-only URL parameter for the current page.
The plugin never writes CPA's preference.

Changing the language reloads the plugin document, including all tables, charts,
and dialogs. Save pending edits first (also stated in the selector's tooltip).
Existing session credentials and remembered credentials use the unchanged
credential vault. Language reloads wait for an in-progress sign-in to finish saving
its session. No new authentication or billing mechanism is introduced.

In Follow CPA mode, a `MutationObserver` watches only the same-origin parent's
standard `lang` attribute. Browser `storage` events pick up CPA language changes
in another same-origin document, including an independently opened plugin page.
There are no polling timers, message bridges, or selectors tied to the host's
internal layout. Cross-origin parents cannot be read; in that case the plugin
uses its own origin's saved preference or the browser language. Manually selected
languages remain authoritative when the host changes.

## Verified host contract

Research on 2026-11-05 used CPAMC commit
`f154fa9380825c4fe94bc577a9abaa7e505f8875` and the actual management asset served by
CPA v8.0.56 (`585f97d`). The management HTML SHA-256 was
`2f6cf57a8ca4ace1fadde0280cbd41084f56999738d0e692a30b135420f7fc30`.

- [PluginResourcePage.tsx](https://github.com/router-for-me/Cli-Proxy-API-Management-Center/blob/f154fa9380825c4fe94bc577a9abaa7e505f8875/src/features/plugins/PluginResourcePage.tsx)
  renders the backend-declared plugin resource in an iframe, without a language
  query parameter or a language `postMessage` bridge.
- [useLanguageStore.ts](https://github.com/router-for-me/Cli-Proxy-API-Management-Center/blob/f154fa9380825c4fe94bc577a9abaa7e505f8875/src/stores/useLanguageStore.ts)
  persists the language with Zustand under `cli-proxy-language`.
- [language.ts](https://github.com/router-for-me/Cli-Proxy-API-Management-Center/blob/f154fa9380825c4fe94bc577a9abaa7e505f8875/src/utils/language.ts)
  reads `{ "state": { "language": "zh-CN" }, "version": 0 }`, as well as the
  legacy object, JSON-string, and plain-string formats.
- [App.tsx](https://github.com/router-for-me/Cli-Proxy-API-Management-Center/blob/f154fa9380825c4fe94bc577a9abaa7e505f8875/src/App.tsx)
  synchronizes `document.documentElement.lang` whenever the language changes.

Runtime inspection confirmed that the installed plugin is in a same-origin
iframe, while CPA's document had `lang="zh-CN"` and the above persisted value.
The original v1.4.2 plugin still had `lang="en"`. This work changes only the fork;
the production plugin was not replaced.

## Translation boundaries

English message IDs and the inline `ZH_CN` dictionary keep the single embedded
HTML artifact self-contained. `t()` is called on explicit UI literals. The initial
static markup is translated once, before the application inserts runtime data;
there is no live DOM text rewriting. Interpolation inserts plain text.

API keys, model IDs, custom notes, plan/window names, route names, provider names,
and account metadata remain data, even when they equal an English UI message.
Known plugin-generated readiness and auto-start messages have a separate, exact
message/template allowlist. Captured IDs and values remain unchanged. Unknown
upstream diagnostics, raw error bodies, and structured runtime logs retain their
original content for debugging. These are not a host language interface.

No Go implementation, SQL schema, auth, quotas, billing, or native routing logic
changed. The dummy backend's readiness summaries were aligned with the actual
Go-produced messages so browser verification exercises the real text contract.

## Checks

```sh
npm ci --prefix scripts
node scripts/format_ui.mjs --check
npm test --prefix scripts
gofmt -l .
go test -race ./...
go vet ./...
go mod tidy -diff
python3 scripts/frontend_dummy_backend.py --port 18765 --host cpamc
```

Browser regression scripts run outside the repository's `scripts/` directory.
See [validation evidence](evidence/ui-i18n-20261105/README.md) for the exercised
flows, screenshots, and native ARM64 load verification.
