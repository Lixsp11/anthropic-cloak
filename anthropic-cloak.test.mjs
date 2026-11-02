import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import * as mod from "./anthropic-cloak.middleware.js";

const cases = JSON.parse(readFileSync(new URL("./cases.json", import.meta.url), "utf8"));
for (const c of cases) {
  test(c.name, async () => {
    const original = structuredClone(c.request);
    const ctx = {
      protocol: "anthropic", model: "", stream: false, path: "", ...c.ctx,
      options: c.options ?? {}, state: {},
    };
    let request = structuredClone(c.request);
    if (c.provider) {
      const result = await mod.onProvider(request, { ...ctx, provider: c.provider });
      // Magpie serializes the returned body to JSON; undefined fields are omitted.
      request = result === undefined ? c.request : JSON.parse(JSON.stringify(result));
    }
    assert.deepEqual(request, c.expect_request ?? c.request);
    assert.deepEqual(c.request, original);
  });
}

test("standalone package resolves its renamed middleware entry", () => {
  const pkg = JSON.parse(readFileSync(new URL("./package.json", import.meta.url), "utf8"));
  assert.equal(pkg.name, "anthropic-cloak");
  assert.equal(pkg.magpie.name, "Anthropic Cloak");
  assert.equal(pkg.magpie.middleware, "./anthropic-cloak.middleware.js");
  assert(existsSync(new URL(pkg.magpie.middleware, import.meta.url)));
  assert.equal(typeof mod.onProvider, "function");
});
