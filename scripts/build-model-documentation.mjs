// Builds reviewed capability defaults from saved supplier documentation, never prices or credentials.
import {readFile,writeFile} from 'node:fs/promises';
const dir=process.argv[2]||'test-results/model-docs-20261025';
const json=async file=>JSON.parse(await readFile(`${dir}/${file}`,'utf8'));
const models={};
const unique=values=>[...new Set(values)];
function describedLimits(text=''){
 const result={};
 for(const [kind,pattern] of [['image',/(\d+)\s*(?:张\s*)?(?:图片|参考图|图)/],['video',/(\d+)\s*(?:段|个)?\s*(?:参考)?视频/],['audio',/(\d+)\s*(?:段|个)?\s*(?:参考)?音频/]]){
  const match=text.match(pattern);if(match)result[kind]=Number(match[1]);
 }
 return result;
}
function describedDuration(text=''){
 const list=text.match(/时长范围\s*\[([^\]]+)\]/);
 if(list)return {values:list[1].split(',').map(Number).filter(Number.isFinite)};
 const range=text.match(/(\d+)\s*s?\s*[-~–—]\s*(\d+)\s*(?:s|秒)/i);
 return range?{min:Number(range[1]),max:Number(range[2])}:undefined;
}
const choice=(values,def)=>({values,default:values.includes(def)?def:values[0]});
for(const vendor of ['openvideo','suda','modelhub','gway','catcoder','wxart']){
 const raw=await json(vendor+'-models.json');const catalog=raw.data||raw.models;
 models[vendor]={};
 const pricing=vendor==='suda'?await json('suda-matching-pricing.json'):vendor==='gway'?await json('gway-matching-pricing.json'):[];
 const wxart=vendor==='wxart'?(await json('wxart-static-capabilities.json'))[0]:{};
 for(const item of catalog){
  if(item.id.includes('watermark-removal'))continue;
  const f={kind:item.kind||item.media_type||item.type||(/image|banana|seedream|flux|qwen-image/i.test(item.id)?'image':'video'),references:{},sources:[],notes:[]};
  if(vendor==='suda'||vendor==='gway'){
   const details=pricing.find(p=>p.model_name===item.id)||{};const text=details.description||'';
   f.sources=[vendor==='suda'?'https://www.sudashuiapi.com/pricing':'https://gway.dev/api/pricing'];
   if(vendor==='suda'&&(details.tags?.includes('图片')||item.id==='sdas-zh-gtp-img2'))f.kind='image';
   f.references=describedLimits(text);f.duration=f.kind==='video'?describedDuration(text):undefined;
   const ratios=unique([...text.matchAll(/\b\d{1,2}:\d{1,2}\b/g)].map(m=>m[0]));if(ratios.length)f.ratios=choice(ratios,'16:9');
   if(f.kind==='image'){
    const resolutions=unique([...text.matchAll(/\b[124]k\b/gi)].map(m=>m[0].toLowerCase()));if(resolutions.length)f.resolutions=choice(resolutions,'1k');
    if(item.id==='sdas-zh-gtp-img2'){f.quality=choice(['low','medium','high'],'high');f.notes.push('参考图片编辑模型，供应商使用内置编辑提示词。');}
   }else if(vendor==='suda'){
    const tags=(details.tags||'').split(',').filter(v=>/^(?:\d+p|\d+k)$/i.test(v));if(tags.length)f.fixedResolution=tags.join(' / ');
   }
   if(vendor==='gway'){
    for(const p of JSON.parse(details.api_parameters||'[]')){
     if(p.name==='duration'&&/^\d+-\d+$/.test(p.range||'')){const [min,max]=p.range.split('-').map(Number);f.duration={min,max};}
     if(['ratio','resolution'].includes(p.name)&&p.range){const values=p.range.split(',').map(s=>s.trim()).filter(Boolean);f[p.name==='ratio'?'ratios':'resolutions']=choice(values,p.default);}
    }
    if(item.id==='seedance-2.5-g2-1'){f.videoMaxByResolution={'720p':0};f.notes.push('720p 规格不支持参考视频。');}
    if(item.id==='minimax-h3')f.notes.push('参考视频和参考音频的时长合计不超过 15 秒。');
   }
  }
  if(vendor==='catcoder'){
   f.sources=['https://video.catcode.top/docs','https://video.catcode.top/v1/models'];
   const caps=item.capabilities||{};
   for(const [kind,key] of [['image','max_images'],['video','max_videos'],['audio','max_audio']])if(caps[key]!=null)f.references[kind]=caps[key];
   if(caps.resolutions?.length)f.resolutions=choice(caps.resolutions,item.resolution);
   if(caps.durations?.length)f.duration={values:caps.durations};
   if(caps.ratios?.length)f.ratios=choice(caps.ratios,'16:9');
   if(item.max_total_media>0)f.maxTotal=item.max_total_media;
   const qualities=unique(Object.keys(item.image_prices||{}).map(k=>k.split('/')[1]).filter(Boolean));if(qualities.length)f.quality=choice(qualities,'medium');
   if(f.kind==='image')f.notes.push('每次生成 1 张图片。');
  }
  if(vendor==='modelhub'){
   f.sources=['https://modelhub.cc/quick-start','https://api.modelhub.cc/v1/models'];
   if(f.kind==='video'){
    f.references=item.id.includes('2.5')?{image:30,video:10,audio:10}:{image:9,video:3,audio:3};f.maxTotal=item.id.includes('2.5')?50:12;
    f.notes.push('参考图片单张小于 10MiB；参考视频、音频各自总时长不超过 '+(item.id.includes('2.5')?'30':'15')+' 秒。');
   }else{
    const images=item.parameters?.find(p=>p.name==='images');const range=images?.description?.match(/1[-–](\d+)\s*张/);if(range)f.references.image=Number(range[1]);
   }
   if(item.provider==='router')f.notes.push('自动路由模型，实际可用规格还取决于当前可用渠道。');
   for(const p of item.parameters||[]){
    const values=p.accepted_values||[];
    if(p.name==='duration'&&values.length)f.duration={values:values.map(Number)};
    if(['resolution','ratio','quality','detail_level'].includes(p.name)&&values.length)f[{resolution:'resolutions',ratio:'ratios',quality:'quality',detail_level:'detailLevel'}[p.name]]=choice(values,p.default);
   }
  }
  if(vendor==='openvideo'){
   f.sources=['https://openaivideo.top/','https://openaivideo.top/v1/models'];
   const caps=item.caps||{};if(caps.res?.length)f.resolutions=choice(caps.res,'720p');if(caps.ratios?.length)f.ratios=choice(caps.ratios,'16:9');f.duration=caps.dur;
   if(item.id==='30-10-10'){f.references={image:30,video:10,audio:10};f.maxTotal=50;f.notes.push('多帧模式使用 2–10 个关键帧。');}
   else f.notes.push('参考视频总时长、参考音频总时长分别不超过 12 秒。');
  }
  if(vendor==='wxart'){
   f.sources=['https://wxart.space/console/docs'];const config=wxart[item.id];
   if(config){f.references=Object.fromEntries(['image','video','audio'].map(k=>[k,config.referenceLimits[k]]));f.maxTotal=config.referenceLimits.total;f.duration=config.duration;f.resolutions=choice(config.resolutions,'720p');if(config.referenceVideoLimits?.maxTotalDurationMS)f.notes.push('参考视频总时长不超过 '+config.referenceVideoLimits.maxTotalDurationMS/1000+' 秒。');}
  }
  if(f.kind==='image'){delete f.references.video;delete f.references.audio;}
  if(!Object.keys(f.references).length)f.notes.push('渠道未公开此模型的参考素材数量上限，提交前请确认。');
  models[vendor][item.id]=f;
 }
}
const result={verifiedAt:'2026-10-25',models};
await writeFile('internal/api/vendor_documentation.json',JSON.stringify(result,null,2)+'\n');
console.log(JSON.stringify(Object.fromEntries(Object.entries(models).map(([vendor,rows])=>[vendor,{models:Object.keys(rows).length,withImageLimit:Object.values(rows).filter(f=>f.references.image!=null).length}]))));
