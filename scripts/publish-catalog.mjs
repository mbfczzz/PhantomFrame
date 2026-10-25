import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";

// Review the plan by default; --apply publishes drafts with the UI's defaults.
const apply = process.argv.includes("--apply");
const base = "http://127.0.0.1:3001/api";
const login = await fetch(base + "/auth/login", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: await readFile(".local-credentials.json", "utf8"),
});
assert.ok(login.ok, "Administrator login failed");
const cookie = login.headers.get("set-cookie").split(";")[0];
const call = async (path, method = "GET", body) => {
  const response = await fetch(base + path, {
    method,
    headers: { Cookie: cookie, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(30000),
  });
  const value = await response.json();
  if (!response.ok) throw new Error(`${method} ${path}: ${value.error || value.message || response.status}`);
  return value;
};
const entriesFor = async (provider) => {
  const entries = [];
  for (let page = 1; ; page++) {
    const result = await call(`/admin/providers/${provider.id}/models?limit=100&page=${page}`);
    entries.push(...result.items);
    if (entries.length >= result.total) return entries;
    assert.ok(result.items.length, "Catalog pagination stopped early");
  }
};
try {
  const providers = (await call("/admin/providers")).filter(p => p.protocol !== "media-upload");
  const existing = await call("/admin/models");
  const reserved = new Map(existing.map(m => [m.id, `${m.provider_id}/${m.upstream_model}`]));
  const plan = [];
  const summaries = [];
  for (const provider of providers) {
    const entries = await entriesFor(provider);
    const drafts = entries.filter(entry => !entry.publishedModels.length);
    if (drafts.length) assert.ok(provider.enabled, `${provider.name} is disabled`);
    for (const entry of drafts) {
      assert.ok(["image", "video"].includes(entry.type), `Missing type for ${entry.name}`);
      const binding = `${provider.id}/${entry.upstreamModel}`;
      const slug = entry.upstreamModel.replace(/[^a-zA-Z0-9_.:-]/g, "-") || "model";
      let modelId = (provider.id.slice(0, 8) + ":" + slug).slice(0, 100);
      if (reserved.has(modelId) && reserved.get(modelId) !== binding) {
        modelId = modelId.slice(0, 91) + ":" + createHash("sha256").update(entry.id).digest("hex").slice(0, 8);
      }
      assert.ok(!reserved.has(modelId) || reserved.get(modelId) === binding, "Model ID collision");
      reserved.set(modelId, binding);
      const prefix = Array.from(provider.name.trim())[0]?.toLowerCase() || "p";
      const name = Array.from(`${prefix}-${entry.name.trim()}`).slice(0, 120).join("");
      plan.push({ providerId: provider.id, providerName: provider.name, entry, body: { modelId, name, type: entry.type, category: "默认分组" } });
    }
    summaries.push({ provider: provider.name, total: entries.length, pending: drafts.length, alreadyPublished: entries.length - drafts.length });
  }
  console.log(JSON.stringify({ apply, pending: plan.length, providers: summaries }));
  if (apply) {
    const directory = "data/maintenance";
    await mkdir(directory, { recursive: true });
    const reportPath = `${directory}/catalog-publication-${new Date().toISOString().replace(/[:.]/g, "-")}.json`;
    const report = { startedAt: new Date().toISOString(), existingModels: existing, plan, results: [], complete: false };
    const save = () => writeFile(reportPath, JSON.stringify(report, null, 2), { mode: 0o600 });
    await save();
    for (const item of plan) {
      const result = await call(`/admin/providers/${item.providerId}/models/${item.entry.id}/publish`, "POST", item.body);
      assert.equal(result.model.provider_id, item.providerId);
      assert.equal(result.model.upstream_model, item.entry.upstreamModel);
      if (!result.alreadyPublished) {
        assert.equal(result.model.name, item.body.name);
        assert.equal(result.model.id, item.body.modelId);
        assert.equal(result.model.enabled, false);
        assert.deepEqual(result.model.prices, []);
        assert.deepEqual(result.model.parameters, item.entry.parameters);
      }
      report.results.push({ provider: item.providerName, modelId: result.model.id, alias: result.model.name, alreadyPublished: result.alreadyPublished });
      await save();
    }
    for (const provider of providers) {
      const entries = await entriesFor(provider);
      assert.ok(entries.every(entry => entry.publishedModels.length > 0), `${provider.name} still has unpublished models`);
    }
    const after = await call("/admin/models");
    for (const before of existing) assert.deepEqual(after.find(model => model.id === before.id), before, "An existing model was modified");
    report.complete = true;
    report.finishedAt = new Date().toISOString();
    report.totalModels = after.length;
    await save();
    console.log(JSON.stringify({ published: report.results.filter(r => !r.alreadyPublished).length, skippedPublished: report.results.filter(r => r.alreadyPublished).length, totalModels: after.length, allCatalogEntriesPublished: true, reportPath }));
  }
} finally {
  await call("/auth/logout", "POST").catch(() => {});
}
