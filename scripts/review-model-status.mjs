import assert from "node:assert/strict";
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";

const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const output = "test-results/model-status";
await mkdir(output, { recursive: true });
const browser = await launchBrowser();
const checks = [], errors = [];
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: "reduce" });
  const login = await context.request.post(base + "/api/auth/login", { data: JSON.parse(await readFile(".local-credentials.json", "utf8")) });
  assert.ok(login.ok());
  const get = async path => { const response = await context.request.get(base + "/api" + path); assert.ok(response.ok()); return response.json(); };
  const models = await get("/models"), before = await get("/admin/models"), jobsBefore = await get("/admin/jobs");
  const liveStatus = await get("/models/status");
  assert.deepEqual(Object.keys(liveStatus.models).sort(), models.map(model => model.id).sort());
  const model = models[0];
  assert.ok(model, "at least one enabled model is needed for the read-only visual review");
  const page = await context.newPage();
  page.on("pageerror", error => errors.push(error.message));
  page.setDefaultTimeout(10000);
  await page.route("**/api/admin/**", route => {
    if (route.request().method() !== "GET") { errors.push("unexpected write"); return route.abort(); }
    return route.continue();
  });
  await page.goto(base + "/models", { waitUntil: "networkidle" });
  const card = page.locator(".catalog-card").filter({ has: page.getByRole("heading", { name: model.name, exact: true }) });
  const health = card.getByRole("region", { name: "模型调用状态", exact: true });
  await health.waitFor();
  await card.screenshot({ path: `${output}/live-card.png`, animations: "disabled" });
  checks.push("live endpoint returns anonymous statistics only for visible models");

  // These responses are browser-only fixtures. No tasks or test records are
  // inserted into the user's database and no supplier requests are made.
  let sample = { completed24h: 25, succeeded24h: 24, failed24h: 1, successRate24h: 96, minSeconds24h: 283, maxSeconds24h: 812, recent: [...Array(9).fill("succeeded"), "failed"] };
  let fail = false, requests = 0;
  await page.route("**/api/models/status", route => {
    requests++;
    return route.fulfill({ status: fail ? 503 : 200, contentType: "application/json", body: JSON.stringify(fail ? { message: "statistics unavailable" } : { models: { [model.id]: sample }, windowHours: 24, updatedAt: new Date().toISOString() }) });
  });
  for (const theme of ["dark", "light"]) {
    await page.evaluate(value => localStorage.setItem("pf-cinematic-theme-v3", value), theme);
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.goto(base + "/models", { waitUntil: "networkidle" });
      await health.locator(".model-status-rate strong").getByText("96%", { exact: true }).waitFor();
      assert.equal(await health.locator(".model-status-bars .succeeded").count(), 9);
      assert.equal(await health.locator(".model-status-bars .failed").count(), 1);
      assert.equal(await health.locator(".model-status-bars i").last().getAttribute("title"), "第 10 次：失败");
      await health.getByText("283–812 秒", { exact: true }).waitFor();
      assert.equal(await page.evaluate(() => document.body.scrollWidth > innerWidth), false);
      assert.equal(await health.evaluate(node => node.scrollWidth > node.clientWidth), false);
      await card.screenshot({ path: `${output}/fixture-card-${theme}-${width}.png`, animations: "disabled" });
      await health.screenshot({ path: `${output}/fixture-strip-${theme}-${width}.png`, animations: "disabled" });
      checks.push(`mixed outcomes fit ${theme} ${width}px`);
    }
  }
  sample = { completed24h: 0, succeeded24h: 0, failed24h: 0, successRate24h: null, minSeconds24h: null, maxSeconds24h: null, recent: [] };
  await page.reload({ waitUntil: "networkidle" });
  await health.getByText("暂无调用", { exact: true }).waitFor();
  assert.equal(await health.locator(".model-status-bars .empty").count(), 10);
  assert.ok(!(await health.innerText()).includes("0%"));
  checks.push("empty statistics do not imply 0% success or invent completed tasks");
  sample = { ...sample, completed24h: 2, failed24h: 2, successRate24h: 0, recent: ["failed", "failed"] };
  await page.reload({ waitUntil: "networkidle" });
  assert.equal(await health.locator(".model-status-rate strong").innerText(), "0%");
  assert.equal(await health.locator(".model-status-bars .failed").count(), 2);
  assert.equal(await health.locator(".model-status-bars .empty").count(), 8);
  await health.getByText("近2次", { exact: true }).waitFor();
  checks.push("all failures retain 0% with no successful-duration estimate and partial sample size is explicit");
  fail = true;
  await page.reload({ waitUntil: "networkidle" });
  await health.getByText("暂不可用", { exact: true }).waitFor();
  await card.getByRole("link", { name: /使用这个模型/ }).waitFor();
  checks.push("statistics failures leave the model card usable and are distinct from empty data");
  await page.clock.install();
  // Install the fake clock before React registers its polling interval.
  await page.reload({ waitUntil: "networkidle" });
  await health.getByText("暂不可用", { exact: true }).waitFor();
  const countBefore = requests;
  fail = false;
  sample = { ...sample, completed24h: 2, succeeded24h: 2, failed24h: 0, successRate24h: 100, minSeconds24h: 1, maxSeconds24h: 2, recent: ["succeeded", "succeeded"] };
  await page.clock.fastForward(31000);
  await health.locator(".model-status-rate strong").getByText("100%", { exact: true }).waitFor();
  assert.ok(requests > countBefore);
  checks.push("30-second refresh recovers from a temporary statistics error");
  assert.deepEqual(await get("/admin/models"), before);
  assert.deepEqual(await get("/admin/jobs"), jobsBefore);
  assert.deepEqual(errors, []);
  await writeFile(output + "/report.json", JSON.stringify({ checks, errors, browserOnlyFixtures: true, supplierGenerationCalls: 0 }, null, 2));
  console.log(JSON.stringify({ checks, errors }));
} finally { await browser.close(); }
