import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const snapshot = JSON.parse(await readFile("data/migration/sqlite-export.json", "utf8"));
const credentials = JSON.parse(await readFile(".local-credentials.json", "utf8"));
const response = await fetch(base + "/api/auth/login", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(credentials) });
assert.equal(response.status, 200, "The existing administrator must still be able to sign in");
const cookie = response.headers.get("set-cookie").split(";")[0];
const get = async path => {
  const r = await fetch(base + "/api" + path, { headers: { Cookie: cookie } });
  assert.equal(r.status, 200, path);
  return r.json();
};
const [health, users, models, prices, settings, transactions] = await Promise.all([get("/health"), get("/admin/users"), get("/admin/models"), get("/admin/prices"), get("/admin/settings"), get("/admin/transactions")]);
assert.equal(health.backend, "go");
assert.equal(health.database, "mysql");
assert.equal(health.cache, "redis");
assert.equal(users.length, snapshot.tables.users.length);
assert.equal(models.length, snapshot.tables.models.length);
assert.equal(prices.length, snapshot.tables.prices.length);
assert.equal(transactions.total, snapshot.tables.transactions.length);
for (const old of snapshot.tables.users) {
  const user = users.find(u => u.id === old.id);
  assert.ok(user);
  assert.equal(user.username, old.username);
  assert.equal(user.balance, old.balance / 100);
  assert.equal(user.role, old.role);
  assert.equal(user.status, old.status);
}
for (const old of snapshot.tables.models) {
  const model = models.find(m => m.id === old.id);
  assert.ok(model);
  for (const key of ["name", "type", "provider_id", "upstream_model", "category", "description"]) assert.equal(model[key], old[key]);
  assert.deepEqual(model.parameters, JSON.parse(old.parameters));
  assert.deepEqual(model.tags, JSON.parse(old.tags));
}
for (const old of snapshot.tables.prices) {
  const price = prices.find(p => p.id === old.id);
  assert.ok(price);
  assert.equal(price.credits, old.credits / 100);
  assert.equal(price.unit, old.unit);
  assert.equal(price.enabled, Boolean(old.enabled));
  assert.deepEqual(price.conditions, JSON.parse(old.conditions));
}
for (const old of snapshot.tables.settings) assert.equal(settings[old.key], old.value);
const report = { checkedAt: new Date().toISOString(), health, users: users.length, models: models.length, priceRules: prices.length, transactions: transactions.total, existingAdminLogin: true, balancesAndModelConfigurationPreserved: true };
await writeFile("data/migration/verification.json", JSON.stringify(report, null, 2));
console.log(JSON.stringify(report));
