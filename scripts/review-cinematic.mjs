import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";
import { chooseOption } from "./ui-controls.mjs";
const root = process.env.REVIEW_OUTPUT || "test-results/product";
await mkdir(root, { recursive: true });
const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const browser = await launchBrowser();
const context = await browser.newContext({
  viewport: { width: 1440, height: 1000 },
});
const page = await context.newPage();
const errors = [],
  checks = [];
page.on("pageerror", (error) => errors.push(error.message));
const dimensions = [
  [1440, 1000],
  [1440, 900],
  [1366, 768],
  [1024, 768],
  [768, 1024],
  [390, 844],
  [390, 740],
  [360, 640],
  [320, 568],
];
try {
  await page.goto(base + "/login", { waitUntil: "networkidle" });
  assert.equal(await page.locator("html").getAttribute("data-theme"), "dark");
  const type = await page.evaluate(() => ({
    input: parseFloat(
      getComputedStyle(document.querySelector("input")).fontSize,
    ),
    button: parseFloat(
      getComputedStyle(document.querySelector(".auth-submit")).fontSize,
    ),
    hero: document.querySelector(".auth-hero-image").naturalWidth,
  }));
  assert.ok(type.input >= 15);
  assert.ok(type.button >= 16);
  assert.ok(type.hero > 0);
  checks.push({ page: "login", ...type });
  for (const [width, height] of dimensions.filter(
    ([w]) => w === 1440 || w <= 390,
  )) {
    await page.setViewportSize({ width, height });
    await page.screenshot({
      path: `${root}/login-${width}-${height}.png`,
      fullPage: false,
    });
    const bounds = await page.locator(".auth-submit").boundingBox();
    checks.push({
      page: "login",
      width,
      height,
      buttonBottom: Math.round(bounds.y + bounds.height),
      visible: bounds.y + bounds.height <= height - 8,
    });
  }
  const credentials = JSON.parse(
    await readFile(".local-credentials.json", "utf8"),
  );
  assert.ok(
    (
      await context.request.post(base + "/api/auth/login", {
        data: credentials,
      })
    ).ok(),
  );
  for (const [width, height] of dimensions) {
    await page.setViewportSize({ width, height });
    await page.goto(base + "/generate", { waitUntil: "networkidle" });
    for (const mode of ["image", "video"]) {
      await chooseOption(page, "生成类型", mode);
      await page
        .getByRole("textbox", { name: "提示词", exact: true })
        .fill(
          "一座悬浮在云层中的未来城市，紫蓝色光线勾勒建筑轮廓，电影级光影，广角镜头。",
        );
      await page.waitForFunction(
        () => !document.querySelector(".studio-generate").disabled,
      );
      await page.waitForTimeout(200);
      const bounds = await page.locator(".studio-generate").boundingBox();
      const overflow = await page.evaluate(
        () => document.body.scrollWidth > innerWidth,
      );
      const bottom = Math.round(bounds.y + bounds.height),
        limit = height - 10;
      checks.push({
        page: "studio",
        width,
        height,
        mode,
        buttonBottom: bottom,
        limit,
        visible: bottom <= limit,
        overflow,
      });
      await page.screenshot({
        path: `${root}/studio-${mode}-${width}-${height}.png`,
        fullPage: false,
      });
    }
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  for (const route of [
    "models",
    "admin/prices",
    "profile",
    "history",
    "assets",
    "developer",
  ]) {
    await page.goto(base + "/" + route, { waitUntil: "networkidle" });
    await page.screenshot({
      path: root + "/" + route.replaceAll("/", "-") + ".png",
      fullPage: false,
    });
  }
  await page.evaluate(() =>
    localStorage.setItem("pf-cinematic-theme-v3", "light"),
  );
  await page.goto(base + "/generate", { waitUntil: "networkidle" });
  await page.screenshot({ path: root + "/studio-light.png", fullPage: false });
  assert.equal(await page.locator("html").getAttribute("data-theme"), "light");
  const report = { checks, errors };
  await writeFile(root + "/report.json", JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report));
  assert.deepEqual(errors, []);
  assert.ok(
    checks.every((c) => c.visible !== false && !c.overflow),
    "Primary actions must fit the first viewport.",
  );
} finally {
  await browser.close();
}
