import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { mkdir, mkdtemp, writeFile } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { launchBrowser } from './browser.mjs';

const root = resolve('test-results/model-delete');
await mkdir(root, { recursive: true });
const dataDir = await mkdtemp(join(root, 'isolated-'));
const database = 'phantomframe_test_delete_' + Date.now();
const port = 4176, base = `http://127.0.0.1:${port}`;
let server, browser;
const checks = [], errors = [];
try {
  execFileSync('go', ['run', './cmd/testdb', 'create', database], { stdio: 'pipe', windowsHide: true });
  server = spawn(resolve('bin/phantomframe' + (process.platform === 'win32' ? '.exe' : '')), [], {
    env: { ...process.env, PORT: String(port), HOST: '127.0.0.1', APP_URL: base, DATA_DIR: dataDir, MYSQL_DATABASE: database, REDIS_PREFIX: database + ':', DEMO_MODE: 'true', ADMIN_USERNAME: 'delete-admin', ADMIN_PASSWORD: 'Delete-test-only-2026', ENCRYPTION_KEY: 'b'.repeat(64), APP_ENV: 'test', COOKIE_SECURE: 'false' },
    windowsHide: true, stdio: 'ignore',
  });
  let started = false;
  for (let i = 0; i < 60; i++) {
    try { if ((await fetch(base + '/api/health')).ok) { started = true; break; } } catch {}
    await new Promise(resolve => setTimeout(resolve, 150));
  }
  assert.ok(started, 'isolated API must start');
  browser = await launchBrowser();
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' });
  const req = async (path, method = 'GET', data) => {
    const response = await context.request.fetch(base + '/api' + path, { method, data });
    assert.ok(response.ok(), method + ' ' + path);
    return response.json();
  };
  await req('/auth/login', 'POST', { username: 'delete-admin', password: 'Delete-test-only-2026' });
  const provider = await req('/admin/providers', 'POST', { name: '删除流程测试渠道', protocol: 'demo' });
  const names = { 'delete-desktop': '删除测试桌面模型', 'delete-mobile': '删除测试手机模型', 'delete-retry': '删除重试模型', 'delete-history': '保留历史模型' };
  for (const [id, name] of Object.entries(names)) {
    await req('/admin/models', 'POST', { id, name, type: 'image', provider_id: provider.id, upstream_model: id });
    await req('/admin/prices', 'POST', { model_id: id, label: 'test', unit: 'request', credits: 0 });
  }
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  page.on('pageerror', error => errors.push(error.message));
  let mode = 'normal', deletes = 0;
  await page.route('**/api/admin/models/*', async route => {
    if (route.request().method() !== 'DELETE') return route.continue();
    deletes++;
    if (mode === 'fail') return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ message: '测试：删除服务暂不可用' }) });
    if (mode === 'slow') await new Promise(resolve => setTimeout(resolve, 700));
    return route.continue();
  });
  const open = async id => {
    await page.goto(base + '/admin/models', { waitUntil: 'networkidle' });
    await page.getByPlaceholder('搜索渠道、模型名称或 ID', { exact: true }).fill(id);
    await page.getByRole('button', { name: '删除 ' + names[id], exact: true }).click();
    const dialog = page.getByRole('dialog', { name: '删除模型', exact: true });
    await dialog.waitFor();
    return dialog;
  };
  for (const [width, id] of [[1440, 'delete-desktop'], [390, 'delete-mobile']]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 844 });
    let dialog = await open(id);
    const count = deletes;
    await dialog.getByRole('button', { name: '取消', exact: true }).click();
    await dialog.waitFor({ state: 'hidden' });
    assert.equal(deletes, count);
    assert.ok((await req('/admin/models')).some(model => model.id === id));
    await page.getByRole('button', { name: '删除 ' + names[id], exact: true }).click();
    dialog = page.getByRole('dialog', { name: '删除模型', exact: true });
    await page.screenshot({ path: join(root, `confirm-${width}.png`), animations: 'disabled' });
    mode = 'slow';
    await dialog.getByRole('button', { name: '删除模型', exact: true }).click();
    const pending = dialog.getByRole('button', { name: '正在删除…', exact: true });
    await pending.waitFor();
    assert.equal(await pending.isDisabled(), true);
    await page.keyboard.press('Escape');
    await dialog.waitFor();
    await pending.evaluate(button => button.click());
    await dialog.waitFor({ state: 'hidden' });
    assert.equal(deletes, count + 1);
    assert.equal(await page.getByRole('button', { name: '删除 ' + names[id], exact: true }).count(), 0);
    await page.getByRole('status').getByText('已删除模型「' + names[id] + '」', { exact: true }).waitFor();
    assert.ok(!(await req('/admin/models')).some(model => model.id === id));
    assert.ok(!(await req('/admin/prices')).some(price => price.model_id === id));
    const entry = (await req('/admin/providers/' + provider.id + '/models')).items.find(entry => entry.upstreamModel === id);
    assert.equal(entry.publishedModels.length, 0);
    checks.push({ width, cancelledWithoutDelete: true, deletedOnce: true, rowAndPricesRemoved: true, channelEntryPreserved: true });
  }
  mode = 'fail';
  let dialog = await open('delete-retry');
  await dialog.getByRole('button', { name: '删除模型', exact: true }).click();
  await dialog.getByRole('alert').getByText('测试：删除服务暂不可用', { exact: true }).waitFor();
  assert.ok((await req('/admin/models')).some(model => model.id === 'delete-retry'));
  mode = 'normal';
  await dialog.getByRole('button', { name: '重试删除', exact: true }).click();
  await dialog.waitFor({ state: 'hidden' });
  assert.ok(!(await req('/admin/models')).some(model => model.id === 'delete-retry'));
  checks.push({ failureVisibleInDialog: true, retryDeletesSuccessfully: true });
  const job = await req('/jobs', 'POST', { modelId: 'delete-history', prompt: 'isolated demo for history protection' });
  dialog = await open('delete-history');
  await dialog.getByRole('button', { name: '删除模型', exact: true }).click();
  await dialog.getByRole('alert').getByText('该模型有关联任务，请使用停用功能保留历史记录', { exact: true }).waitFor();
  assert.ok((await req('/admin/models')).some(model => model.id === 'delete-history'));
  assert.ok((await req('/jobs')).items.some(record => record.id === job.id));
  await page.screenshot({ path: join(root, 'history-protected.png'), animations: 'disabled' });
  checks.push({ historyProtected: true, reasonVisibleInDialog: true });
  assert.deepEqual(errors, []);
  await writeFile(join(root, 'report.json'), JSON.stringify({ checks, errors, isolatedDatabase: true }, null, 2));
  console.log(JSON.stringify({ checks, errors }));
} finally {
  await browser?.close();
  if (server && server.exitCode === null) {
    const stopped = new Promise(resolve => server.once('exit', resolve));
    server.kill();
    await stopped;
  }
  execFileSync('go', ['run', './cmd/testdb', 'drop', database], { stdio: 'pipe', windowsHide: true });
}
