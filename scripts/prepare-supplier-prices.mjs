import assert from "node:assert/strict";
import { readFile, writeFile, mkdir } from "node:fs/promises";

const directory="test-results/supplier-pricing";
const read=async file=>JSON.parse(await readFile(`${directory}/${file}.json`,"utf8"));
const source={openvideo:await read("openvideo-channels"),modelhub:await read("modelhub-models"),catcoder:await read("catcoder-models"),suda:await read("suda-pricing"),gway:await read("gway-pricing"),wxart:await read("wxart-authenticated-matrix")};
const sudaCurrency=await read("suda-currency");
const wxMarket=(await read("wxart-authenticated-market")).data;
const base="http://127.0.0.1:3001/api";
const login=await fetch(base+"/auth/login",{method:"POST",headers:{"Content-Type":"application/json"},body:await readFile(".local-credentials.json","utf8")});assert.ok(login.ok);
const cookie=login.headers.get("set-cookie").split(";")[0];
const get=async path=>{const r=await fetch(base+path,{headers:{Cookie:cookie}});assert.ok(r.ok);return r.json();};
const providers=await get("/admin/providers"),models=await get("/admin/models");
const markedUp=value=>Number((BigInt(Math.round(Number(value)*1e6))*6n+2n)/5n)/1e6;
const optionValues=p=>(p?.options||[]).map(o=>o.v??o);
const canon=(m,name,value)=>optionValues(m.parameters.find(p=>p.name===name)).find(v=>String(v).toLowerCase()===String(value).toLowerCase());
const ensureChoice=(m,name,label,values,preferred)=>{
 const field=m.parameters.find(p=>p.name===name);
 if(field){for(const value of values)if(!optionValues(field).some(v=>String(v).toLowerCase()===String(value).toLowerCase()))field.options.push({l:String(value),v:value});}
 else m.parameters.push({name,label,type:"select",options:values.map(value=>({l:String(value),v:value})),default:values.includes(preferred)?preferred:values[0]});
};
const plan={version:1,markupPercent:20,createdAt:new Date().toISOString(),conversions:{openvideo:"内部数值 / 1000 = CNY",modelhub:"积分 / 100 = CNY",catcoder:"micro / 1000000 = CNY",suda:`USD × ${sudaCurrency.usd_exchange_rate} = CNY；按量视频输入列为秒价`,gway:"自定义人民币显示，倍率 1",wxart:"price_amount / 100 = R，R 按人民币口径；使用当前账户价格矩阵"},models:[],pending:[],skippedTiers:[]};
function add(target,unit,cost,conditions={},extras={},sourceLabel=""){
 if(target.model.type==="image")unit="request";
 if(!Number.isFinite(Number(cost))||Number(cost)<=0)throw new Error(`Invalid source price: ${target.model.name}`);
 if(target.vendor==="openvideo"&&target.model.parameters.some(p=>p.name==="faceEnabled"))conditions.faceEnabled=false;
 for(const [name,value]of Object.entries(conditions)){
  const p=target.model.parameters.find(p=>p.name===name);
  if(!p || p.type==="select"&&canon(target.model,name,value)===undefined){plan.skippedTiers.push({model:target.model.name,conditions,reason:"规格未在模型参数中开放"});return;}
  if(p.type==="select")conditions[name]=canon(target.model,name,value);
 }
 const key=JSON.stringify(Object.entries(conditions).sort());if(target.rules.some(rule=>JSON.stringify(Object.entries(rule.conditions).sort())===key&&rule.unit===unit))return;
 const label=[...new Set(Object.entries(conditions).map(([name,value])=>name==="duration"?`${value}秒`:typeof value==="boolean"?`${name==="faceEnabled"?"人脸处理":name}${value?"开启":"关闭"}`:({low:"低质量",medium:"中质量",high:"高质量",xhigh:"超高质量",max:"最高质量",auto:"自动",text:"常规生成",edit:"编辑模式"})[String(value)]||String(value).replace(/^(\d+)k$/i,'$1K')))].join(" · ")||"标准价格";
 const retailExtras={...extras};for(const key of ["imagePrice","videoSecondPrice"])if(retailExtras[key]!=null)retailExtras[key]=markedUp(retailExtras[key]);
 target.rules.push({label:label.slice(0,80),unit,credits:markedUp(cost),conditions,extras:retailExtras,enabled:true,sourceCostCNY:Number(Number(cost).toFixed(6)),sourceExtras:extras,sourceLabel});
}
try{
 for(const original of models){
  const provider=providers.find(p=>p.id===original.provider_id),vendor=provider.protocol,id=original.upstream_model;
  const target={id:original.id,vendor,model:structuredClone(original),before:original,rules:[],sourceUrl:({openvideo:"https://openaivideo.top/v1/channels",modelhub:"https://api.modelhub.cc/v1/models",catcoder:"https://video.catcode.top/v1/models",suda:"https://www.sudashuiapi.com/pricing",gway:"https://gway.dev/api/pricing",wxart:"https://api.wxart.space/api/model-pricing-matrix"})[vendor]};
  let pending="";
  if(vendor==="openvideo"){
   const channel=source.openvideo.channels.find(c=>c.models?.video?.includes(id)||c.models?.image?.includes(id));
   const price=channel?.pricing[original.type]?.[id];
   if(!price)pending="渠道未返回价格";
   else if(price.mode==="flat"||price.mode==="per_second")add(target,price.mode==="per_second"?"second":"request",price.price/1000,{}, {},price.mode);
   else if(price.mode==="resolution")for(const [resolution,value]of Object.entries(price)){if(resolution!=="mode")add(target,"request",value/1000,{resolution},{},"resolution");}
   else pending="尚未核实此渠道的复合价格规则";
  }else if(vendor==="suda"){
   const price=source.suda.data.find(m=>m.model_name===id);const rate=Number(sudaCurrency.usd_exchange_rate);
   if(!price)pending="模型广场未返回价格";
   else{
    assert.equal(source.suda.group_ratio.default,1);
    if(price.quota_type===0&&original.type!=="video")pending="按 Token 计费，不能作为固定图片单价";
    else{
     const unit=price.quota_type===0?"second":"request",cost=(price.quota_type===0?price.model_ratio*2:price.model_price)*rate;
     const resolutions=original.type==="image"?optionValues(original.parameters.find(p=>p.name==="resolution")):[];
     if(resolutions.length)for(const resolution of resolutions)add(target,unit,cost,{resolution},{},price.quota_type===0?"输入价格即秒价":"按次");else add(target,unit,cost,{}, {},price.quota_type===0?"输入价格即秒价":"按次");
    }
   }
  }else if(vendor==="gway"){
   const price=source.gway.data.find(m=>m.model_name===id);assert.equal(source.gway.group_ratio.default,1);
   if(price?.video_pricing?.enabled){const v=price.video_pricing;const tiers=v.by_group?.default?.tiers||v.by_group?.['*']?.tiers;if(!v.final_price||!tiers)pending="无法确认账户最终价格";else for(const row of tiers)add(target,v.unit==="per_second"?"second":"request",Number(row.price),{resolution:row.resolution},{},v.unit);}
   else if(price?.video_billing?.enabled){const v=price.video_billing;for(const row of v.tiers)add(target,v.unit==="per_second"?"second":"request",v.unit==="per_second"?row.price_per_second:row.price_per_item,{resolution:row.key},{},v.unit);}
   else pending="没有可核对的视频规格价格";
  }else if(vendor==="modelhub"){
   const item=source.modelhub.data.find(m=>m.id===id),p=item?.pricing;
   if(!p)pending="模型接口未返回价格";
   else if(p.unit==="second"){
    for(const [resolution,range]of Object.entries(p.resolutions||{})){
     if(typeof range==="object"&&range.min!==range.max){plan.skippedTiers.push({model:original.name,resolution,reason:"供应商价格为浮动区间"});continue;}
     const cost=(typeof range==="object"?range.min:range)/100;
     add(target,"second",cost,{resolution},{videoSecondPrice:cost,videoRounding:"total"},p.kind);
    }
   }else if(p.unit==="image"){
    const resolutionField=original.parameters.some(p=>p.name==="resolution")?"resolution":"quality";
    const qualityField=original.parameters.some(p=>p.name==="detail_level")?"detail_level":original.parameters.some(p=>p.name==="quality")&&resolutionField!=="quality"?"quality":null;
    const extra=(item.highlights||[]).some(s=>s.includes("每张参考图另加 5"))?{includedImages:0,imagePrice:.05}:{};
    for(const [resolution,qualities]of Object.entries(p.prices||{})){
     if(canon(original,resolutionField,resolution)===undefined)continue;
     if(!qualityField){if(qualities.default!=null)add(target,"image",qualities.default/100,{[resolutionField]:resolution},extra,p.kind);continue;}
     for(const [quality,cost]of Object.entries(qualities)){
      if(canon(original,qualityField,quality)===undefined)continue;
      const conditions={[resolutionField]:resolution,[qualityField]:quality};
      if(resolutionField!=="quality"&&original.parameters.some(p=>p.name==="quality"))conditions.quality=quality;
      add(target,"image",cost/100,conditions,extra,p.kind);
     }
    }
   }
  }else if(vendor==="catcoder"){
   const item=source.catcoder.data.find(m=>m.id===id);
   if(!item)pending="模型接口未返回价格";
   else if(item.pricing_mode==="video_token")pending="按实际视频 Token 结算；接口秒价只是预冻结金额，不能当作最终价格";
   else if(original.type==="image")for(const [spec,cost]of Object.entries(item.image_prices||{})){const [resolution,quality]=spec.split('/');add(target,"image",cost/1000000,{resolution,quality},{},"per_image");}
   else{
    for(const [resolution,cost]of Object.entries(item.resolution_prices||{[item.resolution]:item.price})){
     const extras={};if(item.extra_image_price>0){extras.includedImages=item.included_images;extras.imagePrice=item.extra_image_price/1000000;}
     if(item.reference_video_model_price){extras.videoSecondPrice=cost/1000000;extras.videoRounding="each";}
     add(target,item.pricing_mode==="second"?"second":"request",cost/1000000,{resolution},extras,item.pricing_mode);
    }
   }
  }else if(vendor==="wxart"){
   const matching=source.wxart.data.rows.filter(m=>m.public_model===id);
   if(!matching.length){const guide=(Array.isArray(wxMarket)?wxMarket:wxMarket.items||[]).find(m=>m.public_model===id);pending=guide?.price_text?.includes("token")?"图片按输入/输出 Token 分别计费，不能套用固定张价":"账户价格矩阵未返回此模型的开放价格";}
   else{
    const resolutions=[...new Set(matching.flatMap(row=>Object.values(row.cells).filter(c=>c.open&&c.supported).map(c=>c.resolution)))];
    ensureChoice(target.model,"resolution","分辨率",resolutions,original.type==="image"?"1k":"720p");
    if(id==="omni-flash")ensureChoice(target.model,"mode","生成模式",["text","edit"],"text");
    for(const row of matching)for(const cell of Object.values(row.cells).filter(c=>c.open&&c.supported&&c.price_amount>0)){
     const conditions={resolution:cell.resolution};if(id==="omni-flash")conditions.mode=row.request_mode||"text";
     const cost=cell.price_amount/100,extras={};
     if((id.startsWith("seedance2.")&&source.wxart.data.reference_video_input_billing_enabled)||(id==="minimax-h3"&&source.wxart.data.minimax_h3_reference_video_billing_enabled)){extras.videoSecondPrice=cost;extras.videoRounding="each";}
     add(target,cell.pricing_mode==="per_second"?"second":"request",cost,conditions,extras,cell.price_text);
    }
   }
  }
  if(pending||!target.rules.length)plan.pending.push({id:original.id,name:original.name,vendor,upstream:id,reason:pending||"没有与模型当前规格匹配的价格",sourceUrl:target.sourceUrl});
  else plan.models.push(target);
 }
 await mkdir("data/maintenance",{recursive:true});await writeFile("data/maintenance/supplier-price-plan.json",JSON.stringify(plan,null,2),{mode:0o600});
 console.log(JSON.stringify({pricedModels:plan.models.length,rules:plan.models.reduce((n,m)=>n+m.rules.length,0),pending:plan.pending,skippedTiers:plan.skippedTiers,conversions:plan.conversions}));
}finally{await fetch(base+"/auth/logout",{method:"POST",headers:{Cookie:cookie}});}
