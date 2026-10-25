import { chromium } from "playwright";
import { mkdir, readFile } from "node:fs/promises";
await mkdir("test-results", { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH,
  args: ["--disable-gpu"],
});
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const errors = [];
page.on("pageerror", (e) => errors.push(e.message));
await page.goto("http://127.0.0.1:5173/login", { waitUntil: "networkidle" });
await page.screenshot({ path: "test-results/login.png", fullPage: true });
const creds = JSON.parse(await readFile(".local-credentials.json", "utf8"));
await page
  .getByRole("textbox", { name: "用户名", exact: true })
  .fill(creds.username);
await page.getByLabel("密码", { exact: true }).fill(creds.password);
await page.getByRole("button", { name: "登录", exact: true }).click();
await page.waitForURL("**/generate");
await page.waitForTimeout(800);
await page.screenshot({ path: "test-results/generate.png", fullPage: true });
console.log("Generate loaded:", await page.locator("h1").innerText());
for (const path of [
  "models",
  "admin/models",
  "admin/prices",
  "canvas",
  "profile",
  "developer",
  "assets",
  "history",
]) {
  await page.goto("http://127.0.0.1:5173/" + path, {
    waitUntil: "networkidle",
  });
  await page.screenshot({
    path: "test-results/" + path.replaceAll("/", "-") + ".png",
    fullPage: true,
  });
  console.log(path, await page.locator("h1").first().innerText());
}
await page.setViewportSize({ width: 390, height: 844 });
for (const path of ["generate", "models", "admin/prices", "profile"]) {
  await page.goto("http://127.0.0.1:5173/" + path, {
    waitUntil: "networkidle",
  });
  await page.screenshot({
    path: "test-results/mobile-" + path.replaceAll("/", "-") + ".png",
    fullPage: true,
  });
  const width = await page.evaluate(() => ({
    body: document.body.scrollWidth,
    viewport: window.innerWidth,
  }));
  console.log("Mobile", path, width);
}
console.log("Page errors:", errors);
await browser.close();
if (errors.length) process.exitCode = 1;
