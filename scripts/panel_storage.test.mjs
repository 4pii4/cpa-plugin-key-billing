import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import test from "node:test";

const html = fs.readFileSync(new URL("../internal/plugin/ui.html", import.meta.url), "utf8");
const panelStorageCode = html.slice(html.indexOf("function panelStoredValue"), html.indexOf("/* Apply the host or stored theme"));
function readPanelFixture(raw, userAgent = "test-agent") {
  return vm.runInNewContext(`${panelStorageCode}; panelStoredValue("cli-proxy-auth")`, {
    localStorage: { getItem: () => raw }, location: { host: "cpa.test:8317" }, navigator: { userAgent },
    TextEncoder, TextDecoder, atob
  });
}
function panelFixture(version, userAgent = "test-agent") {
  const salt = Buffer.from("cli-proxy-api-webui::secure-storage|cpa.test:8317" + (version === 1 ? "|" + userAgent : ""));
  const bytes = Buffer.from(JSON.stringify({ state: { managementKey: "synthetic-fixture-only" } }));
  return `enc::v${version}::` + Buffer.from(bytes.map((byte, index) => byte ^ salt[index % salt.length])).toString("base64");
}
test("CPAMC v2 saved credentials remain readable across browser UA changes", () => {
  assert.equal(readPanelFixture(panelFixture(2), "another-agent").state.managementKey, "synthetic-fixture-only");
});
test("legacy CPAMC v1 and unwrapped preferences retain their existing behavior", () => {
  assert.equal(readPanelFixture(panelFixture(1)).state.managementKey, "synthetic-fixture-only");
  assert.equal(readPanelFixture(panelFixture(1), "another-agent"), null);
  assert.equal(readPanelFixture('{"state":{"theme":"dark"}}').state.theme, "dark");
});
test("malformed or missing host storage never exposes a credential", () => {
  for (const raw of [null, "", "enc::v2::broken!", "enc::v3::unknown", "{invalid"]) assert.equal(readPanelFixture(raw), null);
});
