import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { launchBrowser } from "./browser.mjs";
import { chooseOption } from "./ui-controls.mjs";

if (
  process.env.MYSQL_PORT !== "3307" ||
  process.env.REDIS_ADDR !== "127.0.0.1:6380"
) {
  throw new Error(
    "This UI check requires the isolated MySQL 3307 and Redis 6380 services",
  );
}

const output = resolve("test-results/studio-modes-ui");
await mkdir(output, { recursive: true });
const database = "phantomframe_test_studio_ui_" + Date.now();
const dir = await mkdtemp(join(output, "data-"));
const base = "http://127.0.0.1:4196";
let app,
  browser,
  serverOutput = "";
const errors = [],
  checks = [];
const check = (message) => {
  checks.push(message);
  console.log("PASS", message);
};
const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
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
        PORT: "4196",
        APP_URL: base,
        MYSQL_DATABASE: database,
        REDIS_PREFIX: database + ":",
        DATA_DIR: dir,
        STORAGE_DRIVER: "local",
        DEMO_MODE: "true",
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
  await start();
  browser = await launchBrowser();
  const ctx = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    reducedMotion: "reduce",
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
  const provider = await api(ctx, "/admin/providers", "POST", {
    name: "Local mode fixture",
    protocol: "demo",
    enabled: true,
  });
  const asset = (name, itemType, max) => ({
    name,
    label: name,
    type: "assets",
    itemType,
    max,
  });
  const choice = (name, label, values) => ({
    name,
    label,
    type: "select",
    default: values[0],
    options: values.map((v) => ({ l: v, v })),
  });
  const common = [
    choice("aspectRatio", "比例", ["16:9", "9:16"]),
    choice("resolution", "分辨率", ["720p", "1080p"]),
    {
      name: "duration",
      label: "时长",
      type: "number",
      min: 4,
      max: 12,
      default: 5,
    },
    choice("channel", "上游线路", ["default", "standard-long-channel-name"]),
  ];
  for (const [id, type, parameters] of [
    [
      "mode-video",
      "video",
      [
        ...common,
        choice("function_mode", "生成模式", [
          "omni_reference",
          "first_last_frames",
        ]),
        asset("imageUrls", "image", 9),
        asset("videoUrls", "video", 3),
        asset("audioUrls", "audio", 3),
      ],
    ],
    [
      "mode-image",
      "image",
      [
        choice("resolution", "分辨率", ["1k", "2k"]),
        asset("imageUrls", "image", 9),
      ],
    ],
    ["mode-text-only", "image", []],
    [
      "mode-total",
      "video",
      [
        { ...asset("imageUrls", "image", 9), maxTotal: 2 },
        { ...asset("audioUrls", "audio", 3), maxTotal: 2 },
      ],
    ],
    [
      "mode-multi-only",
      "video",
      [
        choice("function_mode", "生成模式", ["multi_frame"]),
        asset("imageUrls", "image", 9),
        asset("audioUrls", "audio", 3),
      ],
    ],
    [
      "mode-resolution",
      "video",
      [
        choice("resolution", "分辨率", ["720p", "1080p"]),
        choice("mode", "生成模式", ["frames", "references"]),
        {
          ...asset("imageUrls", "image", 2),
          maxByResolution: { "720p": 2, "1080p": 1 },
        },
      ],
    ],
  ]) {
    const item = await api(ctx, "/admin/models", "POST", {
      id,
      name: id === "mode-video" ? "SD2.5-30-10-10" : id,
      type,
      provider_id: provider.id,
      upstream_model: id,
      enabled: false,
      parameters,
      tags: [],
    });
    await api(ctx, "/admin/prices", "POST", {
      model_id: id,
      label: "local fixture",
      unit: "request",
      credits: 0.5,
      conditions: {},
    });
    await api(ctx, "/admin/models/" + id, "PUT", { ...item, enabled: true });
  }
  const page = await ctx.newPage();
  page.setDefaultTimeout(15000);
  page.on("pageerror", (e) => errors.push(e.message));
  const quotes = [];
  let submissions = 0;
  page.on("request", (req) => {
    if (req.url().endsWith("/api/pricing/quote") && req.method() === "POST")
      quotes.push(req.postDataJSON());
    if (req.url().endsWith("/api/jobs") && req.method() === "POST")
      submissions++;
  });
  const quoteUntil = async (predicate) => {
    for (let i = 0; i < 100; i++) {
      if (quotes.length && predicate(quotes.at(-1))) return;
      await wait(30);
    }
    throw Error("Expected updated quote parameters");
  };
  await page.goto(base + "/generate?model=mode-video", {
    waitUntil: "networkidle",
  });
  assert.match(
    await page
      .getByRole("combobox", { name: "创作模式", exact: true })
      .innerText(),
    /文生视频/,
  );
  assert.equal(await page.locator(".studio-references").count(), 0);
  assert.equal(await page.locator(".prompt-examples").count(), 0);
  await page
    .getByLabel("提示词", { exact: true })
    .fill("本地模式验证，不调用真实模型");
  await chooseOption(page, "创作模式", "image");
  await quoteUntil((q) => q.params.function_mode === "first_last_frames");
  assert.equal(
    await page
      .getByRole("button", { name: "开始生成", exact: true })
      .isDisabled(),
    true,
  );
  assert.equal(
    await page
      .getByRole("button", { name: "添加尾帧", exact: true })
      .isDisabled(),
    true,
  );
  const png = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=",
    "base64",
  );
  async function upload(label, name, mimeType, buffer) {
    await page.getByRole("button", { name: label, exact: true }).click();
    await page
      .locator('input[aria-label="上传参考素材"]')
      .setInputFiles({ name, mimeType, buffer });
    await page
      .getByRole("dialog", { name: "添加参考内容", exact: true })
      .waitFor({ state: "hidden" });
  }
  await upload("添加首帧", "first.png", "image/png", png);
  await quoteUntil((q) => q.params.imageUrls.length === 1);
  await upload("添加尾帧", "last.png", "image/png", png);
  await quoteUntil((q) => q.params.imageUrls.length === 2);
  const assets = await api(ctx, "/assets");
  assert.deepEqual(quotes.at(-1).params.imageUrls, [
    assets.items.find((a) => a.name === "first.png").id,
    assets.items.find((a) => a.name === "last.png").id,
  ]);
  check(
    "image-to-video selects the provider frame mode, blocks missing first frame and preserves first/last order",
  );
  await page.getByRole("button", { name: "移除图片1", exact: true }).click();
  await quoteUntil((q) => q.params.imageUrls.length === 0);
  assert.equal(await page.locator(".reference-item").count(), 0);
  assert.equal(
    await page
      .getByRole("button", { name: "开始生成", exact: true })
      .isDisabled(),
    true,
  );
  check("removing the first frame cannot silently promote the last frame");
  await chooseOption(page, "创作模式", "reference");
  await quoteUntil((q) => q.params.function_mode === "omni_reference");
  assert.equal(
    await page.getByRole("button", { name: "添加图片", exact: true }).count(),
    1,
  );
  assert.equal(
    await page.getByRole("button", { name: "添加视频", exact: true }).count(),
    1,
  );
  assert.equal(
    await page.getByRole("button", { name: "添加音频", exact: true }).count(),
    1,
  );
  const wav = Buffer.alloc(44 + 16000);
  wav.write("RIFF", 0);
  wav.writeUInt32LE(wav.length - 8, 4);
  wav.write("WAVEfmt ", 8);
  wav.writeUInt32LE(16, 16);
  wav.writeUInt16LE(1, 20);
  wav.writeUInt16LE(1, 22);
  wav.writeUInt32LE(8000, 24);
  wav.writeUInt32LE(16000, 28);
  wav.writeUInt16LE(2, 32);
  wav.writeUInt16LE(16, 34);
  wav.write("data", 36);
  wav.writeUInt32LE(16000, 40);
  await upload("添加音频", "reference.wav", "audio/wav", wav);
  await quoteUntil((q) => q.params.audioUrls.length === 1);
  await page.getByLabel("提示词", { exact: true }).fill("跟随 @音频1 绘制");
  await chooseOption(page, "创作模式", "text");
  await quoteUntil(
    (q) => q.params.audioUrls.length === 0 && q.params.imageUrls.length === 0,
  );
  assert.ok(
    !(await page.getByLabel("提示词", { exact: true }).inputValue()).includes(
      "@音频1",
    ),
  );
  assert.equal(await page.locator(".studio-references").count(), 0);
  check(
    "omni reference accepts audio and switching to text clears assets and prompt references before quoting",
  );
  await chooseOption(page, "生成类型", "image");
  await chooseOption(page, "模型", "mode-image");
  await chooseOption(page, "创作模式", "image");
  assert.equal(
    await page
      .getByRole("button", { name: "开始生成", exact: true })
      .isDisabled(),
    true,
  );
  await upload("添加参考内容", "image.png", "image/png", png);
  await quoteUntil(
    (q) => q.modelId === "mode-image" && q.params.imageUrls.length === 1,
  );
  await chooseOption(page, "创作模式", "text");
  await quoteUntil((q) => q.params.imageUrls.length === 0);
  await chooseOption(page, "模型", "mode-text-only");
  await page.getByRole("combobox", { name: "创作模式", exact: true }).click();
  assert.equal(
    await page
      .getByRole("option", { name: "图生图", exact: true })
      .getAttribute("aria-disabled"),
    "true",
  );
  await page.keyboard.press("Escape");
  check(
    "image modes enforce required references and disable unsupported image-to-image capability",
  );
  await chooseOption(page, "生成类型", "video");
  await chooseOption(page, "模型", "mode-total");
  await page.locator('input[aria-label="上传参考素材"]').setInputFiles([
    { name: "generic-first.png", mimeType: "image/png", buffer: png },
    { name: "generic-last.png", mimeType: "image/png", buffer: png },
  ]);
  await quoteUntil(
    (q) => q.modelId === "mode-total" && q.params.imageUrls.length === 2,
  );
  assert.match(
    await page
      .getByRole("combobox", { name: "创作模式", exact: true })
      .innerText(),
    /全能参考/,
  );
  await page.getByRole("button", { name: "添加音频", exact: true }).click();
  await page
    .getByRole("button", { name: "从资产库选择", exact: false })
    .click();
  const library = page.getByRole("dialog", {
    name: "从资产库选择",
    exact: true,
  });
  await library.locator(".asset-choice").first().waitFor();
  assert.equal(
    await library.locator(".asset-choice:not(:disabled)").count(),
    0,
    "total limit must also block other media types in the library",
  );
  let excessUploads = 0;
  const countExcessUploads = (req) => {
    if (req.url().endsWith("/api/assets") && req.method() === "POST")
      excessUploads++;
  };
  page.on("request", countExcessUploads);
  await library
    .locator('input[type="file"]')
    .setInputFiles({ name: "excess.wav", mimeType: "audio/wav", buffer: wav });
  await page
    .getByText("参考素材合计还可添加 0 个，请减少文件后重试", { exact: true })
    .waitFor();
  assert.equal(excessUploads, 0);
  page.off("request", countExcessUploads);
  await page.keyboard.press("Escape");
  check(
    "automatic reference mode preserves two images and the library enforces the shared media limit",
  );
  await chooseOption(page, "模型", "mode-multi-only");
  await chooseOption(page, "创作模式", "reference");
  await quoteUntil(
    (q) =>
      q.modelId === "mode-multi-only" &&
      q.params.function_mode === "multi_frame",
  );
  assert.equal(
    await page.getByRole("button", { name: "添加音频", exact: true }).count(),
    0,
  );
  assert.equal(
    await page.getByRole("button", { name: "添加视频", exact: true }).count(),
    0,
  );
  check(
    "image-only multi-frame wire modes hide incompatible audio and video inputs",
  );
  await chooseOption(page, "模型", "mode-resolution");
  await chooseOption(page, "创作模式", "image");
  await quoteUntil(
    (q) =>
      q.modelId === "mode-resolution" &&
      q.params.mode === "frames" &&
      q.params.imageUrls.length === 2,
  );
  await chooseOption(page, "分辨率", "1080p");
  await quoteUntil(
    (q) =>
      q.modelId === "mode-resolution" &&
      q.params.mode === "references" &&
      q.params.imageUrls.length === 1,
  );
  assert.equal(
    await page.getByRole("button", { name: "添加尾帧", exact: true }).count(),
    0,
  );
  check(
    "resolution changes recompute wire mode and reference capacity before requesting a new quote",
  );
  await chooseOption(page, "模型", "mode-video");
  await chooseOption(page, "创作模式", "image");
  const layout = [];
  await page.getByLabel("提示词", { exact: true }).fill("");
  await page.waitForFunction(() => !document.querySelector(".toast"));
  await page.mouse.move(5, 5);
  for (const [width, height, theme] of [
    [1440, 1000, "dark"],
    [1024, 768, "dark"],
    [390, 844, "dark"],
    [390, 844, "light"],
    [1440, 1000, "light"],
  ]) {
    await page.setViewportSize({ width, height });
    await page.evaluate((theme) => {
      document.documentElement.dataset.theme = theme;
    }, theme);
    const metrics = await page.evaluate(() => {
      const input = document.querySelector(".composer-input");
      const b = input.getBoundingClientRect();
      return {
        overflow: document.documentElement.scrollWidth > innerWidth,
        controlsInside: [...input.querySelectorAll(".ui-select-trigger")].every(
          (el) => {
            const r = el.getBoundingClientRect();
            return r.left >= b.left && r.right <= b.right;
          },
        ),
        outsideReferences: document.querySelectorAll(
          ".surface-composer > .studio-references",
        ).length,
      };
    });
    layout.push({ width, height, theme, ...metrics });
    assert.equal(metrics.overflow, false);
    assert.equal(metrics.controlsInside, true);
    assert.equal(metrics.outsideReferences, 0);
    await page.screenshot({
      path: join(output, `input-${width}-${theme}.png`),
      fullPage: true,
      animations: "disabled",
    });
  }
  await writeFile(join(output, "layout.json"), JSON.stringify(layout, null, 2));
  check(
    "reference slots and creation controls stay inside the input area across desktop, tablet, mobile and both themes",
  );
  assert.equal(submissions, 0);
  assert.deepEqual(errors, []);
  check(
    "all checks use isolated data; no generation or payment submissions occur",
  );
} catch (error) {
  console.error(error);
  console.error(serverOutput);
  process.exitCode = 1;
} finally {
  await writeFile(
    join(output, "results.json"),
    JSON.stringify(
      { checks, pageErrors: errors, realModelCalls: 0, realPaymentCalls: 0 },
      null,
      2,
    ),
  );
  await browser?.close();
  await stop();
  execFileSync("go", ["run", "./cmd/testdb", "drop", database], {
    stdio: "pipe",
    windowsHide: true,
  });
}
