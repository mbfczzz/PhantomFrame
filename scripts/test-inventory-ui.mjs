import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { launchBrowser } from "./browser.mjs";
import { chooseOption } from "./ui-controls.mjs";

const output = resolve("test-results/inventory-ui");
await mkdir(output, { recursive: true });
const database = "phantomframe_test_inventory_ui_" + Date.now();
const dir = await mkdtemp(join(output, "data-"));
const base = "http://127.0.0.1:4192",
  upstream = "http://127.0.0.1:4191";
const fixture = JSON.parse(
  await readFile("internal/api/inventory_fixture_test.json", "utf8"),
);
const columns = Object.keys(fixture[0]);
const cell = (value) => '"' + String(value ?? "").replaceAll('"', '""') + '"';
const csv =
  columns.map(cell).join(",") +
  "\n" +
  fixture
    .map((row) => columns.map((key) => cell(row[key])).join(","))
    .join("\n");
const video = await readFile("server/demo-video.webm");
const wav = Buffer.alloc(204);
wav.write("RIFF", 0);
wav.writeUInt32LE(196, 4);
wav.write("WAVEfmt ", 8);
wav.writeUInt32LE(16, 16);
wav.writeUInt16LE(1, 20);
wav.writeUInt16LE(1, 22);
wav.writeUInt32LE(8000, 24);
wav.writeUInt32LE(16000, 28);
wav.writeUInt16LE(2, 32);
wav.writeUInt16LE(16, 34);
wav.write("data", 36);
wav.writeUInt32LE(160, 40);
let app,
  browser,
  serverOutput = "";
const errors = [],
  checks = [],
  requests = [];
const mock = createServer(async (req, res) => {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  requests.push({ method: req.method, path: req.url });
  res.setHeader("Content-Type", "application/json");
  if (req.url === "/files")
    return res.end(
      JSON.stringify({ url: "https://reference.invalid/input.webm" }),
    );
  if (req.url === "/result") {
    res.setHeader("Content-Type", "video/webm");
    return res.end(video);
  }
  if (req.url === "/v1/responses")
    return res.end(
      JSON.stringify({
        output_text:
          "本地模拟的文本结果。<script>window.unwantedExecution=true</script>",
      }),
    );
  if (req.url === "/v1/audio/speech") {
    res.setHeader("Content-Type", "audio/wav");
    return res.end(wav);
  }
  if (req.url === "/api/v3/wavespeed-ai/video-upscaler")
    return res.end(
      JSON.stringify({ status: "completed", output: upstream + "/result" }),
    );
  res.statusCode = 404;
  res.end(JSON.stringify({ message: "unexpected mock request" }));
});
const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function api(ctx, path, method = "GET", body) {
  const r = await ctx.request.fetch(base + "/api" + path, {
    method,
    data: body,
    headers: { "Idempotency-Key": crypto.randomUUID() },
  });
  const data = await r.json();
  assert.ok(r.ok(), `${method} ${path}: ${r.status()} ${JSON.stringify(data)}`);
  return data;
}
const check = (name) => {
  checks.push(name);
  console.log("PASS", name);
};
try {
  execFileSync("go", ["run", "./cmd/testdb", "create", database], {
    stdio: "pipe",
    windowsHide: true,
  });
  await new Promise((resolve) => mock.listen(4191, "127.0.0.1", resolve));
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
        DEMO_MODE: "true",
        COOKIE_SECURE: "false",
        ADMIN_USERNAME: "inventory-ui-admin",
        ADMIN_PASSWORD: "Inventory-ui-password-2026",
        ENCRYPTION_KEY: "a".repeat(64),
      },
    },
  );
  app.stdout.on("data", (d) => (serverOutput += d));
  app.stderr.on("data", (d) => (serverOutput += d));
  let ready = false;
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(base + "/api/health")).ok) {
        ready = true;
        break;
      }
    } catch {}
    await wait(150);
  }
  assert.ok(ready, serverOutput);
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
    username: "inventory-ui-admin",
    password: "Inventory-ui-password-2026",
  });
  const page = await ctx.newPage();
  page.setDefaultTimeout(15000);
  page.on("pageerror", (e) => errors.push(e.message));
  const startupRoutes = await api(ctx, "/admin/provider-inventory");
  assert.equal(startupRoutes.length, 249);
  assert.equal(new Set(startupRoutes.map((r) => r.vendor)).size, 34);
  assert.ok(
    startupRoutes.every((r) => !r.hasKey && !r.enabled && r.type !== "3d"),
  );
  assert.equal(requests.length, 0);
  await page.goto(base + "/admin/providers", { waitUntil: "networkidle" });
  await page.getByRole("button", { name: "新增渠道", exact: true }).click();
  let presetDialog = page.getByRole("dialog", {
    name: "新增供应商渠道",
    exact: true,
  });
  const supplierOptions = await presetDialog
    .locator('optgroup[label="清单供应商 · 启动时已预置"] option')
    .allTextContents();
  assert.equal(supplierOptions.length, 34);
  for (const vendor of new Set(startupRoutes.map((r) => r.vendor)))
    assert.ok(
      supplierOptions.some((text) => text.includes(vendor + " 专用适配")),
    );
  const apiYiId = startupRoutes.find((r) => r.vendor === "APIYI").providerId;
  await presetDialog
    .getByLabel("接入协议", { exact: true })
    .selectOption("preset:" + apiYiId);
  assert.equal(
    await presetDialog.getByLabel("渠道名称", { exact: true }).inputValue(),
    "APIYI",
  );
  await page.screenshot({
    path: join(output, "supplier-preset-desktop.png"),
    fullPage: false,
    animations: "disabled",
  });
  await presetDialog
    .getByRole("link", { name: "配置 APIYI 的 API Key", exact: true })
    .click();
  await page.waitForURL(
    base + "/admin/inventory?provider=" + encodeURIComponent(apiYiId),
  );
  await page
    .getByRole("button", { name: "路由配置", exact: true })
    .first()
    .click();
  let routeDialog = page.getByRole("dialog", {
    name: "路由配置 · APIYI",
    exact: true,
  });
  assert.ok(
    (
      await routeDialog.getByLabel("API 地址", { exact: true }).inputValue()
    ).startsWith("http"),
  );
  assert.equal(
    await routeDialog.getByLabel("API Key", { exact: true }).inputValue(),
    "",
  );
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await page.screenshot({
    path: join(output, "supplier-route-mobile.png"),
    fullPage: false,
    animations: "disabled",
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await routeDialog
    .getByLabel("API Key", { exact: true })
    .fill("single-route-preset-test-key");
  await routeDialog
    .getByRole("button", { name: "保存路由", exact: true })
    .click();
  await routeDialog.waitFor({ state: "hidden" });
  const keyedRoutes = await api(ctx, "/admin/provider-inventory");
  assert.equal(keyedRoutes.filter((r) => r.hasKey).length, 1);
  assert.ok(keyedRoutes.every((r) => !r.enabled));
  assert.equal(requests.length, 0);
  check(
    "startup prepares all 34 suppliers and 249 keyless routes; every supplier is selectable and configuring one route never changes another key",
  );
  await page.getByRole("link", { name: "全部供应商", exact: true }).click();
  await page.waitForURL(base + "/admin/inventory");
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  await page.goBack();
  await page.waitForURL(base + "/admin/inventory?provider=" + apiYiId);
  assert.equal(
    await page.getByRole("button", { name: "路由配置", exact: true }).count(),
    startupRoutes.filter((r) => r.providerId === apiYiId).length,
    "returning to a supplier must reset pagination",
  );
  await page
    .getByRole("textbox", { name: "搜索供应商、模型、路由或待配置状态" })
    .fill("no-match-fixture");
  await page.getByRole("link", { name: "全部供应商", exact: true }).click();
  await page.waitForURL(base + "/admin/inventory");
  await page
    .getByRole("link", { name: "全部供应商", exact: true })
    .waitFor({ state: "hidden" });
  assert.equal(
    await page
      .getByRole("textbox", { name: "搜索供应商、模型、路由或待配置状态" })
      .inputValue(),
    "",
  );
  assert.equal(
    await page.getByRole("button", { name: "路由配置", exact: true }).count(),
    24,
  );
  check("supplier navigation resets stale search and pagination");
  await page.goto(base + "/admin/inventory", { waitUntil: "networkidle" });
  await page
    .getByRole("textbox", { name: "搜索供应商、模型、路由或待配置状态" })
    .fill("待补地址");
  assert.equal(
    await page.getByRole("button", { name: "路由配置", exact: true }).count(),
    6,
  );
  await page
    .getByRole("textbox", { name: "搜索供应商、模型、路由或待配置状态" })
    .fill("");
  await page.locator('input[type="file"]').setInputFiles({
    name: "synthetic-provider-routes.csv",
    mimeType: "text/csv",
    buffer: Buffer.from(csv),
  });
  const preview = page.getByRole("dialog", {
    name: "导入供应商路由",
    exact: true,
  });
  await preview.waitFor();
  assert.match(await preview.innerText(), /249/);
  assert.match(await preview.innerText(), /34/);
  assert.match(await preview.innerText(), /92/);
  assert.equal((await api(ctx, "/admin/provider-inventory")).length, 249);
  assert.equal(
    (await api(ctx, "/admin/provider-inventory")).filter((r) => r.hasKey)
      .length,
    1,
  );
  await preview.getByRole("button", { name: "确认导入", exact: true }).click();
  await preview.waitFor({ state: "hidden" });
  const routes = await api(ctx, "/admin/provider-inventory");
  assert.equal(routes.length, 249);
  assert.equal(new Set(routes.map((r) => r.vendor)).size, 34);
  assert.ok(routes.every((r) => r.type !== "3d" && !r.enabled));
  assert.equal(requests.length, 0);
  check(
    "CSV preview and import preserve 249 routes across 34 suppliers, exclude 3D, and make no upstream requests",
  );
  await page
    .getByRole("textbox", { name: "搜索供应商、模型、路由或待配置状态" })
    .fill("APIYI");
  await page
    .getByRole("button", { name: "路由配置", exact: true })
    .first()
    .click();
  const editor = page.getByRole("dialog", { name: /路由配置/ });
  assert.equal(
    await editor.getByLabel("API Key", { exact: true }).inputValue(),
    "",
  );
  await page.keyboard.press("Escape");
  check("route configuration never reveals imported credentials");
  const repairRoute = routes.find(
    (row) => row.vendor === "APIYI" && row.model === "gpt-5.4",
  );
  await page
    .getByRole("textbox", { name: "搜索供应商、模型、路由或待配置状态" })
    .fill(repairRoute.id);
  await page.getByRole("button", { name: "更新参数规范", exact: true }).click();
  const confirmation = page.getByRole("dialog", {
    name: "更新参数规范",
    exact: true,
  });
  await confirmation.waitFor();
  await confirmation
    .getByRole("button", { name: "更新并停用模型", exact: true })
    .click();
  await confirmation.waitFor({ state: "hidden" });
  await page
    .getByText("参数已更新，请核对价格后启用模型", { exact: true })
    .waitFor();
  const repaired = (await api(ctx, "/admin/models")).find(
    (model) => model.id === repairRoute.modelId,
  );
  assert.equal(repaired.enabled, false);
  assert.ok(
    repaired.parameters.some((field) => field.name === "maxOutputTokens"),
  );
  assert.equal(requests.length, 0);
  check(
    "parameter repair confirms its impact, disables the model and preserves independent route management without upstream requests",
  );
  const wanted = [
    { vendor: "APIYI", model: "gpt-5.4", kind: "text" },
    { vendor: "openrouter", model: "tts-gemini-openrouter", kind: "audio" },
    {
      vendor: "wavespeed",
      model: "wavespeed-ai/video-upscaler",
      kind: "upscale",
    },
  ];
  for (const target of wanted) {
    const route = routes.find(
      (row) => row.vendor === target.vendor && row.model === target.model,
    );
    assert.ok(route);
    target.id = route.modelId;
    await api(ctx, "/admin/provider-inventory/" + route.id, "PATCH", {
      baseURL: upstream,
      apiKey: "browser-local-key-" + target.kind,
      sourceEnabled: true,
    });
    await api(ctx, "/admin/prices", "POST", {
      model_id: target.id,
      label: "本地测试按次",
      unit: "request",
      credits: 0.25,
    });
    const model = (await api(ctx, "/admin/models")).find(
      (m) => m.id === target.id,
    );
    await api(ctx, "/admin/models/" + target.id, "PUT", {
      ...model,
      enabled: true,
    });
  }
  await api(ctx, "/admin/providers", "POST", {
    name: "本地素材托管模拟",
    protocol: "media-upload",
    base_url: upstream,
    apiKey: "local-upload-key",
    config: { uploadPath: "/files" },
  });
  for (const target of wanted) {
    await page.goto(base + "/generate", { waitUntil: "networkidle" });
    await chooseOption(page, "生成类型", target.kind);
    await chooseOption(page, "模型", target.id);
    await page
      .getByLabel("提示词", { exact: true })
      .fill("本地模拟验证 " + target.kind);
    if (target.kind === "upscale") {
      await page.locator('input[type="file"]').first().setInputFiles({
        name: "reference.webm",
        mimeType: "video/webm",
        buffer: video,
      });
      await page.locator(".reference-item").first().waitFor();
    }
    await page.getByRole("button", { name: "开始生成", exact: true }).click();
    await page.waitForFunction(
      (id) =>
        Array.from(document.querySelectorAll(".job-card")).some(
          (card) =>
            card.textContent.includes("本地模拟验证 " + id) &&
            card.querySelector(".badge.succeeded"),
        ),
      target.kind,
    );
    const card = page
      .locator(".job-card")
      .filter({ hasText: "本地模拟验证 " + target.kind });
    await card.getByTitle("任务详情", { exact: true }).click();
    const detail = page.getByRole("dialog", { name: "任务详情", exact: true });
    await detail.waitFor();
    if (target.kind === "text") {
      await detail.getByText(/本地模拟的文本结果/).waitFor();
      assert.equal(
        await page.evaluate(() => !!window.unwantedExecution),
        false,
      );
      assert.ok(await detail.locator("pre").count());
    }
    const outputElement = detail
      .locator(
        target.kind === "text"
          ? "pre"
          : target.kind === "audio"
            ? "audio"
            : "video",
      )
      .first();
    await outputElement.waitFor();
    await outputElement.evaluate((el) =>
      el.scrollIntoView({ behavior: "instant", block: "center" }),
    );
    await page.screenshot({
      path: join(output, target.kind + "-desktop.png"),
      fullPage: false,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await outputElement.evaluate((el) =>
      el.scrollIntoView({ behavior: "instant", block: "center" }),
    );
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
    await page.screenshot({
      path: join(output, target.kind + "-mobile.png"),
      fullPage: false,
      animations: "disabled",
    });
    await page.keyboard.press("Escape");
    await page.setViewportSize({ width: 1440, height: 1000 });
    check(
      target.kind +
        " generation uses the isolated upstream, stores a usable result and displays its detail safely",
    );
  }
  assert.equal((await api(ctx, "/me")).balance, 999.25);
  await page.goto(base + "/admin/inventory", { waitUntil: "networkidle" });
  for (const theme of ["dark", "light"]) {
    await page.evaluate((theme) => {
      localStorage.setItem("pf-cinematic-theme-v3", theme);
      document.documentElement.dataset.theme = theme;
    }, theme);
    await page.reload({ waitUntil: "networkidle" });
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      assert.equal(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
        false,
      );
      await page.screenshot({
        path: join(output, `inventory-${theme}-${width}.png`),
        fullPage: true,
      });
    }
  }
  check(
    "all generated modalities charge the displayed amount once and the inventory table fits desktop and mobile themes",
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
        mockRequests: requests,
        realModelCalls: 0,
        realPaymentCalls: 0,
      },
      null,
      2,
    ),
  );
  await browser?.close();
  if (app && app.exitCode === null) {
    const exited = new Promise((resolve) => app.once("exit", resolve));
    app.kill();
    await exited;
  }
  await new Promise((resolve) => mock.close(resolve));
  execFileSync("go", ["run", "./cmd/testdb", "drop", database], {
    stdio: "pipe",
    windowsHide: true,
  });
}
