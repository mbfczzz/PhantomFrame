import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
const dir = join(process.env.TEMP || ".", "phantom-reference");
await mkdir(dir, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH,
});
const page = await browser.newPage({
  viewport: { width: 1440, height: 1000 },
  deviceScaleFactor: 1,
});
await page.goto("https://aggc.site/generate", { waitUntil: "networkidle" });
await page.screenshot({ path: join(dir, "login.png"), fullPage: true });
await writeFile(join(dir, "login.txt"), await page.locator("body").innerText());
console.log(
  JSON.stringify({
    url: page.url(),
    screenshot: join(dir, "login.png"),
    text: await page.locator("body").innerText(),
  }),
);
// Render the publicly delivered client with local fixtures; no authenticated request is sent.
await page.route("**/api/**", async (route) => {
  const path = new URL(route.request().url()).pathname;
  let data = [];
  if (path.includes("/config/")) data = { value: "true" };
  if (path.endsWith("/users/me"))
    data = { id: 1, username: "创作者", prot_enabled: 1 };
  if (path.endsWith("/credits/summary"))
    data = { credits: 1000, frozen_credits: 0 };
  if (
    path.endsWith("/generate/jobs") ||
    path.endsWith("/canvas/projects") ||
    path.endsWith("/assets")
  )
    data = { list: [], total: 0, items: [] };
  await route.fulfill({ json: { code: 0, data, message: "local UI fixture" } });
});
await page.evaluate(() =>
  localStorage.setItem("access_token", "local-ui-fixture"),
);
for (const route of [
  "generate",
  "models",
  "canvas",
  "history",
  "assets",
  "developer",
  "profile",
]) {
  await page.goto("https://aggc.site/" + route, { waitUntil: "networkidle" });
  await page.screenshot({
    path: join(dir, route + "-fixture.png"),
    fullPage: true,
  });
  await writeFile(
    join(dir, route + "-fixture.txt"),
    await page.locator("body").innerText(),
  );
  console.log(route, (await page.locator("body").innerText()).slice(0, 2000));
}
await browser.close();
