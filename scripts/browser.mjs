import { chromium } from "playwright";
import { existsSync, readdirSync } from "node:fs";
import { join } from "node:path";
export async function launchBrowser() {
  let executablePath = process.env.PLAYWRIGHT_EXECUTABLE_PATH;
  if (
    !executablePath &&
    !existsSync(chromium.executablePath()) &&
    process.platform === "win32"
  ) {
    const root = join(process.env.LOCALAPPDATA, "ms-playwright");
    if (existsSync(root))
      for (const version of readdirSync(root)
        .filter((v) => v.startsWith("chromium_headless_shell-"))
        .sort()
        .reverse()) {
        const file = join(
          root,
          version,
          "chrome-headless-shell-win64",
          "chrome-headless-shell.exe",
        );
        if (existsSync(file)) {
          executablePath = file;
          break;
        }
      }
  }
  return chromium.launch({
    headless: true,
    executablePath,
    args: ["--disable-gpu"],
  });
}
