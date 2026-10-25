import {writeFile} from 'node:fs/promises';
const models={};
const choice=(name,label,values,preferred)=>({name,label,type:'select',options:values.map(v=>({v,l:String(v)})),default:preferred??values[0]});
const count=(name,label,min,max,value)=>({name,label,type:'number',min,...(max==null?{}:{max}),default:value});
const refs=(image,video=0,audio=0,total)=>[['image',image],['video',video],['audio',audio]].map(([kind,max])=>({name:kind+'Urls',label:{image:'参考图片',video:'参考视频',audio:'参考音频'}[kind],type:'assets',itemType:kind,...(max==null?{limitSource:'undocumented'}:{max,limitSource:'documentation'}),...(total?{maxTotal:total}:{})}));
const imageRefs=max=>refs(max).slice(0,1);
const ratios=['21:9','16:9','3:2','4:3','1:1','3:4','2:3','9:16'];
const imageRatios=['1:1','16:9','9:16','4:3','3:4','3:2','2:3','5:4','4:5','21:9'];
const add=(id,adapter,kind,parameters,description,doc,extra={})=>models[id]={adapter,kind,parameters,description,source:'https://www.rolldek.com/docs/#/'+doc,...extra};
for(const id of ['gpt-image-2','gpt-image-2-high'])add(id,'openai-images','image',[
 choice('size','输出尺寸',['auto','1024x1024','1024x1536','1536x1024','2048x2048','3840x2160','2160x3840'],'1024x1024'),
 choice('quality','质量',id.endsWith('-high')?['medium','high']:['medium'],id.endsWith('-high')?'high':'medium'),count('n','生成数量',1,10,1),...imageRefs(16)
],`图片生成；最多 16 张参考图片；每次生成 1–10 张，按张计费。支持 1K / 2K / 4K 对应像素尺寸。${id.endsWith('-high')?'质量可选 medium / high。':'质量固定为 medium。'}`,'README');
for(const id of ['gpt-image-2.5','gpt-image-2.5-sunburst','gpt-image-2.5-flare']){
 const advanced=id!=='gpt-image-2.5';add(id,'image25','image',[choice('resolution','分辨率',advanced?['1K','2K','4K']:['1K']),choice('aspectRatio','比例',['3:1','21:9','16:9','3:2','4:3','1:1','3:4','2:3','9:16','1:3'],'1:1'),...(advanced?[choice('quality','质量',['auto','low','medium','high','xhigh','max'],'auto')]:[]),...imageRefs(advanced?16:undefined)],`图片生成；每次生成 1 张；${advanced?'最多 16 张参考图片；1K / 2K / 4K；质量支持 auto / low / medium / high / xhigh / max。':'仅 1K，不指定质量；参考图片数量上限未明确。'}尺寸使用文档规定的比例与像素对应值。`,'img25');
}
for(const id of ['gemini-3-pro-image-preview','gemini-3-pro-image','gemini-3.1-flash-image-preview','gemini-3.1-flash-image','gemini-nano-banana-2.1']){
 const expanded=!id.startsWith('gemini-3-pro');const thinking=id==='gemini-nano-banana-2.1';
 add(id,'gemini','image',[choice('resolution','分辨率',['1K','2K','4K'],'1K'),choice('aspectRatio','比例',[...imageRatios,...(expanded?['8:1','4:1','1:4','1:8']:[])],'1:1'),...(thinking?[choice('thinkingLevel','思考等级',['minimal','medium','high'],'medium')]:[]),...imageRefs(14)],`图片生成；最多 14 张参考图片；支持 1K / 2K / 4K。${thinking?'可选择 minimal / medium / high 思考等级。':''}` ,thinking?'banana-2-1':'banana');
}
add('seedream-5.0','seedream','image',[choice('resolution','分辨率',['1k','2k'],'1k'),choice('aspectRatio','比例',['auto','1:1','3:2','2:3','4:3','3:4','5:4','4:5','16:9','9:16','2:1','1:2','3:1','1:3','21:9','9:21'],'auto'),count('n','生成数量',1,4,1),...imageRefs(10)],'图片生成；支持 1K / 2K；每次生成 1–4 张；最多 10 张参考图片，供应商建议不超过 4 张。','seedream');
for(const [id,min,max,images,videos,audios,doc,def]of [
 ['sd-2.0-ch1',4,15,9,3,3,'sd20-ch1',4],['sd-2.5-ch1',4,30,30,10,10,'sd25-ch1',4],['sd-2.5-ch2',4,30,10,10,10,'sd25-ch2',4],['sd-2.5-ch3',4,30,9,0,0,'seedance-ch3',4],['sd-2.5-ch4',4,30,30,10,10,'sd25-ch4',4],['sd-2.5-ch5',5,30,30,10,10,'sd25-ch5',30],['SD2.0满血稳定933',4,15,9,3,3,'sd20-933',4],
]){
 const stable=id==='SD2.0满血稳定933';const frames=id!=='sd-2.0-ch1';
 add(id,'sd','video',[choice('resolution','分辨率',['720p']),count('duration','时长',min,max,def),choice('aspectRatio','比例',id==='sd-2.5-ch3'?['21:9','16:9','4:3','1:1','3:4','9:16']:ratios,'16:9'),...(frames?[choice('function_mode','生成方式',['references','first_last_frames'],'references')]:[]),...refs(images,videos,audios,images+videos+audios)],`视频生成；固定 720p；时长 ${min}–${max} 秒；最多 ${images} 张参考图片、${videos} 段参考视频、${audios} 段参考音频。${frames?'支持独立首尾帧模式，不能与多素材数组混用。':''}`,doc,{stable});
}
for(const id of ['gemini-omni-1.1-flash','veo-3.1-fast','veo-3.1-lite','grok-imagine-video-1.5','grok-imagine-video-1.5-fast','grok-imagine-video-1.5-lite']){
 const omni=id.startsWith('gemini-omni'),grok=id.startsWith('grok');
 add(id,'generic-video','video',[choice('resolution','分辨率',['720p']),omni?choice('duration','时长',[4,6,8,10],4):count('duration','时长',1,null,8),choice('aspectRatio','比例',['16:9','9:16'],'16:9'),...refs(undefined,undefined,undefined)],`视频生成；${omni?'720p，支持 4 / 6 / 8 / 10 秒。':'默认采用文档示例中的 720p、8 秒；其他规格需按账户分组核对。'}参考素材数量上限未公开。`,omni?'gemini-omni-video':grok?'grok':'veo');
}
add('kling-4.0','generic-video','video',[choice('resolution','分辨率',['720p','1080p'],'720p'),count('duration','时长',3,30,5),choice('aspectRatio','比例',ratios,'16:9'),...refs(10,5,3)],'视频生成；720p / 1080p；输出 3–30 秒；最多 10 张图片、5 段视频、3 段音频。参考视频每段不超过 30 秒，输入视频时长参与计费；音频每段 5–30 秒。','kling4');
for(const id of ['doubao-seedance-2-0-260128','doubao-seedance-2-0-fast-260128','doubao-seedance-2-0-mini-260615','doubao-seedance-2-5-260628'])add(id,'ark','video',[choice('resolution','分辨率',['720p','1080p'],'720p'),count('duration','时长',1,null,6),choice('aspectRatio','比例',['16:9','9:16','1:1','21:9','4:3','3:4'],'16:9'),choice('function_mode','生成方式',['references','first_last_frames'],'references'),...refs(undefined,undefined,undefined)],'视频生成；720p / 1080p；支持参考素材和独立首尾帧。按实际视频 Token 结算，固定秒价不能代替最终费用。素材数量上限需按具体分组确认。','doubao-seedance',{usageBilling:true});
for(const prefix of ['wan3.0-video','wan3.0-video-prime','wan3.0-image','wan3.0-image-prime','wan3.0-video-nsfw']){
 for(const resolution of ['', '480p','720p','1080p']){const id=prefix+(resolution?'-'+resolution:'');const isImage=prefix.includes('-image');add(id,'wan','video',[choice('resolution','分辨率',resolution?[resolution]:['480p','720p','1080p'],resolution||'720p'),count('duration','时长',1,null,10),choice('aspectRatio','比例',['16:9','9:16','1:1','adaptive'],'16:9'),choice('function_mode','生成方式',['references','first_last_frames'],'references'),...refs(10,isImage?0:5,5,20)],`视频生成；${resolution?`固定 ${resolution}`:'480p / 720p / 1080p'}；最多 10 张图片、${isImage?0:5} 段视频、5 段音频，合计最多 20 个素材。${isImage?'不支持参考视频。':'参考视频总时长不超过 15 秒，并参与计费。'}参考音频总时长不超过 15 秒。`,'wan3',{fixedResolution:resolution});}
}
await writeFile('internal/api/rolldek_catalog.json',JSON.stringify({verifiedAt:'2026-10-25',models},null,2)+'\n');
console.log(JSON.stringify({documentedModelVariants:Object.keys(models).length}));
