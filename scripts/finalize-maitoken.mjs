import assert from 'node:assert/strict';import {readFile,writeFile,mkdir} from 'node:fs/promises';
const dir='test-results/maitoken',base='http://127.0.0.1:3001/api';
const connection=JSON.parse(await readFile(dir+'/connection.json','utf8'));
const access=JSON.parse(await readFile(dir+'/authenticated-models.json','utf8')).data;
const pricing=JSON.parse(await readFile(dir+'/account-pricing.json','utf8'));
const status=JSON.parse(await readFile(dir+'/_api_status.txt','utf8')).data;
assert.equal(status.quota_display_type,'CNY');assert.equal(pricing.success,true);
const sourceRows=access.map(model=>pricing.data.find(row=>row.model_name===model.id));assert.ok(sourceRows.every(Boolean));
const groups=sourceRows.reduce((common,row)=>common===null?row.enable_groups:common.filter(g=>row.enable_groups.includes(g)),null);assert.equal(groups.length,1,'Cannot determine the applicable group');const group=groups[0],ratio=Number(pricing.group_ratio[group]);assert.ok(ratio>0);
const sourceRules=new Map();const markup=value=>Math.round(value*ratio*1.2*1e6)/1e6;
for(const row of sourceRows){
 assert.equal(row.billing_mode,'resolution_tiered');const p=row.resolution_pricing;assert.equal(p.version,'v1');assert.equal(p.billing_unit,'second');
 const ref=p.reference_billing;assert.equal(ref.version,'v1');assert.equal(ref.billing_unit,'second');assert.equal(ref.reference_duration_mode,'ignore');assert.equal(ref.include_output_duration,true);assert.equal(Object.keys(ref.minimum_duration_table||{}).length,0);
 const info=row.reference_video_billing;assert.equal(info.status,'known');assert.equal(info.charged,true);assert.equal(info.mode,'per_second');assert.equal(info.unit,'second');assert.equal(info.duration_mode,'ignore');assert.equal(info.include_output_duration,true);
 const rules=[];for(const [resolution,tier]of Object.entries(p.tiers)){
  if(!tier.enabled)continue;assert.equal(typeof tier.normal,'number');assert.equal(typeof tier.reference_video,'number');assert.ok(tier.normal>=0&&tier.reference_video>=0);
  assert.equal(info.tiers.find(t=>t.resolution===resolution)?.amount,tier.reference_video);
  rules.push({label:resolution,unit:'second',credits:markup(tier.normal),conditions:{resolution},extras:{videoOutputSecondPrice:markup(tier.reference_video)},enabled:true,sourceCostCNY:tier.normal*ratio,sourceVideoExtraCNY:tier.reference_video*ratio});
 }assert.ok(rules.length);sourceRules.set(row.model_name,rules);
}
const login=await fetch(base+'/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:await readFile('.local-credentials.json','utf8')});assert.ok(login.ok);const cookie=login.headers.get('set-cookie').split(';')[0];
const call=async(path,method='GET',body)=>{const res=await fetch(base+path,{method,headers:{Cookie:cookie,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});const data=await res.json();if(!res.ok)throw new Error(data.message||`HTTP ${res.status}`);return data;};
try{
 const before=await call('/admin/models'),provider=(await call('/admin/providers')).find(p=>p.id===connection.providerId);assert.ok(provider?.hasKey);
 const report={createdAt:new Date().toISOString(),providerId:provider.id,group,groupRatio:ratio,markupPercent:20,pricingSource:'经登录账号核对 resolution_tiered 完整规则；价格导出接口暂未覆盖此模式',models:[],skippedExistingPrices:[]};
 await mkdir('data/maintenance',{recursive:true});const reportPath='data/maintenance/maitoken-integration.json';await writeFile(reportPath,JSON.stringify(report,null,2));
 await call('/admin/providers/'+provider.id,'PUT',{...provider,protocol:'maitoken',apiKey:'',enabled:true,config:{...provider.config,integrationStatus:'ready',modelsVerified:true,generationVerified:false,pricingGroup:group,pricingGroupRatio:ratio,pricingSource:report.pricingSource,pricingStatus:'verified_account_rules',markupPercent:20}});
 const tested=await call('/admin/providers/'+provider.id+'/test','POST');assert.equal(tested.ok,true);
 const synced=await call('/admin/providers/'+provider.id+'/sync-models','POST');const entries=(await call('/admin/providers/'+provider.id+'/models?limit=100')).items;assert.equal(entries.length,access.length);
 const plan={version:1,markupPercent:20,models:[],pending:[],skippedTiers:[],conversions:{maitoken:'CNY，2.5官转组倍率1，普通单价和参考视频场景附加单价分别乘1.2；忽略输入视频时长，仅按输出时长计附加费'}};
 for(const entry of entries){
  const published=await call(`/admin/providers/${provider.id}/models/${entry.id}/publish`,'POST',{modelId:(provider.id.slice(0,8)+':'+entry.upstreamModel.replace(/[^a-zA-Z0-9_.:-]/g,'-')).slice(0,100),name:Array.from('m-'+entry.name).slice(0,120).join(''),type:entry.type,category:'默认分组'});
  let model=published.model;const rules=sourceRules.get(entry.upstreamModel);assert.ok(rules);
  if(!model.prices.length)model=await call('/admin/models/'+encodeURIComponent(model.id)+'/prices','PUT',{expectedVersion:model.pricingVersion,prices:rules});else report.skippedExistingPrices.push(model.id);
  plan.models.push({id:model.id,model,rules:model.prices,sourceUrl:'https://api.mai-token.com/api/pricing'});
  report.models.push({id:model.id,upstream:entry.upstreamModel,alias:model.name,enabled:model.enabled,rules:rules.map(r=>({resolution:r.conditions.resolution,sourcePrice:r.sourceCostCNY,unitPrice:r.credits,sourceReferenceExtra:r.sourceVideoExtraCNY,referenceExtraPerOutputSecond:r.extras.videoOutputSecondPrice}))});await writeFile(reportPath,JSON.stringify(report,null,2));
 }
 const after=await call('/admin/models');for(const original of before.filter(m=>m.provider_id!==provider.id))assert.deepEqual(after.find(m=>m.id===original.id),original,'Existing model configuration changed');
 await writeFile('data/maintenance/maitoken-price-verification.json',JSON.stringify(plan,null,2));
 console.log(JSON.stringify({connected:true,synced,...report}));
}finally{await call('/auth/logout','POST').catch(()=>{});}
