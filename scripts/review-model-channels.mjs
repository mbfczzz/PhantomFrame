import assert from "node:assert/strict";
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";

const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const output = "test-results/model-channels";
await mkdir(output, { recursive: true });
const browser = await launchBrowser();
const checks = [], errors = [];
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: "reduce" });
  assert.ok((await context.request.post(base + "/api/auth/login", { data: JSON.parse(await readFile(".local-credentials.json", "utf8")) })).ok());
  const get = async path => {
    const response = await context.request.get(base + "/api" + path);
    assert.ok(response.ok());
    return response.json();
  };
  const models = await get("/admin/models"), providers = await get("/admin/providers");
  const channelIds = new Set(models.map(model => model.provider_id));
  const provider = providers.find(item => item.config?.vendor === "maitoken") || providers.find(item => channelIds.has(item.id));
  const catalogBefore = await get(`/admin/providers/${provider.id}/models`);
  const entry = catalogBefore.items[0];
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  page.on("pageerror", error => errors.push(error.message));
  await page.route("**/api/admin/**", route => {
    if (route.request().method() !== "GET") { errors.push("Unexpected admin mutation"); return route.abort(); }
    return route.continue();
  });
  for (const theme of ["dark", "light"]) {
    await page.goto(base + "/admin/models", { waitUntil: "networkidle" });
    await page.evaluate(value => localStorage.setItem("pf-cinematic-theme-v3", value), theme);
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: width === 320 ? 568 : 1000 });
      await page.goto(base + "/admin/models", { waitUntil: "networkidle" });
      assert.equal(await page.locator(".model-channel-group").count(), channelIds.size);
      assert.equal(await page.locator(".model-channel-table").count(), 0);
      assert.equal(await page.evaluate(() => document.body.scrollWidth > innerWidth), false);
      await page.screenshot({ path: `${output}/groups-${theme}-${width}.png`, fullPage: true, animations: "disabled" });
      const group = page.locator(".model-channel-group").filter({ has: page.locator(".model-channel-title strong", { hasText: provider.name }) });
      await group.locator(".model-channel-toggle").click();
      assert.equal(await group.locator("tbody tr").count(), models.filter(model => model.provider_id === provider.id).length);
      assert.equal(await page.evaluate(() => document.body.scrollWidth > innerWidth), false);
      await group.screenshot({ path: `${output}/expanded-${theme}-${width}.png`, animations: "disabled" });
      await group.getByRole("button", { name: "配置模型密钥 " + provider.name, exact: true }).click();
      const catalog = page.getByRole("dialog", { name: "渠道模型", exact: true });
      const keyButton = catalog.getByRole("button", { name: "配置调用密钥 " + entry.name, exact: true });
      await keyButton.waitFor();
      assert.equal(await catalog.evaluate(node => node.scrollWidth > node.clientWidth), false);
      await page.screenshot({ path: `${output}/catalog-${theme}-${width}.png`, animations: "disabled" });
      await keyButton.click();
      const dialog = page.getByRole("dialog", { name: "模型调用密钥", exact: true });
      await dialog.getByRole("radio", { name: /使用模型独立 Key/ }).check();
      assert.equal(await dialog.getByLabel("模型独立 API Key", { exact: true }).inputValue(), "");
      assert.equal(await dialog.getByRole("button", { name: "保存密钥", exact: true }).isDisabled(), true);
      assert.equal(await dialog.evaluate(node => node.scrollWidth > node.clientWidth), false);
      const bounds = await dialog.boundingBox();
      assert.ok(bounds.y >= 0 && bounds.y + bounds.height <= (width === 320 ? 568 : 1000) + 1);
      await page.screenshot({ path: `${output}/key-${theme}-${width}.png`, animations: "disabled" });
      await page.keyboard.press("Escape");
      await dialog.waitFor({ state: "hidden" });
      await catalog.waitFor();
      await page.keyboard.press("Escape");
      await catalog.waitFor({ state: "hidden" });
      const search = page.getByPlaceholder("搜索渠道、模型名称或 ID", { exact: true });
      await search.fill(provider.name);
      assert.equal(await page.locator(".model-channel-group").count(), 1);
      assert.equal(await page.locator(".model-channel-toggle").getAttribute("aria-expanded"), "true");
      checks.push({ theme, width, grouped: true, keyDialog: true });
    }
  }
  assert.deepEqual(await get("/admin/models"), models);
  assert.deepEqual(await get("/admin/providers"), providers);
  assert.deepEqual(await get(`/admin/providers/${provider.id}/models`), catalogBefore);
  assert.deepEqual(errors, []);
  await writeFile(output + "/report.json", JSON.stringify({ checks, errors }, null, 2));
  console.log(JSON.stringify({ checks, errors }));
} finally { await browser.close(); }
