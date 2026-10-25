import { spawn } from "node:child_process";
import { resolve } from "node:path";
const binary = resolve(`bin/phantomframe${process.platform === "win32" ? ".exe" : ""}`);
const child = spawn(binary, [], { stdio: "inherit", windowsHide: true });
for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => child.kill(signal));
child.on("error", error => { console.error(`${error.message}\nRun npm run build:server first.`); process.exitCode = 1; });
child.on("exit", code => { process.exitCode = code ?? 0; });
