import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir} from 'node:fs/promises';
const base='http://127.0.0.1:3001/api',dir='test-results/rolldek';
const login=await fetch(base+'/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:await readFile('.local-credentials.json','utf8')});assert.ok(login.ok);const cookie=login.headers.get('set-cookie').split(';')[0];
const call=async(path,method='GET',body)=>{const r=await fetch(base+path,{method,headers:{Cookie:cookie,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});const data=await r.json();if(!r.ok)throw new Error(data.message||`HTTP ${r.status}`);return data;};
try{
 const providers=await call('/admin/providers'),beforeModels=await call('/admin/models');
 const provider=providers.find(p=>p.config?.vendor==='rolldek');assert.ok(provider?.hasKey,'Rolldek key must be registered first');
 const keyCatalog=JSON.parse(await readFile(dir+'/authenticated-models.json','utf8')).data;
 const keyMetadata=JSON.parse(await readFile(dir+'/key-group-check.json','utf8'));assert.equal(keyMetadata.find(r=>r.path==='/api/usage/token/')?.model_limits_enabled,false,'Explicit model limits require checking the actual key group');
 const pricingResponse=await fetch('https://www.rolldek.com/api/pricing',{redirect:'manual',signal:AbortSignal.timeout(20000)});assert.ok(pricingResponse.ok);const pricing=await pricingResponse.json();assert.equal(pricing.success,true);
 const statusResponse=await fetch('https://www.rolldek.com/api/status',{redirect:'manual',signal:AbortSignal.timeout(20000)});assert.ok(statusResponse.ok);const status=(await statusResponse.json()).data;
 const rows=keyCatalog.map(m=>pricing.data.find(row=>row.model_name===m.id));assert.ok(rows.every(Boolean));
 const groups=rows.reduce((common,row)=>common===null?row.enable_groups:common.filter(group=>row.enable_groups.includes(group)),null);
 assert.equal(groups.length,1,'Cannot infer a unique billing group from the visible model catalog');
 const group=groups[0],groupRatio=Number(pricing.group_ratio[group]),cnyPerCredit=Number(status.price);
 assert.ok(groupRatio>0&&cnyPerCredit>0);assert.ok(rows.every(row=>row.quota_type===1&&row.billing_mode==='per_call'));
 const report={createdAt:new Date().toISOString(),providerId:provider.id,group,groupBasis:'唯一可见模型分组交集；Key 未启用模型白名单',groupRatio,cnyPerCredit,displayExchangeRate:status.usd_exchange_rate,markupPercent:20,models:[],skippedExistingPrices:[]};
 await mkdir('data/maintenance',{recursive:true});const reportPath='data/maintenance/rolldek-integration.json';
 await writeFile(reportPath,JSON.stringify({...report,beforeModels:beforeModels.filter(m=>m.provider_id===provider.id)},null,2));
 await call('/admin/providers/'+provider.id,'PUT',{...provider,protocol:'rolldek',enabled:true,apiKey:'',config:{...provider.config,integrationStatus:'ready',modelsVerified:true,generationVerified:false,pricingGroup:group,pricingGroupBasis:report.groupBasis,pricingGroupRatio:groupRatio,cnyPerCredit,markupPercent:20}});
 const connection=await call('/admin/providers/'+provider.id+'/test','POST');assert.equal(connection.ok,true);
 const sync=await call('/admin/providers/'+provider.id+'/sync-models','POST');
 const entries=(await call('/admin/providers/'+provider.id+'/models?limit=100')).items;assert.equal(entries.length,keyCatalog.length,'All accessible models must have verified adapter templates');
 const pricePlan={version:1,markupPercent:20,models:[],pending:[],skippedTiers:[],conversions:{rolldek:`站内充值系数 ${cnyPerCredit} 元/额度，分组 ${group} 倍率 ${groupRatio}，单价再乘 1.2`}};
 for(const entry of entries){
  const source=pricing.data.find(row=>row.model_name===entry.upstreamModel);assert.ok(source?.model_price>0);
  const cost=Number((source.model_price*groupRatio*cnyPerCredit).toFixed(6));const credits=Number((Math.round(cost*1e6)*1.2/1e6).toFixed(6));
  const publish=await call(`/admin/providers/${provider.id}/models/${entry.id}/publish`,'POST',{modelId:(provider.id.slice(0,8)+':'+entry.upstreamModel.replace(/[^a-zA-Z0-9_.:-]/g,'-')).slice(0,100),name:Array.from('r-'+entry.name).slice(0,120).join(''),type:entry.type,category:'默认分组'});
  let model=publish.model;
  const dimensions=model.parameters.filter(p=>p.type==='select'&&['size','resolution','quality','thinkingLevel'].includes(p.name));
  let variants=[{conditions:{},labels:[]}];for(const p of dimensions)variants=variants.flatMap(row=>p.options.map(option=>({conditions:{...row.conditions,[p.name]:option.v??option},labels:[...row.labels,String(option.l??option)]})));
  const rules=variants.map(row=>({label:(row.labels.join(' · ')||'标准价格').slice(0,80),unit:'request',credits,conditions:row.conditions,extras:{},enabled:true,sourceCostCNY:cost}));
  assert.ok(rules.length<=200);
  if(!model.prices.length)model=await call('/admin/models/'+encodeURIComponent(model.id)+'/prices','PUT',{expectedVersion:model.pricingVersion,prices:rules});else report.skippedExistingPrices.push(model.id);
  pricePlan.models.push({id:model.id,model,rules:model.prices,sourceUrl:'https://www.rolldek.com/api/pricing'});
  report.models.push({id:model.id,upstream:entry.upstreamModel,alias:model.name,costCNY:cost,unitPriceCNY:credits,unit:'request',rules:model.prices.length,enabled:model.enabled});
  await writeFile(reportPath,JSON.stringify(report,null,2));
 }
 const after=await call('/admin/models');for(const old of beforeModels.filter(m=>m.provider_id!==provider.id))assert.deepEqual(after.find(m=>m.id===old.id),old,'Existing models must not change');
 await writeFile('data/maintenance/rolldek-price-verification.json',JSON.stringify(pricePlan,null,2));await writeFile(dir+'/pricing-current.json',JSON.stringify(pricing,null,2));
 console.log(JSON.stringify({connection:true,synced:sync,...report}));
}finally{await call('/auth/logout','POST').catch(()=>{});}
