import { mkdir, readFile, writeFile } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";
const root = "test-results/studio";
await mkdir(root, { recursive: true });
const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const browser = await launchBrowser();
const context = await browser.newContext({
  viewport: { width: 1440, height: 1080 },
});
const page = await context.newPage();
const errors = [],
  checks = [];
page.on("pageerror", (e) => errors.push(e.message));
try {
  await page.goto(base + "/login", { waitUntil: "networkidle" });
  await page.screenshot({ path: root + "/login.png", fullPage: true });
  const credentials = JSON.parse(
    await readFile(".local-credentials.json", "utf8"),
  );
  const login = await context.request.post(base + "/api/auth/login", {
    data: credentials,
  });
  if (!login.ok()) throw new Error("Local preview login failed");
  for (const route of [
    "generate",
    "models",
    "admin/prices",
    "profile",
    "assets",
    "canvas",
    "developer",
  ]) {
    await page.goto(base + "/" + route, { waitUntil: "networkidle" });
    await page.screenshot({
      path: root + "/" + route.replaceAll("/", "-") + ".png",
      fullPage: true,
    });
    checks.push({
      route,
      width: 1440,
      overflow: await page.evaluate(
        () => document.body.scrollWidth > innerWidth,
      ),
    });
  }
  for (const width of [390, 768, 1024]) {
    await page.setViewportSize({ width, height: 844 });
    for (const route of ["generate", "models", "admin/prices"]) {
      await page.goto(base + "/" + route, { waitUntil: "networkidle" });
      await page.screenshot({
        path: root + "/" + route.replaceAll("/", "-") + "-" + width + ".png",
        fullPage: true,
      });
      checks.push({
        route,
        width,
        overflow: await page.evaluate(
          () => document.body.scrollWidth > innerWidth,
        ),
      });
    }
  }
  await page.goto(base + "/models", { waitUntil: "networkidle" });
  await page.getByRole("tab", { name: "图片", exact: true }).click();
  await page.waitForFunction(
    () => document.querySelectorAll(".catalog-card").length === 4,
  );
  await page.getByRole("button", { name: "默认分组", exact: true }).click();
  await page
    .getByRole("textbox", { name: "搜索模型名称、ID 或渠道" })
    .fill("GPT");
  await page.waitForFunction(
    () => document.querySelectorAll(".catalog-card").length === 1,
  );
  await page.goto(base + "/generate", { waitUntil: "networkidle" });
  await page.getByRole("button", { name: "试试灵感", exact: true }).click();
  if (
    !(
      await page
        .getByRole("textbox", { name: "提示词", exact: true })
        .inputValue()
    ).length
  )
    throw new Error("Prompt suggestion did not fill the editor");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(base + "/login", { waitUntil: "networkidle" });
  await page.screenshot({ path: root + "/login-390.png", fullPage: true });
  await page.goto(base + "/register", { waitUntil: "networkidle" });
  await page.screenshot({ path: root + "/register-390.png", fullPage: true });
  await page.setViewportSize({ width: 1440, height: 1080 });
  await page.evaluate(() => localStorage.setItem("pf-studio-theme", "dark"));
  await page.goto(base + "/generate", { waitUntil: "networkidle" });
  await page.screenshot({ path: root + "/generate-dark.png", fullPage: true });
  await writeFile(
    root + "/review.json",
    JSON.stringify({ checks, errors }, null, 2),
  );
  console.log(JSON.stringify({ checks, errors }));
  if (errors.length || checks.some((c) => c.overflow)) process.exitCode = 1;
} finally {
  await browser.close();
}
