import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

const html = readFileSync(new URL("../internal/plugin/ui.html", import.meta.url), "utf8");
const routeCode = html.slice(html.indexOf("const ADMIN_TAB_IDS ="), html.indexOf("const initialRoute = pageRoute();"));

function routes(hash = "", saved = "") {
  return vm.runInNewContext(`(() => { ${routeCode}; return { parsePageRoute, pageRoute, ADMIN_TAB_IDS, ACCOUNT_TAB_IDS }; })()`, {
    embedded: false,
    location: { hash },
    sessionStorage: { getItem: () => saved }
  });
}

test("Analysis is first in both navigation bars and route lists", () => {
  const logic = routes();
  for (const [attribute, ids] of [["data-tab", logic.ADMIN_TAB_IDS], ["data-account-tab", logic.ACCOUNT_TAB_IDS]]) {
    const buttons = [...html.matchAll(new RegExp(`<button[^>]*${attribute}="([^"]+)"[^>]*>([^<]+)</button>`, "g"))];
    assert.equal(buttons[0][1], "analysis");
    assert.match(buttons[0][0], /class="active"/);
    assert.deepEqual(buttons.map((match) => match[1]), Array.from(ids));
    assert.equal(buttons.find((match) => match[1] === "auth-files")[2], "Auth file");
    assert.equal(buttons.find((match) => match[1] === "request-events")[2], "Requests");
    assert.equal(buttons.find((match) => match[1] === "errors")[2], "Errors");
  }
  assert.match(html, /<section id="tab-analysis">/);
  assert.match(html, /<section id="account-tab-analysis">/);
  assert.match(html, /<section id="tab-keys" class="hidden">/);
  assert.match(html, /<section id="account-tab-subscription" class="hidden">/);
});

test("New sessions and invalid tab routes land on Analysis", () => {
  assert.equal(routes().pageRoute().tab, "analysis");
  for (const role of ["admin", "account"]) {
    assert.equal(routes(`#${role}`).pageRoute().tab, "analysis");
    assert.equal(routes(`#${role}/missing`).pageRoute().tab, "analysis");
    assert.equal(routes().parsePageRoute(role).tab, "analysis");
  }
});

test("Explicit and remembered routes survive the default-tab change", () => {
  for (const route of ["admin/keys", "admin/auth-files", "account/subscription", "account/errors"]) {
    assert.equal(routes(`#${route}`).pageRoute().tab, route.split("/")[1]);
    assert.equal(routes("", route).pageRoute().tab, route.split("/")[1]);
  }
  assert.equal(routes("#admin/analysis", "admin/keys").pageRoute().tab, "analysis");
});

test("Auth labels stay compact in both views", () => {
  assert.equal(html.includes("Authentication files"), false);
  assert.equal(html.includes("Obfuscate emails"), false);
  assert.equal((html.match(/<span>Mask emails<\/span>/g) || []).length, 2);
  assert.equal((html.match(/placeholder="Search auth files"/g) || []).length, 2);
});
