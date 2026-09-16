import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { assertEquivalent, formatUI } from "./format_ui.mjs";

const document = ({ css = "", js = "", html = "" } = {}) => `<!doctype html>
<html>
  <head>
    <style>
${css
  .split("\n")
  .map((line) => "      " + line)
  .join("\n")}
    </style>
  </head>
  <body>
${html}
    <script>
${js
  .split("\n")
  .map((line) => "      " + line)
  .join("\n")}
    </script>
  </body>
</html>
`;

test("compacts short CSS, HTML and JavaScript while preserving groups", async () => {
  const before = document({
    css: ".row {\n  display: flex;\n  gap: 4px;\n}\n\n.hidden {\n  display: none;\n}",
    html: '    <button\n      id="save"\n      type="button"\n    >Save</button>',
    js: "function label(value) {\n  return String(value);\n}\n\nfunction update() {\n  first();\n\n  second();\n}"
  });
  const after = await formatUI(before);
  assert.match(after, /      \.row \{ display: flex; gap: 4px; \}/);
  assert.match(after, /<button id="save" type="button">Save<\/button>/);
  assert.match(after, /function label\(value\) \{ return String\(value\); \}/);
  assert.match(after, /first\(\);\n\n        second\(\);/);
  assert.equal(await formatUI(after), after);
});

test("preserves comments, literals, raw HTML text and thin separators", async () => {
  const before = document({
    css: '.icon { background: url("data:image/svg+xml,%3Csvg%3E%3C/svg%3E"); }',
    html: "    <pre><span>A\n   B · C</span></pre>\n    <textarea>A\n B</textarea>\n    <span>A&thinsp;·&thinsp;B</span>",
    js: [
      "// Keep A · B unchanged.",
      "const raw = `A · B",
      " odd indentation",
      "`;",
      'const separator = "\\u2009·\\u2009";',
      'const escaped = "a\\',
      'b";',
      'const data = "URL > text";'
    ].join("\n")
  });
  const after = await formatUI(before);
  assertEquivalent(before, after);
  assert.match(after, /<pre><span>A\n   B · C<\/span><\/pre>/);
  assert.match(after, /<textarea>A\n B<\/textarea>/);
  assert.match(after, /A&thinsp;·&thinsp;B/);
  assert.equal(await formatUI(after), after);
});

test("quotes inside CSS comments do not interfere with two-space indentation", async () => {
  const before = document({
    css: "/* Resolve CPAMP's semantic tokens. */\n.row {\n  display: flex;\n  gap: 4px;\n}\n.after { content: 'done'; }"
  });
  const after = await formatUI(before);
  assert.match(after, /\n      \/\* Resolve CPAMP's semantic tokens\. \*\//);
  assert.match(after, /\n      \.row \{ display: flex; gap: 4px; \}/);
  assert.match(after, /\n      \.after/);
  assert.equal(await formatUI(after), after);
});

test("keeps method-chain callback indentation and long loop bodies readable", async () => {
  const before = document({
    js: [
      "function render(value, fields, price, longContext) {",
      "  displayLabel(value)",
      "    .split(DISPLAY_SEPARATOR)",
      "    .forEach((part, index) => {",
      "      if (index) appendSeparator();",
      "      append(part);",
      "    });",
      '  for (const [label, field] of [["Cache read", "cache_read_per_1m"], ["Cache write", "cache_write_per_1m"]]) {',
      "    if (price?.[field] != null || longContext?.[field] != null) fields.push([label, field]);",
      "  }",
      "}"
    ].join("\n")
  });
  const after = await formatUI(before);
  assert.match(after, /        displayLabel\(value\)\.split\(DISPLAY_SEPARATOR\)\.forEach\(\(part, index\) => \{\n          if/);
  assert.match(after, /          append\(part\);\n        \}\);/);
  assert(after.split("\n").every((line) => line.length <= 140));
  assert.equal(await formatUI(after), after);
});

test("handles attributed inline scripts and styles without touching JSON scripts", async () => {
  const before = document({ js: "function value() {\n  return 42;\n}" })
    .replace("<script>", '<script type="module">')
    .replace("<style>", '<style media="screen">')
    .replace("  </body>", '    <script type="application/json">{\n "label": "A · B"\n}</script>\n  </body>');
  const after = await formatUI(before);
  assert.match(after, /function value\(\) \{ return 42; \}/);
  assert.match(after, /<script type="application\/json">\{\n "label": "A · B"\n\}<\/script>/);
  assert.throws(() => assertEquivalent(after, after.replace('"label": "A · B"', '"label": "changed"')), /HTML changed/);
  assert.equal(await formatUI(after), after);
});

test("semantic safety checks reject behavior and display-text changes", () => {
  const before = document({ css: ".row { color: red; }", js: "const answer = 42;", html: "    <span>A · B</span>" });
  assert.throws(() => assertEquivalent(before, before.replace("42", "43")), /AST changed/);
  assert.throws(() => assertEquivalent(before, before.replace("color: red", "color: blue")), /Stylesheet/);
  assert.throws(() => assertEquivalent(before, before.replace("A · B", "A · B")), /HTML changed/);
});

test("CLI check mode is read-only, write mode is idempotent, and invalid input is preserved", (t) => {
  const directory = mkdtempSync(join(tmpdir(), "cpa-format-test-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const file = join(directory, "ui.html");
  const script = fileURLToPath(new URL("./format_ui.mjs", import.meta.url));
  const run = (...args) => spawnSync(process.execPath, [script, ...args, file], { cwd: directory, encoding: "utf8" });
  const before = document({ js: "function answer() {\n  return 42;\n}" });
  writeFileSync(file, before);
  assert.equal(run("--check").status, 1);
  assert.equal(readFileSync(file, "utf8"), before);
  assert.equal(run().status, 0);
  const formatted = readFileSync(file, "utf8");
  assert.notEqual(formatted, before);
  assert.equal(run("--check").status, 0);
  assert.equal(run().status, 0);
  assert.equal(readFileSync(file, "utf8"), formatted);
  const invalid = document({ js: "const = ;" });
  writeFileSync(file, invalid);
  assert.equal(run().status, 1);
  assert.equal(readFileSync(file, "utf8"), invalid);
});
