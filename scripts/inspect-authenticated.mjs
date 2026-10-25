import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
if (process.stdin.isTTY) process.stdin.setRawMode(true);
process.stdout.write(
  "Ready for credentials JSON on stdin (input is hidden).\n",
);
const credentials = await new Promise((resolve) => {
  let input = "";
  process.stdin.on("data", (chunk) => {
    input += chunk.toString();
    if (input.includes("\n") || input.includes("\r"))
      resolve(JSON.parse(input.trim()));
  });
});
process.stdin.pause();
const dir = join(process.env.TEMP, "phantom-reference");
await mkdir(dir, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH,
});
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
page.on("response", async (res) => {
  if (/\/api\/v1\/models(?:\?|$)/.test(res.url()) && res.ok()) {
    await writeFile(
      join(dir, "models-authenticated.json"),
      JSON.stringify(await res.json(), null, 2),
    );
  }
});
await page.goto("https://aggc.site/login", { waitUntil: "networkidle" });
await page.locator("input").nth(0).fill(credentials.username);
await page.locator("input[type=password]").fill(credentials.password);
await page.getByRole("button", { name: "登 录" }).click();
await page.waitForURL("**/generate", { timeout: 30000 });
for (const path of [
  "generate",
  "models",
  "canvas",
  "history",
  "assets",
  "developer",
  "profile",
]) {
  if (path !== "generate") {
    const label = {
      models: "模型",
      canvas: "画布",
      history: "历史",
      assets: "资产库",
      developer: "开放平台",
      profile: "个人中心",
    }[path];
    await page
      .locator(".sidebar")
      .getByRole("link", { name: label, exact: true })
      .click()
      .catch(() => page.goto("https://aggc.site/" + path));
  }
  await page.waitForTimeout(1400);
  await page.screenshot({
    path: join(dir, path + "-authenticated.png"),
    fullPage: true,
  });
  await writeFile(
    join(dir, path + "-authenticated.txt"),
    await page.locator("body").innerText(),
  );
  console.log("Captured " + path);
}
await browser.close();
process.exit(0);
