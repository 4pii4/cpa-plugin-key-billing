import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import test from "node:test";
import { parse } from "@babel/parser";
import * as parse5 from "parse5";

const html = fs.readFileSync(new URL("../internal/plugin/ui.html", import.meta.url), "utf8");
const scripts = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map((match) => match[1]);
const runtime = scripts.find((script) => script.includes("const ZH_CN ="));
const main = scripts.at(-1);
function language({ own, host, parentLanguage, browser = "en-US", blocked = false, search = "" } = {}) {
  const context = {
    window: {}, parent: { document: { documentElement: { lang: parentLanguage || "" } } },
    document: { documentElement: {} }, navigator: { language: browser, languages: [browser] },
    location: { search }, URLSearchParams,
    localStorage: { getItem(key) {
      if (blocked) throw new Error("Storage blocked");
      return key === "cpa-key-billing:language" ? own : host;
    } }
  };
  if (!parentLanguage) context.parent = context.window;
  vm.createContext(context);
  vm.runInContext(runtime, context);
  const result = vm.runInContext("({ t, pluginMessage, uiLocale, languageMode, ZH_CN, reloadLanguage, setLanguageBusy })", context);
  result.context = context;
  return result;
}

test("manual language overrides the host, and invalid preferences follow CPA", () => {
  assert.equal(language({ own: "en", parentLanguage: "zh-CN" }).uiLocale, "en");
  assert.equal(language({ own: "zh-CN", parentLanguage: "en" }).uiLocale, "zh-CN");
  assert.equal(language({ own: "broken", parentLanguage: "zh-CN" }).uiLocale, "zh-CN");
  assert.equal(language({ parentLanguage: "ru", browser: "zh-CN" }).uiLocale, "en");
});

test("standalone understands the source-backed CPA preference formats and browser fallback", () => {
  for (const host of ['{"state":{"language":"zh-CN"},"version":0}', '{"language":"zh-CN"}', '"zh-CN"', "zh-CN"]) {
    assert.equal(language({ host }).uiLocale, "zh-CN");
  }
  assert.equal(language({ host: "{invalid", browser: "zh-CN" }).uiLocale, "zh-CN");
  assert.equal(language({ host: "{}", browser: "zh-CN" }).uiLocale, "zh-CN");
  assert.equal(language({ browser: "zh-Hans-SG" }).uiLocale, "zh-CN");
  assert.equal(language({ browser: "fr-FR" }).uiLocale, "en");
  assert.equal(language({ blocked: true, browser: "zh-CN" }).uiLocale, "zh-CN");
  assert.equal(language({ blocked: true, search: "?billing-language=en", browser: "zh-CN" }).uiLocale, "en");
});

test("English stays intact; interpolation preserves model IDs and unknown diagnostics", () => {
  const en = language();
  const zh = language({ own: "zh-CN" });
  assert.equal(en.t("Requests"), "Requests");
  assert.equal(zh.t("Requests"), "请求记录");
  assert.equal(zh.t("{count} days from now", { count: 3 }), "3 天后");
  const diagnostic = 'CPA offered all 2 enabled accounts for the last requested model "gpt-6-sol".';
  assert.equal(zh.pluginMessage(diagnostic), 'CPA 为上次请求的模型 "gpt-6-sol" 提供了全部 2 个已启用账号。');
  assert.equal(zh.pluginMessage("provider diagnostic: Requests <raw>"), "provider diagnostic: Requests <raw>");
  assert.equal(zh.t("Unrecognized message"), "Unrecognized message");
  assert.equal(zh.t("{value}", { value: "<b>key</b>" }), "<b>key</b>");
});

test("every explicit UI message has Chinese and matching interpolation placeholders", () => {
  const { ZH_CN } = language();
  let count = 0;
  function walk(node) {
    if (!node?.type) return;
    if (node.type === "CallExpression" && node.callee.name === "t") {
      const visit = (arg) => {
        if (arg.type === "ConditionalExpression") { visit(arg.consequent); visit(arg.alternate); return; }
        assert.equal(arg.type, "StringLiteral", "Runtime business data must not be passed to t()");
        assert.ok(Object.hasOwn(ZH_CN, arg.value), `Missing translation: ${arg.value}`);
        count++;
      };
      visit(node.arguments[0]);
    }
    for (const value of Object.values(node)) {
      if (Array.isArray(value)) value.forEach(walk);
      else if (value?.type) walk(value);
    }
  }
  walk(parse(main));
  assert.ok(count > 700);
  for (const [message, translated] of Object.entries(ZH_CN)) {
    assert.deepEqual([...message.matchAll(/\{\w+\}/g)].map((m) => m[0]).sort(),
      [...translated.matchAll(/\{\w+\}/g)].map((m) => m[0]).sort(), message);
  }
});

test("all initial visible copy and accessible labels are covered, except brands and technical identifiers", () => {
  const { ZH_CN } = language();
  const unchanged = new Set(["CPA Key Billing", "cyber_policy", "Claude", "Antigravity", "Codex", "xAI", "Kimi", "English", "MIT", "LINUX DO"]);
  function scan(node, ignored = false) {
    ignored ||= ["script", "style", "code"].includes(node.tagName);
    if (!ignored) {
      const values = node.nodeName === "#text" ? [node.value.trim()] : [];
      for (const attr of node.attrs || []) if (["title", "placeholder", "aria-label"].includes(attr.name)) values.push(attr.value);
      for (const value of values) {
        if (!/[a-z]/i.test(value)) continue;
        assert.ok(unchanged.has(value) || Object.hasOwn(ZH_CN, value), `Missing static translation: ${value}`);
      }
    }
    for (const child of node.childNodes || []) scan(child, ignored);
  }
  scan(parse5.parse(html));
});

test("translation calls cannot alter CSS classes, protocol values, or form values", () => {
  const technical = new Set(["class", "className", "style", "id", "value", "metric", "key", "endpoint", "tab", "type", "method"]);
  function containsTranslation(node) {
    if (!node?.type) return false;
    if (node.type === "CallExpression" && node.callee.name === "t") return true;
    // The second requireObject argument labels a validation error, not the value.
    if (node.type === "CallExpression" && node.callee.name === "requireObject") return containsTranslation(node.arguments[0]);
    return Object.values(node).some((value) => Array.isArray(value) ? value.some(containsTranslation) : value?.type && containsTranslation(value));
  }
  function walk(node) {
    if (!node?.type) return;
    if (node.type === "ObjectProperty" && technical.has(node.key.name || node.key.value)) {
      assert.equal(containsTranslation(node.value), false, `Technical property ${node.key.name || node.key.value} must stay untranslated`);
    }
    for (const value of Object.values(node)) {
      if (Array.isArray(value)) value.forEach(walk);
      else if (value?.type) walk(value);
    }
  }
  walk(parse(main));
});


test("host language reload waits until the existing sign-in has saved its session", () => {
  const scope = language();
  const button = { disabled: true };
  const control = { disabled: false };
  let reloads = 0;
  scope.context.document.getElementById = () => button;
  scope.context.document.querySelectorAll = () => [control];
  Object.assign(scope.context.location, { href: "http://localhost/ui", reload: () => reloads++ });
  scope.setLanguageBusy(true);
  scope.reloadLanguage();
  assert.equal(reloads, 0);
  assert.equal(control.disabled, true);
  button.disabled = false;
  scope.setLanguageBusy(false);
  assert.equal(reloads, 1);
  assert.equal(control.disabled, false);
});
