import assert from 'node:assert/strict';
import { readFile, mkdir, writeFile } from 'node:fs/promises';

if (process.stdin.isTTY) process.stdin.setRawMode(true);
process.stdout.write('READY: waiting for OSS credentials JSON on hidden stdin\n');
let input = '';
const supplied = await new Promise((resolve, reject) => {
  process.stdin.on('data', chunk => {
    input += chunk.toString();
    if (/[\r\n]/.test(input)) {
      process.stdin.pause();
      try { resolve(JSON.parse(input.trim())); } catch { reject(new Error('Invalid credential input')); }
    }
  });
});
if (process.stdin.isTTY) process.stdin.setRawMode(false);
input = '';
const base = 'http://127.0.0.1:3001/api';
const login = await fetch(base + '/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: await readFile('.local-credentials.json', 'utf8') });
assert.ok(login.ok);
const cookie = login.headers.get('set-cookie').split(';')[0];
const api = async (path, method = 'GET', body) => {
  const response = await fetch(base + path, { method, headers: { Cookie: cookie, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(60000) });
  const result = await response.json();
  if (!response.ok) throw new Error(result.message || `HTTP ${response.status}`);
  return result;
};
try {
  const current = await api('/admin/storage');
  const config = { ...current, enabled: true, bucket: 'flowlinght', region: 'cn-shanghai', endpoint: 'https://oss-cn-shanghai.aliyuncs.com', accelerateEndpoint: 'https://flowlinght.oss-accelerate.aliyuncs.com', prefix: 'phantomframe/uploads/', uploadAccelerate: true, readAccelerate: true, cdnDomain: supplied.cdnDomain, accessKeyId: supplied.accessKeyId, accessKeySecret: supplied.accessKeySecret, publicRead: true, urlTTL: 3600 };
  assert.ok(config.accessKeyId && config.accessKeySecret && config.cdnDomain);
  let test;
  try { test = await api('/admin/storage/test', 'POST', config); }
  catch (error) {
    if (!error.message.includes('OSS 上传成功')) throw error;
    config.publicRead = false;
    test = await api('/admin/storage/test', 'POST', config);
  }
  const saved = await api('/admin/storage', 'PUT', config);
  assert.ok(saved.enabled && saved.hasCredentials);
  assert.equal(saved.cdnDomain, config.cdnDomain);
  for (const path of ['/config', '/admin/settings', '/admin/storage']) {
    const body = JSON.stringify(await api(path));
    assert.ok(!body.includes(config.accessKeySecret) && !body.includes(config.accessKeyId), 'credential disclosure');
  }
  const report = { configuredAt: new Date().toISOString(), ...saved, verification: test.message, supplierGenerationCalls: 0 };
  await mkdir('data/maintenance', { recursive: true });
  await writeFile('data/maintenance/oss-integration.json', JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report));
  config.accessKeyId = config.accessKeySecret = '';
} finally { supplied.accessKeyId = supplied.accessKeySecret = ''; }
