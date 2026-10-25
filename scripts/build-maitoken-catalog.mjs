import {readFile,writeFile} from 'node:fs/promises';
const docs=JSON.parse(await readFile('test-results/maitoken/model-documents.json','utf8'));
const models={};
const choice=(name,label,values,value)=>({name,label,type:'select',options:values.map(v=>({v,l:String(v)})),default:values.includes(value)?value:values[0]});
for(const [id,doc]of Object.entries(docs)){
 if(doc.mode==='replace'||!['image','video'].includes(doc.category))continue;
 const kind=doc.category,create=doc.endpoints.find(e=>e.id==='create');if(create?.path!==(kind==='video'?'/v1/videos':'/v1/images/generations'))continue;
 const declaresContract=doc.parameters.some(p=>p.name==='contract_version'&&(p.values?.includes(kind+'-v1')||p.description?.includes(kind+'-v1')));if(!declaresContract)continue;
 const caps=doc.capabilities||{},parameters=[];let example={};try{example=JSON.parse(doc.request_examples?.json||'{}');}catch{}
 if(caps.resolutions?.length)parameters.push(choice('resolution','分辨率',caps.resolutions,example.resolution));
 if(kind==='video'&&Number.isFinite(caps.min_seconds)&&Number.isFinite(caps.max_seconds))parameters.push({name:'duration',label:'时长',type:'number',integer:true,min:caps.min_seconds,max:caps.max_seconds,default:caps.default_seconds??caps.min_seconds});
 if(caps.ratios?.length)parameters.push(choice('aspectRatio','比例',caps.ratios,example.ratio??(kind==='image'?'1:1':'16:9')));
 const frames=caps.image_roles?.includes('first_frame')&&caps.image_roles?.includes('last_frame');if(frames)parameters.push(choice('function_mode','生成方式',['references','first_last_frames'],'references'));
 const options=[];for(const p of doc.parameters){if(['generate_audio','watermark','draft','seed'].includes(p.name)&&p.default!=null){parameters.push({name:p.name,label:({generate_audio:'生成音频',watermark:'水印',draft:'草稿',seed:'随机种子'})[p.name],type:p.type==='boolean'?'boolean':'number',default:p.default,...(p.type==='integer'?{integer:true}:{})});options.push(p.name);}}
 const hasReferences=doc.parameters.some(p=>p.name==='references');
 for(const [media,maxName,label]of [['image','max_images','参考图片'],['video','max_videos','参考视频'],['audio','max_audios','参考音频']]){
  if(kind==='image'&&media!=='image')continue;
  if(caps[maxName]==null&&!hasReferences&&!caps['reference_'+media])continue;
  const maximum=caps[maxName]??(caps['reference_'+media]===false?0:undefined);
  parameters.push({name:media+'Urls',label,type:'assets',itemType:media,...(maximum==null?{limitSource:'undocumented'}:{max:maximum,limitSource:'documentation'})});
 }
 const parts=[kind==='image'?'图片生成':'视频生成'];
 if(caps.resolutions?.length)parts.push('分辨率 '+caps.resolutions.join(' / '));if(kind==='video'&&caps.min_seconds!=null)parts.push(`时长 ${caps.min_seconds}–${caps.max_seconds} 秒`);
 for(const p of parameters.filter(p=>p.type==='assets'))parts.push(p.max==null?p.label+'数量上限待确认':p.max===0?'不支持'+p.label:p.label+`最多 ${p.max} ${p.itemType==='image'?'张':'段'}`);
 if(frames)parts.push('支持独立首尾帧');if(kind==='image')parts.push('当前接入默认单张输出');
 models[id]={kind,parameters,frames,optionNames:options,source:'https://api.mai-token.com/docs?category='+kind+'&model='+encodeURIComponent(id),description:parts.join('；')+'。'};
}
await writeFile('internal/api/maitoken_catalog.json',JSON.stringify({verifiedAt:'2026-10-25',models},null,2)+'\n');console.log(JSON.stringify({contractModels:Object.keys(models).length}));
