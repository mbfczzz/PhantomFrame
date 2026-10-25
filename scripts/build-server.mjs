import { mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
await mkdir("bin", { recursive: true });
const output = `bin/phantomframe${process.platform === "win32" ? ".exe" : ""}`;
const child = spawn("go", ["build", "-trimpath", "-o", output, "./cmd/api"], { stdio: "inherit", windowsHide: true });
child.on("error", error => { console.error(error.message); process.exitCode = 1; });
child.on("exit", code => { process.exitCode = code ?? 1; });
