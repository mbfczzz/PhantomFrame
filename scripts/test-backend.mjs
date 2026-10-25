import { spawn } from "node:child_process";
const child = spawn("go", ["test", "./...", "-count=1", "-v"], { stdio: "inherit", windowsHide: true, env: { ...process.env, PF_INTEGRATION: "1" } });
child.on("error", error => { console.error(error.message); process.exitCode = 1; });
child.on("exit", code => { process.exitCode = code ?? 1; });
