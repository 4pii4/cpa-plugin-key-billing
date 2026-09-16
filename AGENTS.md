# Repository Guidelines

## Related Source Repositories

Source paths are relative to this repository's root:

- **CLIProxyAPI:** `../CLIProxyAPI`
- **CPAMC:** `../Cli-Proxy-API-Management-Center`
- **CPAMP:** `../CPA-Manager-Plus`

## Required Checks

- **Before committing:** Run `gofmt -l .`; format any listed files and rerun until the output is empty.
- **Frontend changes:** Start `python3 scripts/frontend_dummy_backend.py --port 18765` (not the default port) and verify affected desktop and narrow-screen layouts with Playwright, beyond static checks.
- **Frontend regression scripts:** Store JavaScript scripts used with `playwright-cli` for browser regression testing in a temporary directory, never in the project's `scripts/` directory.
- **Billing changes:** Run `scripts/e2e_cpa_billing.sh v7.2.143` after modifying any billing behavior, including usage parsing, pricing, quota enforcement, or failure reporting.

## UI Formatting

- Use `scripts/format_ui.mjs` for `internal/plugin/ui.html`; do not run plain Prettier on that file, since it expands intentionally compact code.
- Requires Node.js 20+ and npm. Install the pinned development-only dependencies with `npm ci --prefix scripts` after checkout or a lockfile change.
- Format with `node scripts/format_ui.mjs`. Check without writing with `node scripts/format_ui.mjs --check` (exit status 1 means changes are needed or validation failed). The default target is resolved relative to the script, independently of the working directory.
- Keep 2-space indentation, useful blank lines, and multi-statement blocks. Short CSS rules, HTML elements, and JavaScript expressions or single-statement blocks are kept on one line where practical, using roughly 140 characters as a guide rather than minifying.
- The formatter checks JavaScript ASTs, CSS structure, and HTML display text before writing. If validation fails, inspect the unsupported formatting case; do not bypass the check or change application behavior just to make formatting pass.
- After changing the formatter, run `npm test --prefix scripts` and verify that a second format pass leaves `ui.html` unchanged. The desktop/narrow-screen browser checks above still apply to UI formatting changes.

## Architecture Invariants

- Implement every plugin feature within CLIProxyAPI's existing capabilities. Do not propose or rely on CLIProxyAPI modifications as part of the plugin implementation.
- `usage.handle` is the only source of provider usage, billing data, latency, and upstream failure details. Do not reconstruct usage from raw responses or request/response lifecycle hooks.
- Use `request.intercept_before` only for admission controls such as model, quota, and concurrency enforcement. Use `request.complete` only for lifecycle bookkeeping such as releasing concurrency slots.
- If `UsageRecord` does not expose required information, degrade the feature honestly. Never correlate concurrent records heuristically by model, credential, timestamps, or route.
- Failure events use `UsageRecord.Failed` and `UsageRecord.Failure`. Log only fields present in the record; do not invent a downstream request path or request ID.
- Preserve the host's TTFT value as reported for both streaming and non-streaming requests. Do not infer streaming mode from TTFT, response headers, or approximate latency equality.
- Keep provider token semantics aligned with CLIProxyAPI. In particular, Claude's raw `OutputTokens` includes reasoning tokens; do not charge reasoning twice.
- Do not introduce plugin-owned background goroutines, timers, or flushers. Complete work synchronously within host calls so the embedded Go runtime remains inactive between calls.

## Data and Compatibility

- Do not bump the SQLite schema version for an idempotent repair or code cleanup. A real format change requires an explicit migration and review.
- Preserve historical data during SQLite and legacy JSON migrations, including failed or all-zero usage rows. If a legacy schema is incompatible, fail and roll back instead of dropping or silently hiding its table.
- Never persist or log plaintext downstream or upstream API keys. Mask API-key credentials, omit uncertain account values, and use dummy credentials in tests; do not copy real credentials into the workspace.

## Release and Changelog

- Before tagging, increment the patch version unless the user explicitly requests a major or minor change, and create an annotated tag with `git tag -a` and a message. Whenever changing `Version` in `internal/plugin/types.go`, update the footer version in `internal/plugin/ui.html` to match.
- Edit `Changelog.md` only when the user explicitly requests preparation for a tag or release.
- Prepend one `## vX.Y.Z` section directly below `# Changelog`; never append releases or add an unreleased placeholder.
- Treat sections for tags that already exist as immutable history. Do not edit, move, merge, or delete them unless the user explicitly requests changes to that tag's entry.
- Use concise English bullets that describe released behavior, not the development process. Include only relevant sections, ordered as `### Upgrade notes`, `### Backend`, then `### Frontend`.
- Put breaking changes, migration requirements, and operator actions in `### Upgrade notes`. Omit implementation details unless they affect users or operators.

## Commit Messages

- Use Conventional Commits: `<type>(<scope>): <imperative summary>`, in concise English. Include `scope` only when it clearly identifies the affected module.
- Use one of these common types: `feat`, `fix`, `refactor`, `perf`, `docs`, `test`, `build`, `ci`, or `chore`.
- Keep each commit focused on one logical change.
- Omit the body only for simple, narrowly scoped changes fully explained by the subject. Otherwise, add a blank line and a body explaining motivation, important implementation details, and behavioral impact; wrap at approximately 72 columns.
- Describe the final change, not the development process or implementation history.
