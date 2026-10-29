import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { launchBrowser } from "./browser.mjs";
import { chooseOption } from "./ui-controls.mjs";

const output = resolve("test-results/provider-instances-ui");
await mkdir(output, { recursive: true });
const database = "phantomframe_test_instances_ui_" + Date.now();
const dir = await mkdtemp(join(output, "data-"));
const base = "http://127.0.0.1:4192",
  upstream = "http://127.0.0.1:4191";
let app,
  browser,
  serverOutput = "";
const errors = [],
  checks = [],
  requests = [];
const check = (message) => {
  checks.push(message);
  console.log("PASS", message);
};
const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const mock = createServer(async (req, res) => {
  requests.push({
    path: req.url,
    method: req.method,
    key: req.headers.authorization,
  });
  res.setHeader("Content-Type", "application/json");
  if (req.url === "/v1/models" && req.method === "GET")
    return res.end(
      JSON.stringify({
        data: [
          { id: "gpt-5.4-mini", type: "text" },
          { id: "ignored-3d", type: "3d" },
        ],
      }),
    );
  res.statusCode = 404;
  res.end(JSON.stringify({ message: "Unexpected mock request" }));
});
async function api(ctx, path, method = "GET", body) {
  const response = await ctx.request.fetch(base + "/api" + path, {
    method,
    data: body,
    headers: { "Idempotency-Key": crypto.randomUUID() },
  });
  const data = await response.json();
  assert.ok(
    response.ok(),
    `${method} ${path}: ${response.status()} ${JSON.stringify(data)}`,
  );
  return data;
}
async function start() {
  app = spawn(
    resolve(`bin/phantomframe${process.platform === "win32" ? ".exe" : ""}`),
    [],
    {
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
      env: {
        ...process.env,
        APP_ENV: "test",
        HOST: "127.0.0.1",
        PORT: "4192",
        APP_URL: base,
        MYSQL_DATABASE: database,
        REDIS_PREFIX: database + ":",
        DATA_DIR: dir,
        STORAGE_DRIVER: "local",
        DEMO_MODE: "false",
        COOKIE_SECURE: "false",
        ADMIN_USERNAME: "instance-ui-admin",
        ADMIN_PASSWORD: "Instance-ui-password-2026",
        ENCRYPTION_KEY: "a".repeat(64),
      },
    },
  );
  app.stdout.on("data", (d) => (serverOutput += d));
  app.stderr.on("data", (d) => (serverOutput += d));
  for (let i = 0; i < 150; i++) {
    try {
      if ((await fetch(base + "/api/health")).ok) return;
    } catch {}
    await wait(150);
  }
  throw Error(serverOutput);
}
async function stop() {
  if (app && app.exitCode === null) {
    const exited = new Promise((resolve) => app.once("exit", resolve));
    app.kill();
    await exited;
  }
}
try {
  execFileSync("go", ["run", "./cmd/testdb", "create", database], {
    stdio: "pipe",
    windowsHide: true,
  });
  await new Promise((resolve) => mock.listen(4191, "127.0.0.1", resolve));
  await start();
  browser = await launchBrowser();
  const ctx = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await ctx.route("**/*", (route) =>
    new URL(route.request().url()).hostname === "127.0.0.1"
      ? route.continue()
      : route.abort(),
  );
  await api(ctx, "/auth/login", "POST", {
    username: "instance-ui-admin",
    password: "Instance-ui-password-2026",
  });
  const page = await ctx.newPage();
  page.setDefaultTimeout(15000);
  page.on("pageerror", (error) => errors.push(error.message));
  assert.equal((await api(ctx, "/admin/providers")).length, 0);
  assert.equal((await api(ctx, "/admin/models")).length, 0);
  await page.goto(base + "/admin/providers", { waitUntil: "networkidle" });
  await page
    .getByRole("heading", { name: "尚未添加供应商渠道", exact: true })
    .waitFor();
  check(
    "startup has no automatic supplier accounts, imported routes or models",
  );
  async function addSupplier(name, protocol, key) {
    await page.getByRole("button", { name: "新增渠道", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "新增供应商渠道",
      exact: true,
    });
    await dialog.getByLabel("接入协议", { exact: true }).selectOption(protocol);
    assert.equal(
      await dialog.locator('select option[value^="native:"]').count(),
      34,
    );
    await dialog.getByLabel("渠道名称", { exact: true }).fill(name);
    await dialog
      .getByLabel("API 基础地址", { exact: true })
      .fill(upstream + "/v1");
    await dialog.getByLabel("API Key", { exact: true }).fill(key);
    await dialog.getByRole("button", { name: "保存渠道", exact: true }).click();
    await dialog.waitFor({ state: "hidden" });
    await page.getByRole("heading", { name, exact: true }).waitFor();
    return (await api(ctx, "/admin/providers")).find((p) => p.name === name);
  }
  const first = await addSupplier(
    "APIYI 主账号",
    "native:apiyi",
    "supplier-ui-fixture-a",
  );
  const second = await addSupplier(
    "APIYI 备用账号",
    "native:apiyi",
    "supplier-ui-fixture-b",
  );
  assert.notEqual(first.id, second.id);
  const card = (name) =>
    page
      .locator(".provider-card")
      .filter({ has: page.getByRole("heading", { name, exact: true }) });
  await card(first.name)
    .getByRole("button", { name: "配置 API Key", exact: true })
    .click();
  let edit = page.getByRole("dialog", { name: "编辑供应商渠道", exact: true });
  assert.equal(
    await edit.getByLabel("API Key", { exact: true }).inputValue(),
    "",
  );
  await edit.getByRole("button", { name: "显示密钥", exact: true }).click();
  await edit.getByRole("button", { name: "隐藏密钥", exact: true }).waitFor();
  assert.equal(
    await edit.getByLabel("API Key", { exact: true }).inputValue(),
    "supplier-ui-fixture-a",
  );
  await edit.getByRole("button", { name: "隐藏密钥", exact: true }).click();
  await edit.getByRole("button", { name: "取消", exact: true }).click();
  await edit.waitFor({ state: "hidden" });
  check(
    "same protocol creates independent accounts and the outer key editor reveals only the selected account",
  );
  for (const p of [first, second]) {
    await card(p.name)
      .getByRole("button", { name: "管理模型 " + p.name, exact: true })
      .click();
    const catalog = page.getByRole("dialog", { name: "渠道模型", exact: true });
    await catalog
      .getByRole("button", { name: "拉取模型列表", exact: true })
      .click();
    await catalog
      .getByRole("button", { name: "发布 gpt-5.4-mini", exact: true })
      .waitFor();
    const entries = await api(ctx, "/admin/providers/" + p.id + "/models");
    assert.equal(entries.total, 1);
    if (p.id === first.id) {
      await catalog
        .getByRole("button", { name: "发布 gpt-5.4-mini", exact: true })
        .click();
      const publish = page.getByRole("dialog", {
        name: "发布到模型管理",
        exact: true,
      });
      await publish
        .getByLabel("站内模型 ID", { exact: true })
        .fill("new-instance-text");
      await publish
        .getByRole("button", { name: "确认发布", exact: true })
        .click();
      await publish.waitFor({ state: "hidden" });
    }
    await catalog
      .getByRole("button", { name: "关闭列表", exact: true })
      .click();
    await catalog.waitFor({ state: "hidden" });
  }
  const published = (await api(ctx, "/admin/models")).find(
    (m) => m.id === "new-instance-text",
  );
  assert.equal(published.enabled, false);
  assert.equal(published.prices.length, 0);
  assert.equal(
    requests.filter(
      (r) =>
        r.key === "Bearer supplier-ui-fixture-a" && r.path === "/v1/models",
    ).length,
    1,
  );
  assert.equal(
    requests.filter(
      (r) =>
        r.key === "Bearer supplier-ui-fixture-b" && r.path === "/v1/models",
    ).length,
    1,
  );
  check(
    "model pulls use each account key, omit 3D and publish disabled models without invented prices",
  );
  await api(ctx, "/admin/prices", "POST", {
    model_id: published.id,
    label: "review fixture",
    unit: "request",
    credits: 1,
    conditions: {},
  });
  await api(ctx, "/admin/models/" + published.id, "PUT", {
    ...published,
    enabled: true,
  });
  await card(first.name).getByTitle("编辑渠道", { exact: true }).click();
  const reviewEditor = page.getByRole("dialog", {
    name: "编辑供应商渠道",
    exact: true,
  });
  await reviewEditor
    .getByLabel("API 基础地址", { exact: true })
    .fill(upstream + "/alternate");
  await reviewEditor
    .getByRole("button", { name: "保存渠道", exact: true })
    .click();
  await reviewEditor.waitFor({ state: "hidden" });
  const afterAddressChange = (await api(ctx, "/admin/models")).find(
    (m) => m.id === published.id,
  );
  assert.equal(afterAddressChange.enabled, false);
  assert.equal(afterAddressChange.prices.length, 1);
  assert.equal(afterAddressChange.prices[0].credits, 1);
  check(
    "changing the supplier generation address disables reviewed models while preserving retail prices",
  );
  const manual = await addSupplier(
    "ALI 手动账号",
    "native:ali",
    "supplier-ui-manual-key",
  );
  await card(manual.name)
    .getByRole("button", { name: "管理模型 " + manual.name, exact: true })
    .click();
  const catalog = page.getByRole("dialog", { name: "渠道模型", exact: true });
  assert.equal(
    await catalog
      .getByRole("button", { name: "拉取模型列表", exact: true })
      .isDisabled(),
    true,
  );
  await catalog
    .getByRole("button", { name: "添加支持模型", exact: true })
    .click();
  const manualDialog = page.getByRole("dialog", {
    name: "添加支持模型",
    exact: true,
  });
  await manualDialog
    .getByLabel("上游模型 ID", { exact: true })
    .fill("qwen-image-2.1-pro");
  await manualDialog
    .getByLabel("模型名称", { exact: true })
    .fill("手动图片模型");
  await page.waitForFunction(() => !document.querySelector(".toast"));
  await page.screenshot({
    path: join(output, "manual-model-desktop.png"),
    animations: "disabled",
  });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await page.screenshot({
    path: join(output, "manual-model-mobile.png"),
    animations: "disabled",
  });
  await manualDialog
    .getByRole("button", { name: "保存渠道模型", exact: true })
    .click();
  await manualDialog.waitFor({ state: "hidden" });
  const entries = await api(ctx, "/admin/providers/" + manual.id + "/models");
  assert.equal(entries.total, 1);
  assert.ok(entries.items[0].parameters.length > 0);
  await catalog.getByRole("button", { name: "关闭列表", exact: true }).click();
  await catalog.waitFor({ state: "hidden" });
  check(
    "suppliers without a directory support manual model IDs and adapter-generated parameter schemas",
  );
  const videoAccount = await addSupplier(
    "兼容视频测试账号",
    "openai-video",
    "video-account-fixture-key",
  );
  await card(videoAccount.name)
    .getByRole("button", { name: "管理模型 " + videoAccount.name, exact: true })
    .click();
  const videoCatalog = page.getByRole("dialog", {
    name: "渠道模型",
    exact: true,
  });
  await videoCatalog
    .getByRole("button", { name: "添加支持模型", exact: true })
    .click();
  const videoForm = page.getByRole("dialog", {
    name: "添加支持模型",
    exact: true,
  });
  assert.equal(
    await videoForm.getByLabel("生成类型", { exact: true }).inputValue(),
    "video",
  );
  assert.deepEqual(
    await videoForm
      .getByLabel("生成类型", { exact: true })
      .locator("option")
      .evaluateAll((options) => options.map((o) => o.value)),
    ["image", "video"],
  );
  await videoForm.getByRole("button", { name: "取消", exact: true }).click();
  await videoForm.waitFor({ state: "hidden" });
  await videoCatalog
    .getByRole("button", { name: "关闭列表", exact: true })
    .click();
  await videoCatalog.waitFor({ state: "hidden" });
  // Reproduce the real catalog with mixed Chinese/Latin names and four rows.
  for (const name of [
    "MiniMax-H3",
    "MINIMAX-H3-2.0采样-933",
    "MINIMAX-H3-2.0采样",
    "SD2.5-30-10-10",
  ]) {
    await api(ctx, "/admin/providers/" + videoAccount.id + "/models", "POST", {
      upstreamModel: name,
      name,
      type: "video",
      parameters: [],
    });
  }
  await page.reload({ waitUntil: "networkidle" });
  await card(videoAccount.name)
    .getByRole("button", { name: "管理模型 " + videoAccount.name, exact: true })
    .click();
  await videoCatalog.locator("tbody tr").nth(3).waitFor();
  const layoutFailures = [];
  const layouts = [];
  for (const [width, height, theme = "dark"] of [
    [1440, 900],
    [892, 810],
    [768, 600],
    [390, 844],
    [844, 390],
    [320, 568],
    [892, 810, "light"],
  ]) {
    await page.setViewportSize({ width, height });
    await page.evaluate((theme) => {
      document.documentElement.dataset.theme = theme;
    }, theme);
    await videoCatalog.getByPlaceholder("搜索上游模型 ID 或名称").focus();
    const result = await videoCatalog.evaluate((dialog) => {
      const rect = (node) => {
        const { x, y, width, height, bottom, right } =
          node.getBoundingClientRect();
        return { x, y, width, height, bottom, right };
      };
      const search = dialog.querySelector(".searchbox");
      const input = search.querySelector("input");
      return {
        dialog: rect(dialog),
        header: rect(dialog.querySelector("header")),
        footer: rect(dialog.querySelector(".modal-footer")),
        results: rect(dialog.querySelector(".channel-model-results")),
        search: rect(search),
        input: rect(input),
        inputShadow: getComputedStyle(input).boxShadow,
        clippedButtons: [...dialog.querySelectorAll(".row-actions > button")]
          .filter((b) => b.scrollWidth > b.clientWidth + 1)
          .map((b) => b.getAttribute("aria-label")),
        documentOverflow: document.documentElement.scrollWidth > innerWidth,
      };
    });
    layouts.push({ width, height, theme, ...result });
    if (
      result.dialog.y < 0 ||
      result.dialog.bottom > height + 1 ||
      result.header.y < 0 ||
      result.footer.bottom > height + 1
    )
      layoutFailures.push(
        `${width}x${height}: dialog controls outside viewport`,
      );
    if (result.results.height < 80)
      layoutFailures.push(`${width}x${height}: model list collapsed`);
    if (
      result.input.right > result.search.right ||
      result.inputShadow !== "none"
    )
      layoutFailures.push(`${width}x${height}: nested search outline/overflow`);
    if (result.clippedButtons.length)
      layoutFailures.push(
        `${width}x${height}: clipped action text ${result.clippedButtons.join(",")}`,
      );
    if (result.documentOverflow)
      layoutFailures.push(`${width}x${height}: page overflow`);
    await page.screenshot({
      path: join(output, `catalog-layout-${width}x${height}-${theme}.png`),
      animations: "disabled",
    });
  }
  await writeFile(
    join(output, "catalog-layout.json"),
    JSON.stringify({ layouts, layoutFailures }, null, 2),
  );
  assert.deepEqual(layoutFailures, []);
  await page.setViewportSize({ width: 320, height: 568 });
  await videoCatalog
    .getByRole("button", { name: "发布 MiniMax-H3", exact: true })
    .click();
  const nestedPublish = page.getByRole("dialog", {
    name: "发布到模型管理",
    exact: true,
  });
  await nestedPublish.waitFor();
  const publishBounds = await nestedPublish.boundingBox();
  assert.ok(
    publishBounds.y >= 0 && publishBounds.y + publishBounds.height <= 568,
  );
  assert.equal(
    await page.locator(".channel-model-dialog").getAttribute("aria-hidden"),
    "true",
  );
  await page.keyboard.press("Escape");
  await nestedPublish.waitFor({ state: "hidden" });
  assert.equal(await videoCatalog.getAttribute("aria-modal"), "true");
  await videoCatalog
    .getByRole("button", { name: "关闭列表", exact: true })
    .click();
  await videoCatalog.waitFor({ state: "hidden" });
  await page.evaluate(() => {
    document.documentElement.dataset.theme = "dark";
  });
  await page.setViewportSize({ width: 390, height: 844 });
  check(
    "catalog controls fit desktop, mobile and landscape viewports without clipped search or publish buttons; nested dialogs restore the parent",
  );
  await api(ctx, "/admin/providers/" + videoAccount.id, "DELETE");
  await page.reload({ waitUntil: "networkidle" });
  check(
    "video accounts default to video and only show generation types supported by the protocol",
  );
  await page.waitForFunction(() => !document.querySelector(".toast"));
  await page.screenshot({
    path: join(output, "supplier-instances-mobile.png"),
    animations: "disabled",
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.screenshot({
    path: join(output, "supplier-instances-desktop.png"),
    animations: "disabled",
  });
  await stop();
  await start();
  assert.equal((await api(ctx, "/admin/providers")).length, 3);
  assert.equal((await api(ctx, "/admin/models")).length, 1);
  const restartedModel = (await api(ctx, "/admin/models"))[0];
  assert.equal(restartedModel.enabled, false);
  assert.equal(restartedModel.prices[0].credits, 1);
  assert.equal(
    (await api(ctx, "/admin/providers/" + manual.id + "/models")).total,
    1,
  );
  assert.equal(requests.length, 2);
  check(
    "real process restart preserves newly configured accounts and models; no generation or payment calls occur",
  );
  assert.deepEqual(errors, []);
} catch (error) {
  console.error(error);
  console.error(serverOutput);
  process.exitCode = 1;
} finally {
  await writeFile(
    join(output, "results.json"),
    JSON.stringify(
      {
        checks,
        pageErrors: errors,
        catalogRequests: requests.length,
        realModelCalls: 0,
        realPaymentCalls: 0,
      },
      null,
      2,
    ),
  );
  await browser?.close();
  await stop();
  await new Promise((resolve) => mock.close(resolve));
  execFileSync("go", ["run", "./cmd/testdb", "drop", database], {
    stdio: "pipe",
    windowsHide: true,
  });
}
