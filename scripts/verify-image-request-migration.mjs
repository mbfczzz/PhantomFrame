import assert from 'node:assert/strict';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { request } from 'playwright';
const directory = 'data/maintenance';
await mkdir(directory, { recursive: true });
const client = await request.newContext({ baseURL: 'http://127.0.0.1:3001' });
try {
  assert.ok((await client.post('/api/auth/login', { data: JSON.parse(await readFile('.local-credentials.json', 'utf8')) })).ok());
  const get = async path => { const r = await client.get('/api' + path); assert.ok(r.ok(), path); return r.json(); };
  const models = await get('/admin/models');
  const accounts = await get('/admin/users'), ledger = await get('/admin/transactions'), jobs = await get('/admin/jobs');
  const providers = await get('/admin/providers');
  const path = directory + '/image-request-before.json';
  if (process.argv.includes('--snapshot')) {
    await writeFile(path, JSON.stringify({ models, accounts, ledger, jobs, providers }, null, 2), { flag: 'wx' });
    console.log(JSON.stringify({ snapshot: true, imageModels: models.filter(m => m.type === 'image').length, rulesToConvert: models.filter(m => m.type === 'image').flatMap(m => m.prices).filter(p => p.unit === 'image').length }));
  } else {
    const before = JSON.parse(await readFile(path, 'utf8'));
    assert.deepEqual(accounts, before.accounts);
    assert.deepEqual(ledger, before.ledger);
    assert.deepEqual(jobs, before.jobs);
    assert.deepEqual(providers, before.providers);
    assert.equal(models.length, before.models.length);
    let converted = 0, normalized = 0;
    const names = new Set(['n','image_count','num_images','numImages','batch_size','imageCount','num_outputs','count']);
    for (const old of before.models) {
      const current = models.find(model => model.id === old.id);
      if (old.type !== 'image') { assert.deepEqual(current, old); continue; }
      const expected = structuredClone(old);
      delete expected.pricingVersion;
      expected.maxOutputsPerRequest = /(^|[/:._-])(midjourney|mj)($|[/:._-])/i.test(old.upstream_model) ? 4 : 1;
      expected.description = expected.description.replaceAll('按张计费','按次计费').replace(/每次生成\s*\d+\s*[–～~-]\s*\d+\s*张/g,'每次生成 1 张');
      expected.parameters = expected.parameters.map(p => {
        if (p.type === 'assets' || !names.has(p.name)) return p;
        const next = { ...p, type: 'number', default: 1, min: 1, max: 1, integer: true };
        delete next.options;
        normalized++;
        return next;
      });
      for (const price of expected.prices) if (price.unit === 'image') { price.unit = 'request'; converted++; }
      const actual = structuredClone(current);
      delete actual.pricingVersion;
      assert.deepEqual(actual, expected, old.id);
      assert.ok(current.prices.every(price => price.unit === 'request'));
    }
    const report = { verifiedAt: new Date().toISOString(), convertedPrices: converted, fixedCountParameters: normalized, models: models.length, amountsAndSpecificationsPreserved: true, videosPreserved: true, accountsAndHistoryPreserved: true, providerSettingsPreserved: true };
    await writeFile(directory + '/image-request-verification.json', JSON.stringify(report, null, 2));
    console.log(JSON.stringify(report));
  }
} finally { await client.dispose(); }
