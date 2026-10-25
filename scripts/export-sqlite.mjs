// One-time migration utility only; the application runtime is Go + MySQL + Redis.
import { DatabaseSync } from "node:sqlite";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
const dataDir = resolve(process.env.DATA_DIR || "data");
const target = resolve(process.argv[2] || join(dataDir, "migration", "sqlite-export.json"));
await mkdir(resolve(target, ".."), { recursive: true });
const db = new DatabaseSync(join(dataDir, "phantomframe.sqlite"), { readOnly: true });
try {
  const active = db.prepare("SELECT count(*) n FROM jobs WHERE status IN ('queued','running')").get().n;
  if (active) throw new Error("Wait for generation jobs to finish, then stop the Node API before exporting.");
  const backup = join(resolve(target, ".."), `phantomframe-${Date.now()}.sqlite`);
  db.exec(`VACUUM INTO '${backup.replaceAll("'", "''")}'`);
  const tables = {};
  db.exec("BEGIN");
  for (const name of ["users", "providers", "models", "prices", "jobs", "transactions", "assets", "projects", "api_keys", "redeem_codes", "settings", "migrations", "audit_log", "sessions"]) {
    tables[name] = db.prepare(`SELECT * FROM ${name === "settings" ? '"settings"' : name} ORDER BY rowid`).all();
  }
  db.exec("COMMIT");
  await writeFile(target, JSON.stringify({ version: 1, exportedAt: new Date().toISOString(), tables }), { mode: 0o600 });
  console.log(JSON.stringify({ export: target, backup, counts: Object.fromEntries(Object.entries(tables).map(([name, rows]) => [name, rows.length])) }));
} finally { db.close(); }
