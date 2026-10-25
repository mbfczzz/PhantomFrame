import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { launchBrowser } from "./browser.mjs";
import { chooseOption } from "./ui-controls.mjs";
const root = resolve("test-results");
await mkdir(root, { recursive: true });
const dataDir = await mkdtemp(join(root, "ui-data-"));
const database = "phantomframe_test_ui_" + Date.now();
const redisPrefix = database + ":";
const port = Number(process.env.UI_TEST_PORT || 4174),
  base = `http://127.0.0.1:${port}`;
let server,
  output = "",
  browser,
  page;
const errors = [];
const checks = [];
const start = async () => {
  server = spawn(
    resolve(`bin/phantomframe${process.platform === "win32" ? ".exe" : ""}`),
    [],
    {
      env: {
        ...process.env,
        PORT: String(port),
        HOST: "127.0.0.1",
        APP_URL: base,
        DATA_DIR: dataDir,
        MYSQL_DATABASE: database,
        REDIS_PREFIX: redisPrefix,
        DEMO_MODE: "true",
        STORAGE_DRIVER: "local",
        ADMIN_USERNAME: "ui-admin",
        ADMIN_PASSWORD: "UITest-password-2026",
        ENCRYPTION_KEY: "b".repeat(64),
        APP_ENV: "test",
        COOKIE_SECURE: "false",
      },
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
  server.stdout.on("data", (d) => (output += d.toString()));
  server.stderr.on("data", (d) => (output += d.toString()));
  for (let i = 0; i < 80; i++) {
    try {
      if ((await fetch(base + "/api/health")).ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 150));
  }
  throw new Error("UI server failed to start: " + output);
};
const stop = async () => {
  if (!server) return;
  const child = server;
  server = undefined;
  if (child.exitCode !== null) return;
  const done = new Promise((r) => child.once("exit", r));
  child.kill();
  await done;
};
const check = (name) => {
  checks.push(name);
  console.log("PASS", name);
};
try {
  execFileSync("go", ["run", "./cmd/testdb", "create", database], {
    stdio: "pipe",
    windowsHide: true,
  });
  await start();
  browser = await launchBrowser();
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    acceptDownloads: true,
  });
  page = await context.newPage();
  page.setDefaultTimeout(15000);
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("dialog", (d) => {
    errors.push("Unexpected native dialog: " + d.type());
    d.dismiss();
  });
  await page.goto(base + "/login");
  await page
    .getByRole("textbox", { name: "用户名", exact: true })
    .fill("ui-admin");
  await page.getByLabel("密码", { exact: true }).fill("wrong-password");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page
    .getByRole("alert")
    .filter({ hasText: "账号或密码不正确" })
    .waitFor();
  assert.equal(
    await page.getByLabel("密码", { exact: true }).getAttribute("aria-invalid"),
    "true",
  );
  await page.getByLabel("密码", { exact: true }).fill("UITest-password-2026");
  assert.equal(await page.locator(".auth-error").count(), 0);
  check("login errors appear inline and clear when credentials change");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL("**/generate");
  const moduleNames = ["创作", "生成记录", "模型与价格", "资产库", "开放平台"];
  const serviceNames = ["提交工单", "资金流水", "兑换充值"];
  assert.deepEqual(
    await page.locator(".primary-nav a").allTextContents(),
    moduleNames,
  );
  assert.deepEqual(
    await page.locator('.service-nav a:not([href="/docs"])').allTextContents(),
    serviceNames,
  );
  assert.equal(await page.locator(".sidebar").count(), 0);
  assert.equal(await page.locator('a[href^="/canvas"]').count(), 0);
  await chooseOption(page, "生成类型", "video");
  await chooseOption(page, "模型", "sd2.0-933-op");
  check("login and redesigned creation interface");
  await page.locator(".prompt-examples summary").click();
  await page.keyboard.press("Escape");
  assert.equal(await page.locator(".prompt-examples[open]").count(), 0);
  await page.locator(".prompt-examples summary").click();
  await page
    .locator(".prompt-examples button")
    .filter({ hasText: "产品静物" })
    .click();
  assert.match(
    await page
      .getByRole("textbox", { name: "提示词", exact: true })
      .inputValue(),
    /磨砂陶瓷/,
  );
  assert.equal(await page.locator(".prompt-examples[open]").count(), 0);
  check("prompt examples fill the editor and close after selection");
  const req = async (path, method = "GET", data) => {
    const r = await context.request.fetch(base + "/api" + path, {
      method,
      data,
    });
    return { status: r.status(), data: await r.json() };
  };
  // Image and video generation go through the real local worker and wallet.
  await chooseOption(page, "生成类型", "image");
  await chooseOption(page, "模型", "nanobanana");
  await page
    .getByLabel("提示词", { exact: true })
    .fill("星云中的透明水母，梦幻的极光与柔和的光影");
  await page.getByRole("button", { name: "开始生成", exact: true }).click();
  await page
    .locator(".job-card .badge.succeeded")
    .first()
    .waitFor({ timeout: 20000 });
  assert.equal((await req("/me")).data.balance, 999);
  await page.screenshot({
    path: join(root, "ui-image-generation.png"),
    fullPage: true,
  });
  check("image generation, polling, private output and exact debit");
  await page
    .getByRole("button", { name: "任务详情", exact: true })
    .first()
    .click();
  await page
    .getByRole("dialog", { name: "任务详情", exact: true })
    .getByRole("button", { name: "删除记录", exact: true })
    .click();
  const deletion = page.getByRole("dialog", {
    name: "删除生成记录",
    exact: true,
  });
  await deletion.waitFor();
  await page.waitForFunction(
    () => document.activeElement?.textContent === "取消",
  );
  assert.equal(
    await page
      .locator(".modal:not(.confirmation-dialog)")
      .getAttribute("aria-hidden"),
    "true",
  );
  await page.keyboard.press("Escape");
  await deletion.waitFor({ state: "hidden" });
  await page.getByRole("dialog", { name: "任务详情", exact: true }).waitFor();
  assert.equal(
    await page.evaluate(() => document.body.style.overflow),
    "hidden",
  );
  assert.equal((await req("/jobs")).data.total, 1);
  await page.keyboard.press("Escape");
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal(await page.evaluate(() => document.body.style.overflow), "");
  check(
    "nested confirmation cancellation preserves data, parent dialog, focus and scroll lock",
  );
  await page.getByRole("button", { name: "收起生成预览", exact: true }).click();
  assert.equal(
    await page
      .locator("#generation-preview")
      .evaluate((el) => getComputedStyle(el).display),
    "none",
  );
  await page.getByRole("button", { name: "展开生成预览", exact: true }).click();
  await page.locator(".job-card .badge.succeeded").first().waitFor();
  check(
    "generation preview can be collapsed and restored without losing results",
  );
  await chooseOption(page, "生成类型", "video");
  await chooseOption(page, "模型", "sd2.0-933-op");
  await page
    .getByLabel("提示词", { exact: true })
    .fill("山间的云海缓缓流动，镜头向前推进");
  await page.getByRole("button", { name: "开始生成", exact: true }).click();
  await page.waitForFunction(
    () => document.querySelectorAll(".job-card .badge.succeeded").length === 2,
    {},
    { timeout: 20000 },
  );
  assert.equal((await req("/me")).data.balance, 961);
  const videoJob = (await req("/jobs")).data.items.find(
    (j) => j.type === "video",
  );
  assert.equal(videoJob.outputs[0].type, "video");
  check("video generation and persisted playable video output");
  await page.goto(base + "/assets");
  await page.locator(".asset-card").first().waitFor();
  assert.equal(await page.locator(".asset-card").count(), 2);
  check("generated assets appear in the asset library");
  // Exercise the actual admin editors.
  await page.goto(base + "/admin/providers");
  await page.getByRole("button", { name: "新增渠道" }).click();
  let dialog = page.getByRole("dialog");
  await dialog.getByLabel("渠道名称").fill("UI 测试通道");
  await dialog.getByLabel("接入协议").selectOption("demo");
  await dialog.getByRole("button", { name: "保存渠道" }).click();
  await dialog.waitFor({ state: "hidden" });
  const provider = (await req("/admin/providers")).data.find(
    (p) => p.name === "UI 测试通道",
  );
  assert.ok(provider);
  check("provider create form");
  await page
    .getByRole("button", { name: "管理模型 UI 测试通道", exact: true })
    .click();
  const channelDialog = page.getByRole("dialog", {
    name: "渠道模型",
    exact: true,
  });
  await channelDialog
    .getByRole("button", { name: "添加支持模型", exact: true })
    .click();
  dialog = page.getByRole("dialog", { name: "添加支持模型", exact: true });
  await dialog.getByLabel("上游模型 ID", { exact: true }).fill("ui-upstream");
  await dialog.getByLabel("模型名称", { exact: true }).fill("UI 测试模型");
  assert.equal(
    await dialog.getByLabel("图片数量上限", { exact: true }).inputValue(),
    "",
  );
  await dialog
    .getByRole("checkbox", { name: "支持视频参考", exact: true })
    .check();
  await dialog
    .getByRole("checkbox", { name: "支持音频参考", exact: true })
    .check();
  await dialog.getByLabel("视频数量上限", { exact: true }).fill("1");
  await dialog.getByLabel("音频数量上限", { exact: true }).fill("2");
  await dialog
    .getByRole("button", { name: "保存渠道模型", exact: true })
    .click();
  await dialog.waitFor({ state: "hidden" });
  const channelEntry = (
    await req("/admin/providers/" + provider.id + "/models")
  ).data.items.find((item) => item.upstreamModel === "ui-upstream");
  assert.ok(channelEntry);
  assert.equal(channelEntry.publishedModels.length, 0);
  const keyButton = channelDialog.getByRole("button", { name: "配置调用密钥 UI 测试模型", exact: true });
  await keyButton.click();
  let keyDialog = page.getByRole("dialog", { name: "模型调用密钥", exact: true });
  await keyDialog.getByRole("radio", { name: /使用模型独立 Key/ }).check();
  await keyDialog.getByLabel("模型独立 API Key", { exact: true }).fill("ui-test-model-key");
  await keyDialog.getByRole("button", { name: "保存密钥", exact: true }).click();
  await keyDialog.waitFor({ state: "hidden" });
  await keyButton.getByText("独立 Key", { exact: true }).waitFor();
  const keySaved = (await req("/admin/providers/" + provider.id + "/models")).data;
  assert.equal(keySaved.items.find(item => item.id === channelEntry.id).hasKey, true);
  assert.ok(!JSON.stringify(keySaved).includes("ui-test-model-key"));
  await keyButton.click();
  keyDialog = page.getByRole("dialog", { name: "模型调用密钥", exact: true });
  assert.equal(await keyDialog.getByLabel("模型独立 API Key", { exact: true }).inputValue(), "");
  assert.equal(await keyDialog.getByRole("button", { name: "保存密钥", exact: true }).isDisabled(), true);
  await keyDialog.getByLabel("模型独立 API Key", { exact: true }).fill("cancelled-key-change");
  await keyDialog.getByRole("button", { name: "取消", exact: true }).click();
  await keyDialog.waitFor({ state: "hidden" });
  check("model-specific keys save without disclosure and cancelling an edit keeps the configured key");
  assert.ok(
    !(await req("/admin/models")).data.some(
      (model) => model.upstream_model === "ui-upstream",
    ),
  );
  await channelDialog
    .getByRole("button", { name: "发布 UI 测试模型", exact: true })
    .click();
  dialog = page.getByRole("dialog", { name: "发布到模型管理", exact: true });
  assert.equal(await dialog.getByLabel("发布名称", {exact:true}).inputValue(), "u-UI 测试模型");
  await dialog.getByLabel("发布名称", {exact:true}).fill("UI 测试模型");
  await dialog.getByLabel("站内模型 ID", { exact: true }).fill("ui-model");
  await dialog.getByRole("button", { name: "确认发布", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  await keyButton.click();
  keyDialog = page.getByRole("dialog", { name: "模型调用密钥", exact: true });
  await keyDialog.getByRole("radio", { name: /使用渠道默认 Key/ }).check();
  await keyDialog.getByRole("button", { name: "恢复渠道默认", exact: true }).click();
  await keyDialog.waitFor({ state: "hidden" });
  await keyButton.getByText("渠道默认", { exact: true }).waitFor();
  assert.equal((await req("/admin/providers/" + provider.id + "/models")).data.items.find(item => item.id === channelEntry.id).hasKey, false);
  check("published models can restore their channel default key");
  assert.equal(
    (await req("/admin/models")).data.find((model) => model.id === "ui-model")
      .enabled,
    false,
  );
  assert.ok(
    !(await req("/models")).data.some((model) => model.id === "ui-model"),
  );
  await channelDialog
    .getByRole("link", { name: "去配置参数与价格", exact: true })
    .click();
  await page
    .getByRole("heading", { name: "此模型尚未配置价格", exact: true })
    .waitFor();
  assert.equal(
    await page
      .getByRole("tab", { name: "计费规则", exact: true })
      .getAttribute("aria-selected"),
    "true",
  );
  check(
    "channel drafts remain separate until publication, then open model pricing while disabled",
  );
  const originalOtherPrices = (await req("/admin/models")).data.find(
    (model) => model.id === "nanobanana",
  ).prices;
  assert.equal(
    await page.getByRole("link", { name: "价格管理", exact: true }).count(),
    0,
  );
  await page.getByRole("button", { name: "新增价格规则" }).click();
  dialog = page.getByRole("dialog", { name: "新增价格规则", exact: true });
  assert.equal(
    await dialog.getByLabel("模型", { exact: true }).inputValue(),
    "UI 测试模型",
  );
  assert.equal(
    await dialog.getByLabel("模型", { exact: true }).isDisabled(),
    true,
  );
  await dialog.getByLabel("规格名称").fill("标准");
  await dialog.getByLabel("单价（元）").fill("0.5");
  await dialog.getByRole("button", { name: "保存价格" }).click();
  await dialog.waitFor({ state: "hidden" });
  await page.getByRole("tab", { name: "基本信息", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "编辑模型", exact: true });
  assert.equal(
    await dialog.getByLabel("供应商渠道", { exact: true }).isDisabled(),
    true,
  );
  await dialog.getByLabel("状态", { exact: true }).selectOption("true");
  await dialog.getByRole("button", { name: "保存模型", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  assert.ok(
    (await req("/models")).data.some((model) => model.id === "ui-model"),
  );
  await page
    .getByRole("button", { name: "配置价格 UI 测试模型", exact: true })
    .click();
  assert.equal(
    (await req("/pricing/quote", "POST", { modelId: "ui-model", params: {} }))
      .data.total,
    0.5,
  );
  await page
    .getByRole("button", { name: "编辑价格 UI 测试模型 标准", exact: true })
    .click();
  dialog = page.getByRole("dialog", { name: "编辑价格规则", exact: true });
  await dialog.getByLabel("单价（元）").fill("0.1");
  await dialog.getByRole("button", { name: "保存价格" }).click();
  await dialog.waitFor({ state: "hidden" });
  assert.equal(
    (await req("/pricing/quote", "POST", { modelId: "ui-model", params: {} }))
      .data.total,
    0.1,
  );
  check("price creation and edits immediately affect server quotes");
  await page.waitForFunction(() => document.querySelector(".model-pricing-section .gold strong")?.textContent === "¥0.1");
  assert.equal(await page.locator(".model-pricing-section .gold strong").first().innerText(),"¥0.1");
  await page.getByRole("button", { name: "新增价格规则", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "新增价格规则", exact: true });
  await dialog.getByLabel("规格名称", { exact: true }).fill("方图规格");
  await dialog.getByLabel("单价（元）", { exact: true }).fill("0.25");
  await dialog.getByLabel("比例", { exact: true }).selectOption("1:1");
  await dialog.getByRole("button", { name: "保存价格", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  assert.equal(
    (
      await req("/pricing/quote", "POST", {
        modelId: "ui-model",
        params: { aspectRatio: "1:1" },
      })
    ).data.total,
    0.25,
  );
  const scopedRow = page
    .locator(".model-pricing-section tbody tr")
    .filter({ hasText: "方图规格" });
  await scopedRow
    .getByRole("button", { name: "删除价格规则", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "删除价格规则", exact: true })
    .getByRole("button", { name: "取消", exact: true })
    .click();
  await scopedRow
    .getByRole("button", { name: "删除价格规则", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "删除价格规则", exact: true })
    .getByRole("button", { name: "删除规则", exact: true })
    .click();
  await scopedRow.waitFor({ state: "hidden" });
  assert.equal(
    (
      await req("/pricing/quote", "POST", {
        modelId: "ui-model",
        params: { aspectRatio: "1:1" },
      })
    ).data.total,
    0.1,
  );
  assert.deepEqual(
    (await req("/admin/models")).data.find((model) => model.id === "nanobanana")
      .prices,
    originalOtherPrices,
  );
  await page.getByRole("button", { name: "完成", exact: true }).click();
  await page
    .getByRole("button", { name: "配置价格 UI 测试模型", exact: true })
    .click();
  await page
    .getByRole("button", { name: "编辑价格 UI 测试模型 标准", exact: true })
    .waitFor();
  await page.getByRole("button", { name: "完成", exact: true }).click();
  await page.goto(base + "/admin/prices");
  await page.waitForURL("**/admin/models");
  await page.locator(".model-channel-toggle").first().waitFor();
  assert.equal(await page.locator(".model-channel-table").count(), 0);
  await page.getByRole("button", { name: "全部展开", exact: true }).click();
  assert.equal(await page.locator(".model-channel-table").count(), await page.locator(".model-channel-group").count());
  await page.getByRole("button", { name: "全部收起", exact: true }).click();
  assert.equal(await page.locator(".model-channel-table").count(), 0);
  const groupSearch = page.getByPlaceholder("搜索渠道、模型名称或 ID", { exact: true });
  await groupSearch.fill("ui-upstream");
  assert.equal(await page.locator(".model-channel-group").count(), 1);
  assert.equal(await page.locator(".model-channel-table tbody tr").count(), 1);
  await groupSearch.fill("UI 测试通道");
  assert.equal(await page.locator(".model-channel-group").count(), 1);
  await groupSearch.fill("no-such-model-321");
  await page.getByRole("heading", { name: "没有匹配的模型", exact: true }).waitFor();
  await groupSearch.fill("");
  assert.equal(await page.locator(".model-channel-table").count(), 0);
  check("model management groups by channel, supports expand/collapse all and expands search matches");
  check(
    "pricing stays scoped to its model, conditional rules can be removed, other models stay unchanged and legacy route redirects",
  );
  const configuredModel = (await req("/admin/models")).data.find(
    (model) => model.id === "ui-model",
  );
  assert.equal(
    configuredModel.parameters.find(
      (parameter) => parameter.itemType === "image",
    ).max,
    undefined,
  );
  assert.equal(
    configuredModel.parameters.find(
      (parameter) => parameter.itemType === "video",
    ).max,
    1,
  );
  assert.equal(
    configuredModel.parameters.find(
      (parameter) => parameter.itemType === "audio",
    ).max,
    2,
  );
  await page.goto(base + "/models", { waitUntil: "networkidle" });
  const modelCard = page.locator(".catalog-card").filter({
    has: page.getByRole("heading", { name: "UI 测试模型", exact: true }),
  });
  await modelCard.getByLabel("参考图片：不限", { exact: true }).waitFor();
  assert.equal(await modelCard.locator(".catalog-price strong").innerText(),"¥0.1");
  await modelCard.getByLabel("参考视频：1 段", { exact: true }).waitFor();
  await modelCard.getByLabel("参考音频：2 段", { exact: true }).waitFor();
  await page.goto(base + "/generate?model=ui-model", {
    waitUntil: "networkidle",
  });
  const limitPNG = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=",
    "base64",
  );
  await page.locator('input[aria-label="上传参考素材"]').setInputFiles(
    Array.from({ length: 12 }, (_, index) => ({
      name: `unlimited-${index}.png`,
      mimeType: "image/png",
      buffer: limitPNG,
    })),
  );
  await page.waitForFunction(
    () => document.querySelectorAll(".reference-item").length === 12,
  );
  await page.getByLabel("提示词", { exact: true }).fill("参考场景 @图片12");
  await page.waitForFunction(
    () => !document.querySelector(".studio-generate").disabled,
  );
  const configPage = await context.newPage();
  configPage.on("pageerror", (error) => errors.push(error.message));
  await configPage.goto(base + "/admin/models", { waitUntil: "networkidle" });
  await configPage.locator(".model-channel-toggle").filter({ hasText: "UI 测试通道" }).click();
  await configPage
    .getByRole("button", { name: "编辑 UI 测试模型", exact: true })
    .click();
  await configPage.getByLabel("图片数量上限", { exact: true }).fill("2");
  await configPage
    .getByRole("button", { name: "保存模型", exact: true })
    .click();
  await configPage.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal(
    (await req("/admin/models")).data
      .find((model) => model.id === "ui-model")
      .parameters.find((parameter) => parameter.itemType === "image").max,
    2,
  );
  await page.bringToFront();
  await page.evaluate(() =>
    window.dispatchEvent(new Event("visibilitychange")),
  );
  await page.waitForFunction(
    () => document.querySelectorAll(".reference-item").length === 2,
  );
  assert.ok(
    !(await page.getByLabel("提示词", { exact: true }).inputValue()).includes(
      "@图片12",
    ),
  );
  let limitUploads = 0;
  const countUploads = (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/api/assets"
    )
      limitUploads++;
  };
  page.on("request", countUploads);
  await page.locator('input[aria-label="上传参考素材"]').setInputFiles({
    name: "excess.png",
    mimeType: "image/png",
    buffer: limitPNG,
  });
  await page.getByText("当前模型最多支持 2 个图片", { exact: true }).waitFor();
  assert.equal(limitUploads, 0);
  page.off("request", countUploads);
  await page.getByRole("button", { name: "添加参考内容", exact: true }).click();
  await page
    .getByRole("button", { name: "从资产库选择", exact: false })
    .click();
  await page
    .getByRole("dialog", { name: "从资产库选择", exact: true })
    .getByRole("tab", { name: "图片", exact: true })
    .click();
  await page.locator(".asset-choice").first().waitFor();
  assert.equal(await page.locator(".asset-choice:not(:disabled)").count(), 0);
  await page.keyboard.press("Escape");
  await configPage.bringToFront();
  await configPage
    .getByRole("button", { name: "编辑 UI 测试模型", exact: true })
    .click();
  await configPage.getByLabel("图片数量上限", { exact: true }).fill("");
  await configPage
    .getByRole("button", { name: "保存模型", exact: true })
    .click();
  await configPage.getByRole("dialog").waitFor({ state: "hidden" });
  await page.bringToFront();
  await page.evaluate(() =>
    window.dispatchEvent(new Event("visibilitychange")),
  );
  await page
    .locator(".studio-references-heading")
    .getByText(/图片不限/)
    .waitFor();
  await page.locator('input[aria-label="上传参考素材"]').setInputFiles({
    name: "unlimited-again.png",
    mimeType: "image/png",
    buffer: limitPNG,
  });
  await page.waitForFunction(
    () => document.querySelectorAll(".reference-item").length === 3,
  );
  await configPage.close();
  check(
    "admin reference limits drive cards, unlimited uploads, live configuration, prompt cleanup and library selection",
  );
  await page.goto(base + "/admin/models");
  await page.locator(".model-channel-toggle").filter({ hasText: "UI 测试通道" }).click();
  await page
    .getByRole("button", { name: "停用 UI 测试模型", exact: true })
    .click();
  await page
    .getByRole("button", { name: "启用 UI 测试模型", exact: true })
    .waitFor();
  assert.ok(!(await req("/models")).data.some((m) => m.id === "ui-model"));
  await page
    .getByRole("button", { name: "删除 UI 测试模型", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "删除模型", exact: true })
    .getByRole("button", { name: "删除模型", exact: true })
    .click();
  await page
    .getByRole("button", { name: "删除 UI 测试模型", exact: true })
    .waitFor({ state: "hidden" });
  assert.ok(
    !(await req("/admin/models")).data.some((m) => m.id === "ui-model"),
  );
  assert.equal(
    (await req("/admin/providers/" + provider.id + "/models")).data.items.find(
      (item) => item.id === channelEntry.id,
    ).publishedModels.length,
    0,
  );
  check("model status toggle and deletion");
  await page.goto(base + "/admin/codes");
  await page.getByRole("button", { name: "生成兑换码", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("数量", { exact: true }).fill("1");
  await dialog.getByLabel("每张额度（元）").fill("25");
  await dialog
    .getByRole("button", { name: "生成 1 个兑换码", exact: true })
    .click();
  await dialog.waitFor({ state: "hidden" });
  const code = (await req("/admin/codes")).data.items[0].code;
  await page.goto(base + "/redeem");
  await page.getByRole("textbox", { name: "兑换码", exact: true }).fill(code);
  await page.getByRole("button", { name: "确认兑换", exact: true }).click();
  await page
    .getByRole("status")
    .filter({ hasText: "兑换成功，已到账 ¥25" })
    .waitFor();
  assert.equal((await req("/me")).data.balance, 986);
  check("redeem code creation and account credit redemption");
  await page.getByRole("textbox", { name: "兑换码", exact: true }).fill(code);
  await page.getByRole("button", { name: "确认兑换", exact: true }).click();
  await page.getByRole("alert").filter({ hasText: "已使用" }).waitFor();
  assert.equal((await req("/me")).data.balance, 986);
  await page.goto(base + "/transactions?kind=redeem");
  await page.locator(".ledger-panel tbody tr").first().waitFor();
  assert.equal(await page.locator(".ledger-panel tbody tr").count(), 1);
  assert.equal(
    (await req("/transactions?kind=redeem")).data.summary.income,
    25,
  );
  await chooseOption(page, "流水类型", "generation");
  await page.waitForFunction(
    () => document.querySelectorAll(".ledger-panel tbody tr").length === 2,
  );
  await chooseOption(page, "收支方向", "out");
  assert.equal(
    (await req("/transactions?kind=generation&direction=out")).data.summary
      .expense,
    39,
  );
  await page.getByRole("button", { name: "重置筛选", exact: true }).click();
  await page.waitForFunction(
    () => document.querySelectorAll(".ledger-panel tbody tr").length === 3,
  );
  check("redemption receipt, duplicate-code errors and filtered ledger totals");

  await page.goto(base + "/tickets");
  await page.getByRole("link", { name: "新建工单", exact: true }).click();
  await page.getByLabel("问题标题", { exact: true }).fill("积分扣费核对");
  await chooseOption(page, "问题分类", "billing");
  await page
    .getByLabel("问题描述", { exact: true })
    .fill("视频任务已完成，需要核对这次积分扣费。");
  await page
    .getByLabel("关联任务 ID（选填）", { exact: true })
    .fill(videoJob.id);
  await page.getByRole("button", { name: "提交工单", exact: true }).click();
  await page.waitForURL(/\/tickets\/[a-f0-9-]+$/);
  const ticketURL = page.url();
  await page
    .getByLabel("补充说明", { exact: true })
    .fill("补充：任务完成后已经可以下载视频。");
  await page.getByRole("button", { name: "发送回复", exact: true }).click();
  await page
    .getByText("补充：任务完成后已经可以下载视频。", { exact: true })
    .waitFor();
  await page.goto(base + "/admin/tickets");
  await page
    .locator(".ticket-list-item")
    .filter({ hasText: "积分扣费核对" })
    .click();
  await page
    .getByLabel("回复用户", { exact: true })
    .fill("已核对，扣费与生成参数一致，可在资金流水查看。");
  await page.getByRole("button", { name: "发送回复", exact: true }).click();
  await page
    .getByText("已核对，扣费与生成参数一致，可在资金流水查看。", {
      exact: true,
    })
    .waitFor();
  await page.getByRole("button", { name: "标记已解决", exact: true }).click();
  await page
    .locator(".ticket-detail-panel .ticket-state")
    .filter({ hasText: "已解决" })
    .waitFor();
  await page.goto(ticketURL);
  await page
    .getByText("已核对，扣费与生成参数一致，可在资金流水查看。", {
      exact: true,
    })
    .waitFor();
  await page.getByRole("button", { name: "重新打开", exact: true }).click();
  await page.getByLabel("补充说明", { exact: true }).waitFor();
  await page.getByRole("button", { name: "关闭工单", exact: true }).click();
  await page
    .locator(".ticket-detail-panel .ticket-state")
    .filter({ hasText: "已关闭" })
    .waitFor();
  await page.screenshot({
    path: join(root, "ui-ticket-thread.png"),
    fullPage: true,
  });
  check(
    "ticket submission, linked job, customer follow-up, admin reply, resolution and reopening",
  );
  await page.goto(base + "/profile");
  await page
    .locator(".page-header")
    .getByRole("link", { name: "开放平台", exact: true })
    .click();
  await page.waitForURL("**/developer");
  await page.getByRole("button", { name: "创建 Key", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("Key 名称").fill("UI integration");
  await dialog.getByRole("button", { name: "创建", exact: true }).click();
  const token = await page.locator(".secret-value").innerText();
  assert.ok(token.startsWith("pf_"));
  const external = await fetch(base + "/api/models", {
    headers: { Authorization: "Bearer " + token },
  });
  assert.equal(external.status, 200);
  await page.getByRole("button", { name: "我已保存" }).click();
  check("API key creation, one-time reveal and API authentication");
  const apiCreated = await fetch(base + "/api/jobs", {
    method: "POST",
    headers: {
      Authorization: "Bearer " + token,
      "Content-Type": "application/json",
      "Idempotency-Key": "ui-open-platform",
    },
    body: JSON.stringify({
      modelId: "nanobanana",
      prompt: "开放平台 API 生成验证",
      params: {},
    }),
  });
  assert.equal(apiCreated.status, 202);
  const apiJob = await apiCreated.json();
  let apiFinished;
  for (let attempt = 0; attempt < 60; attempt++) {
    const response = await fetch(base + "/api/jobs/" + apiJob.id, {
      headers: { Authorization: "Bearer " + token },
    });
    apiFinished = await response.json();
    if (!["queued", "running"].includes(apiFinished.status)) break;
    await new Promise((resolve) => setTimeout(resolve, 150));
  }
  assert.equal(apiFinished.status, "succeeded");
  assert.equal(apiFinished.source, "api");
  assert.equal(
    (
      await fetch(base + apiFinished.outputs[0].url, {
        headers: { Authorization: "Bearer " + token },
      })
    ).status,
    200,
  );
  await page.getByRole("tab", { name: "调用记录", exact: true }).click();
  await page.locator(".developer-page tbody tr").first().waitFor();
  assert.equal(await page.locator(".developer-page tbody tr").count(), 1);
  assert.equal((await req("/usage?source=api")).data.total, 1);
  assert.equal((await req("/usage?source=api")).data.spent, 1);
  await page.getByRole("tab", { name: "快速接入", exact: true }).click();
  await page.getByRole("tab", { name: "视频生成", exact: true }).click();
  assert.match(
    await page.locator(".platform-code").innerText(),
    /sd2\.0-933-op/,
  );
  await page.getByRole("tab", { name: "Node.js", exact: true }).click();
  assert.match(
    await page.locator(".platform-code").innerText(),
    /process.env.PHANTOMFRAME_API_KEY/,
  );
  assert.ok(
    !(await page.locator(".platform-code").innerText()).includes(token),
  );
  await page.getByRole("tab", { name: "API 密钥", exact: true }).click();
  await page
    .getByRole("button", { name: "撤销 UI integration", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "撤销 API Key", exact: true })
    .getByRole("button", { name: "取消", exact: true })
    .click();
  assert.equal(
    (
      await fetch(base + "/api/models", {
        headers: { Authorization: "Bearer " + token },
      })
    ).status,
    200,
  );
  await page
    .getByRole("button", { name: "撤销 UI integration", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "撤销 API Key", exact: true })
    .getByRole("button", { name: "确认撤销", exact: true })
    .click();
  await page
    .getByRole("button", { name: "撤销 UI integration", exact: true })
    .waitFor({ state: "hidden" });
  assert.equal(
    (
      await fetch(base + "/api/models", {
        headers: { Authorization: "Bearer " + token },
      })
    ).status,
    401,
  );
  check(
    "open platform API generation, download, source-specific usage, examples and immediate revocation",
  );
  await page.goto(base + "/generate");
  const modelControl = page.getByRole("combobox", {
    name: "模型",
    exact: true,
  });
  await modelControl.click();
  await page
    .getByRole("textbox", { name: "搜索模型", exact: true })
    .fill("NanoBanana");
  assert.equal(
    await page
      .getByRole("listbox", { name: "模型", exact: true })
      .getByRole("option")
      .count(),
    1,
  );
  await page.keyboard.press("Enter");
  assert.match(await modelControl.innerText(), /NanoBanana/);
  assert.equal(
    await modelControl.evaluate((el) => el === document.activeElement),
    true,
  );
  await modelControl.click();
  await page.keyboard.press("Escape");
  assert.equal(
    await page.getByRole("listbox", { name: "模型", exact: true }).count(),
    0,
  );
  check("model search, keyboard selection and Escape restore focus");

  const png = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=",
    "base64",
  );
  await page.getByRole("button", { name: "添加参考内容", exact: true }).click();
  await page
    .getByRole("dialog", { name: "添加参考内容", exact: true })
    .waitFor();
  assert.equal(await page.locator(".reference-upload-zone").isVisible(), true);
  await page.locator('input[aria-label="上传参考素材"]').setInputFiles({
    name: "reference.png",
    mimeType: "image/png",
    buffer: png,
  });
  await page.getByRole("button", { name: "预览图片1", exact: true }).waitFor();
  await page.getByLabel("提示词", { exact: true }).fill("按照 @图");
  await page.getByRole("listbox", { name: "引用参考素材" }).waitFor();
  await page.keyboard.press("Enter");
  assert.match(
    await page.getByLabel("提示词", { exact: true }).inputValue(),
    /@图片1/,
  );
  await page.getByRole("button", { name: "移除图片1", exact: true }).click();
  assert.ok(
    !(await page.getByLabel("提示词", { exact: true }).inputValue()).includes(
      "@图片1",
    ),
  );
  check(
    "direct reference upload, mention selection and removal keep prompt references consistent",
  );

  await page.getByLabel("提示词", { exact: true }).fill("按参考素材绘制静物：");
  await page
    .getByLabel("提示词", { exact: true })
    .evaluate((element, bytes) => {
      const clipboard = new DataTransfer();
      clipboard.items.add(
        new File([new Uint8Array(bytes)], "pasted-reference.png", {
          type: "image/png",
        }),
      );
      element.dispatchEvent(
        new ClipboardEvent("paste", {
          clipboardData: clipboard,
          bubbles: true,
          cancelable: true,
        }),
      );
    }, Array.from(png));
  await page.getByRole("button", { name: "预览图片2", exact: true }).waitFor();
  await page.waitForFunction(() =>
    document.querySelector("#creation-prompt").value.includes("@图片2"),
  );
  await page.waitForFunction(
    () => !document.querySelector(".studio-generate").disabled,
  );
  await page.getByRole("button", { name: "开始生成", exact: true }).click();
  await page.waitForFunction(
    () => document.querySelectorAll(".job-card .badge.succeeded").length === 4,
    {},
    { timeout: 20000 },
  );
  const referenced = (await req("/jobs")).data.items.find((job) =>
    job.prompt.includes("按参考素材绘制静物"),
  );
  const pastedAsset = (await req("/assets")).data.items.find(
    (asset) => asset.name === "pasted-reference.png",
  );
  assert.ok(referenced.prompt.includes("参考图片1"));
  assert.ok(!referenced.prompt.includes("@图片2"));
  assert.ok(
    Object.values(referenced.params).some(
      (value) => Array.isArray(value) && value.includes(pastedAsset.id),
    ),
  );
  check(
    "pasted images and labelled references reach the generation API with correct asset ordering",
  );
  const editorBounds = await page.locator(".surface-composer").boundingBox();
  const previewBounds = await page.locator("#generation-preview").boundingBox();
  const settingsBounds = await page.locator(".studio-settings").boundingBox();
  const generateBounds = await page.locator(".studio-generate").boundingBox();
  assert.ok(editorBounds.x + editorBounds.width < settingsBounds.x);
  assert.ok(
    previewBounds.y >=
      Math.max(
        editorBounds.y + editorBounds.height,
        settingsBounds.y + settingsBounds.height,
      ),
  );
  assert.ok(generateBounds.y + generateBounds.height < 1000);
  check(
    "independent settings panel and gallery do not cover the editor or generation controls",
  );
  await page.locator(".surface-composer").evaluate((element, bytes) => {
    const transfer = new DataTransfer();
    transfer.items.add(
      new File([new Uint8Array(bytes)], "dropped-reference.png", {
        type: "image/png",
      }),
    );
    element.dispatchEvent(
      new DragEvent("drop", {
        dataTransfer: transfer,
        bubbles: true,
        cancelable: true,
      }),
    );
  }, Array.from(png));
  await page.getByRole("button", { name: "预览图片3", exact: true }).waitFor();
  assert.equal(await page.locator(".reference-item").count(), 2);
  await page.getByRole("button", { name: "移除图片3", exact: true }).click();
  check(
    "dropping reference files adds assets without replacing existing references",
  );

  const currentUser = (await req("/me")).data;
  await page.route("**/api/me", (route) =>
    route.fulfill({ json: { ...currentUser, balance: 0 } }),
  );
  await page.reload({ waitUntil: "networkidle" });
  await page
    .getByLabel("提示词", { exact: true })
    .fill("余额不足时快捷键也不提交");
  await page.locator(".surface-balance-warning").waitFor();
  let blockedSubmissions = 0;
  const onRequest = (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/api/jobs"
    )
      blockedSubmissions++;
  };
  page.on("request", onRequest);
  await page.getByLabel("提示词", { exact: true }).press("Control+Enter");
  await page.waitForTimeout(250);
  assert.equal(blockedSubmissions, 0);
  assert.equal(
    await page
      .getByRole("button", { name: "开始生成", exact: true })
      .isDisabled(),
    true,
  );
  page.off("request", onRequest);
  await page.unroute("**/api/me");
  await page.reload({ waitUntil: "networkidle" });
  check(
    "keyboard generation follows the same balance and readiness guard as the button",
  );
  for (const [path, name] of [
    ["generate", "创作"],
    ["history", "生成记录"],
    ["models", "模型与价格"],
    ["assets", "资产库"],
    ["developer", "开放平台"],
    ["tickets", "提交工单"],
    ["transactions", "资金流水"],
    ["redeem", "兑换充值"],
  ]) {
    await page
      .locator(".primary-nav,.service-nav")
      .getByRole("link", { name, exact: true })
      .click();
    await page.waitForURL("**/" + path);
    await page.getByRole("heading", { name, exact: true }).waitFor();
  }
  for (const path of ["/canvas", "/canvas/retired-project"]) {
    await page.goto(base + path);
    await page.waitForURL("**/generate");
    assert.equal(await page.locator('a[href^="/canvas"]').count(), 0);
  }
  await page.locator(".account-switch").click();
  await page
    .locator(".page-header")
    .getByRole("link", { name: "管理后台", exact: true })
    .click();
  await page.waitForURL(/\/admin(?:\/.*)?$/);
  check(
    "primary and account-service navigation, retired canvas redirects and account management access",
  );
  // Check preview file playback in a real media element.
  await page.goto(base + "/assets");
  const v = page.locator(".asset-preview video").first();
  await v.waitFor();
  await v.evaluate((video) => video.load());
  await page.waitForFunction(() => {
    const v = document.querySelector(".asset-preview video");
    return v && v.readyState >= 1;
  });
  check("video asset metadata loads in Chromium");
  // Small screen checks and the studio's dark theme.
  await page.setViewportSize({ width: 390, height: 844 });
  assert.deepEqual(
    await page.locator(".mobile-tabs a").allTextContents(),
    moduleNames,
  );
  for (const path of [
    "generate",
    "history",
    "models",
    "assets",
    "admin/prices",
    "profile",
    "developer",
    "tickets",
    "redeem",
    "transactions",
  ]) {
    await page.goto(base + "/" + path);
    await page.waitForTimeout(250);
    const width = await page.evaluate(() => document.body.scrollWidth);
    assert.ok(width <= 392, `${path} overflows: ${width}`);
    if (path === "generate")
      await page.screenshot({
        path: join(root, "ui-mobile-generate.png"),
        fullPage: true,
      });
  }
  await page.getByRole("button", { name: "打开导航", exact: true }).click();
  await page
    .locator(".workspace-menu")
    .getByRole("link", { name: "兑换充值", exact: true })
    .click();
  await page.waitForURL("**/redeem");
  await page.locator(".workspace-menu").waitFor({ state: "detached" });
  await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("button", { name: "浅色主题", exact: true }).click();
  assert.equal(await page.locator("html").getAttribute("data-theme"), "light");
  await page.screenshot({
    path: join(root, "ui-mobile-light.png"),
    fullPage: true,
  });
  check("mobile pages fit 390px and theme switch works");
  // Restart the Go API against the same MySQL database and Redis namespace.
  await stop();
  await start();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(base + "/history");
  await page.locator(".job-card").first().waitFor();
  assert.equal(await page.locator(".job-card").count(), 4);
  assert.equal((await req("/jobs/" + videoJob.id)).data.status, "succeeded");
  const persistedFile = await context.request.get(
    base + videoJob.outputs[0].url,
  );
  assert.equal(persistedFile.status(), 200);
  check(
    "sessions, generation records and generated media survive server restart",
  );
  await page.goto(ticketURL);
  await page
    .getByText("已核对，扣费与生成参数一致，可在资金流水查看。", {
      exact: true,
    })
    .waitFor();
  await page
    .locator(".ticket-detail-panel .ticket-state")
    .filter({ hasText: "已关闭" })
    .waitFor();
  check("ticket conversations and status survive backend restart");
  const regularContext = await browser.newContext({
    viewport: { width: 1440, height: 900 },
  });
  const regularResponse = await regularContext.request.post(
    base + "/api/auth/register",
    {
      data: {
        username: "ordinary-creator",
        password: "Ordinary-pass-2026",
        role: "admin",
      },
    },
  );
  assert.equal(regularResponse.status(), 200);
  const regularUser = await regularResponse.json();
  assert.equal(regularUser.role, "user");
  const regularPage = await regularContext.newPage();
  regularPage.on("pageerror", (error) => errors.push(error.message));
  let regularAdminRequests = 0;
  regularPage.on("request", (request) => {
    if (new URL(request.url()).pathname.startsWith("/api/admin/"))
      regularAdminRequests++;
  });
  await regularPage.goto(base + "/generate", { waitUntil: "networkidle" });
  assert.equal(
    await regularPage
      .getByRole("link", { name: "管理后台", exact: true })
      .count(),
    0,
  );
  await regularPage.goto(base + "/profile", { waitUntil: "networkidle" });
  assert.equal(
    await regularPage
      .getByRole("link", { name: "管理后台", exact: true })
      .count(),
    0,
  );
  for (const route of ["models", "codes", "jobs", "users"]) {
    await regularPage.goto(base + "/admin/" + route);
    await regularPage.waitForURL("**/generate");
  }
  assert.equal(regularAdminRequests, 0);
  for (const route of [
    "models",
    "codes",
    "jobs",
    "users",
    "transactions",
    "audit",
  ])
    assert.equal(
      (await regularContext.request.get(base + "/api/admin/" + route)).status(),
      403,
    );
  await regularPage.setViewportSize({ width: 390, height: 844 });
  await regularPage
    .getByRole("button", { name: "打开导航", exact: true })
    .click();
  assert.equal(
    await regularPage
      .getByRole("link", { name: "管理后台", exact: true })
      .count(),
    0,
  );
  check(
    "ordinary users cannot see desktop/mobile admin entries, open admin pages or access management APIs",
  );
  const regularKey = await (
    await regularContext.request.post(base + "/api/keys", {
      data: { name: "ordinary-app" },
    })
  ).json();
  const regularJobResponse = await regularContext.request.post(
    base + "/api/jobs",
    {
      headers: { Authorization: "Bearer " + regularKey.token },
      data: {
        modelId: "nanobanana",
        prompt: "Admin record inspection across users",
      },
    },
  );
  assert.equal(regularJobResponse.status(), 202);
  const regularJob = await regularJobResponse.json();
  // Windows terminates the test process immediately; allow the 30s Redis dispatcher lease to expire after restart.
  let regularJobStatus = regularJob.status;
  for (let attempt = 0; attempt < 180; attempt++) {
    const result = await (
      await regularContext.request.get(base + "/api/jobs/" + regularJob.id)
    ).json();
    regularJobStatus = result.status;
    if (regularJobStatus === "succeeded") break;
    assert.notEqual(result.status, "failed");
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  assert.equal(
    regularJobStatus,
    "succeeded",
    "Dispatcher must resume processing after restart",
  );
  await page.goto(base + "/generate", { waitUntil: "networkidle" });
  await page.locator(".topbar-admin").click();
  await page.getByRole("heading", { name: "管理后台", exact: true }).waitFor();
  await page
    .getByRole("navigation", { name: "后台管理导航" })
    .getByRole("link", { name: "全站调用", exact: true })
    .click();
  await chooseOption(page, "筛选用户", regularUser.id);
  await chooseOption(page, "调用来源", "api");
  await page
    .getByRole("button", { name: "查看任务 " + regularJob.id, exact: true })
    .waitFor();
  assert.equal(await page.locator(".admin-records tbody tr").count(), 1);
  await chooseOption(page, "任务状态", "failed");
  await page
    .getByRole("heading", { name: "暂无调用记录", exact: true })
    .waitFor();
  await chooseOption(page, "任务状态", "succeeded");
  await page
    .getByRole("button", { name: "查看任务 " + regularJob.id, exact: true })
    .click();
  const adminDetail = page.getByRole("dialog", {
    name: "调用详情",
    exact: true,
  });
  await adminDetail
    .getByText("ordinary-creator · ordinary-creator", { exact: true })
    .waitFor();
  await page.waitForFunction(() => {
    const img = document.querySelector(".modal .detail-media img");
    return img?.complete && img.naturalWidth > 0;
  });
  const protectedOutput = await adminDetail
    .locator(".detail-media img")
    .getAttribute("src");
  assert.equal(
    (await regularContext.request.get(base + protectedOutput)).status(),
    403,
  );
  await page.screenshot({
    path: join(root, "ui-admin-call-detail.png"),
    animations: "disabled",
  });
  await page.keyboard.press("Escape");
  check(
    "administrator can filter all-user API calls and inspect protected output without exposing admin access to the owner",
  );
  await req("/admin/codes", "POST", {
    count: 1,
    credits: 3,
    expiresAt: new Date(Date.now() - 3600000).toISOString(),
  });
  await page.goto(base + "/admin/codes", { waitUntil: "networkidle" });
  await page.getByRole("tab", { name: "已过期", exact: true }).click();
  await page.waitForFunction(
    () => document.querySelectorAll(".admin-page tbody tr").length === 1,
  );
  await page.getByRole("tab", { name: "已使用", exact: true }).click();
  await page
    .locator(".admin-page tbody tr")
    .filter({ hasText: "ui-admin" })
    .waitFor();
  check(
    "redemption management separates expired and redeemed codes and shows the redeemer",
  );
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "打开导航", exact: true }).click();
  await page
    .locator(".workspace-menu")
    .getByRole("link", { name: "管理后台", exact: true })
    .click();
  await page.getByRole("heading", { name: "管理后台", exact: true }).waitFor();
  await regularContext.close();
  const amountBefore=(await req("/me")).data.balance;
  const decimalCodes=(await req("/admin/codes","POST",{count:1,credits:0.25})).data;
  await page.goto(base+"/redeem",{waitUntil:"networkidle"});
  await page.getByRole("textbox",{name:"兑换码",exact:true}).fill(decimalCodes[0]);
  await page.getByRole("button",{name:"确认兑换",exact:true}).click();
  await page.getByText("兑换成功，已到账 ¥0.25",{exact:true}).waitFor();
  assert.equal((await req("/me")).data.balance,Math.round((amountBefore+0.25)*100)/100);
  assert.ok((await page.locator(".topbar-balance strong").innerText()).startsWith("¥"));
  await page.goto(base+"/transactions?kind=redeem",{waitUntil:"networkidle"});
  await page.locator(".ledger-panel tbody").getByText("+¥0.25",{exact:true}).waitFor();
  assert.equal((await req("/transactions")).data.summary.currency,"CNY");
  check("RMB prices retain concise decimals and decimal redemption credits the exact yuan amount");
  await req("/admin/models", "POST", {
    id: "ui-tier-pricing", name: "UI 分规格计费", type: "image", provider_id: "demo", upstream_model: "ui-tier-pricing",
    parameters: [
      {name:"resolution",label:"分辨率",type:"select",default:"1k",options:[{l:"1K",v:"1k"},{l:"2K",v:"2k"},{l:"4K",v:"4k"}]},
      {name:"quality",label:"质量",type:"select",default:"standard",options:[{l:"标准",v:"standard"},{l:"高清",v:"high"}]},
      {name:"n",label:"生成张数",type:"number",default:1,min:1,max:4},
    ],
  });
  await page.setViewportSize({width:1440,height:1000});
  await page.goto(base+"/admin/models?model=ui-tier-pricing&tab=pricing",{waitUntil:"networkidle"});
  const tierParent=page.getByRole("dialog",{name:"编辑模型",exact:true});
  await tierParent.getByRole("button",{name:"按规格定价",exact:true}).click();
  const tiers=page.getByRole("dialog",{name:"按规格定价",exact:true});
  assert.equal(await tiers.getByLabel("计费方式",{exact:true}).inputValue(),"request");
  assert.equal(await tiers.getByLabel("计费方式",{exact:true}).isDisabled(),true);
  assert.deepEqual(await tiers.getByLabel("计费方式",{exact:true}).locator("option").allTextContents(),["按次"]);
  for(const [spec,price] of [["1K · 标准",.108],["1K · 高清",.2],["2K · 标准",.2],["2K · 高清",.4],["4K · 标准",.4],["4K · 高清",.8]]) {
    await tiers.getByRole("spinbutton",{name:spec+" 单价（元）",exact:true}).fill(String(price));
  }
  await page.screenshot({path:join(root,"ui-specification-prices-desktop.png")});
  for(const width of [390,320]) {
    await page.setViewportSize({width,height:844});
    assert.equal(await tiers.evaluate(node=>node.scrollWidth>node.clientWidth),false);
    await page.screenshot({path:join(root,`ui-specification-prices-${width}.png`)});
  }
  await tiers.getByRole("button",{name:"保存规格价格",exact:true}).click();
  await tiers.waitFor({state:"hidden"});
  await page.waitForFunction(()=>document.querySelectorAll(".model-pricing-section tbody tr").length===6);
  const tierModel=(await req("/admin/models")).data.find(model=>model.id==="ui-tier-pricing");
  assert.equal(tierModel.prices.length,6);
  assert.ok(tierModel.prices.every(price=>price.unit==="request"));
  await page.goto(base+"/models",{waitUntil:"networkidle"});
  const tierCard=page.locator(".catalog-card").filter({has:page.getByRole("heading",{name:"UI 分规格计费",exact:true})});
  await tierCard.getByText("4K · 高清",{exact:true}).waitFor();
  await tierCard.getByText("¥0.8/次",{exact:true}).waitFor();
  await tierCard.getByText("每次生成 1 张，按次计费",{exact:true}).waitFor();
  await page.setViewportSize({width:1440,height:1000});
  await page.goto(base+"/generate?model=ui-tier-pricing",{waitUntil:"networkidle"});
  assert.ok((await page.getByRole("combobox",{name:"计费模式",exact:true}).innerText()).includes("按次"));
  assert.equal(await page.getByRole("combobox",{name:"计费模式",exact:true}).isDisabled(),true);
  await chooseOption(page,"分辨率","4k");
  await chooseOption(page,"质量","high");
  assert.equal(await page.getByLabel("生成张数",{exact:true}).count(),0);
  await page.waitForFunction(()=>document.querySelector(".pricing-quote-details")?.textContent.includes("¥0.8/次 × 1次 = ¥0.8"));
  await page.screenshot({path:join(root,"ui-specification-quote.png")});
  check("image resolution and quality prices save atomically and bill one request with image count fixed to one");
  await chooseOption(page,"分辨率","1k");
  await chooseOption(page,"质量","standard");
  await page.waitForFunction(()=>document.querySelector(".pricing-quote-details")?.textContent.includes("¥0.108/次 × 1次 = ¥0.11"));
  check("fractional request prices retain precision and round to fen once");
  assert.deepEqual(errors, []);
  await writeFile(
    join(root, "ui-results.json"),
    JSON.stringify(
      { passed: checks.length, checks, pageErrors: errors },
      null,
      2,
    ),
  );
  console.log(
    `Completed ${checks.length} browser workflow checks without page errors.`,
  );
} catch (error) {
  if (page)
    await page
      .screenshot({ path: join(root, "ui-failure.png"), fullPage: true })
      .catch(() => {});
  console.error(error);
  console.error("Browser errors:", errors);
  console.error("Server output:", output);
  process.exitCode = 1;
} finally {
  await browser?.close();
  await stop();
  execFileSync("go", ["run", "./cmd/testdb", "drop", database], {
    stdio: "pipe",
    windowsHide: true,
  });
}
