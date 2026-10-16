import {createServer} from 'node:http'
import {readFile} from 'node:fs/promises'
import {resolve, extname, sep} from 'node:path'
const root = resolve(import.meta.dirname, '..')
const dist = resolve(root, 'frontend/dist')
const port = Number(process.env.RESD_UI_PORT || 4173)
const base = `http://127.0.0.1:${port}`
const descriptions = ['八年前特斯拉刹车危机，9天翻盘的教训','这碗豪没吃饱饭吗？这都抱不起来','幼虎调皮“偷袭”饲养员，没想到反被饲','跟着课本去旅行','这一波别急着重开，先调整阵容和路线','社保扩面，最该讨论的是什么？','一曲唱响春晚，盛名随之而来','#替我自由#','遇见你很晚，但想陪你很久']
const sizes = [99.93,11.87,11.41,5.39,157.49,101.55,177.86,9.36,2.59]
const rows = Array.from({length:396}, (_,i) => ({Id:`fixture-${i}`,Url:`${base}/fixture/resource-${i}.mp4`,UrlSign:`sign-${i}`,CoverUrl:`${base}/fixture/cover.png`,Size:(sizes[i] || 8.3)*1024*1024,Domain:'qq.com',SourceApp:['chrome.exe','WeChat.exe','msedgewebview2.exe',''][i%4],SourcePID:1000+i,Classify:'video',Suffix:'.mp4',Status:i===0?'done':i===1?'pending':i===4?'running':'ready',Message:i===4?'38%':'',SavePath:i===0?'Downloads/视频.mp4':'',DecodeKey:i===3?'1234567890':'',Description:descriptions[i] || `测试资源 ${i+1}`,ContentType:'video/mp4',OtherData:{}}))
rows[9] = {...rows[9], Description:'图片预览示例', Classify:'image', Suffix:'.png', ContentType:'image/png', Url:`${base}/fixture/cover.png`}
rows[10] = {...rows[10], Description:'播放列表示例', Classify:'m3u8', Suffix:'.m3u8', ContentType:'application/vnd.apple.mpegurl'}
const config = {Theme:'lightTheme',Locale:'zh',Host:'127.0.0.1',Port:String(port),Quality:0,SaveDirectory:'Downloads',FilenameLen:10,FilenameTime:true,UpstreamProxy:'',OpenProxy:false,DownloadProxy:false,AutoProxy:false,WxAction:true,TaskNumber:4,DownNumber:3,UserAgent:'',UseHeaders:'default',InsertTail:true,Rule:'*',MimeMap:{'video/mp4':{Type:'video',Suffix:'.mp4'},'image/png':{Type:'image',Suffix:'.png'},'audio/mpeg':{Type:'audio',Suffix:'.mp3'},'application/vnd.apple.mpegurl':{Type:'m3u8',Suffix:'.m3u8'}}}
const initialRows = JSON.parse(JSON.stringify(rows))
const clients = new Set()
let capturing = true
let captureTypes = ['all']
const previewKeys = new Map()
const send = (type,data) => {for(const response of clients) response.write(`data: ${JSON.stringify({type,data})}\n\n`)}
const mime = {'.js':'application/javascript','.css':'text/css','.png':'image/png','.svg':'image/svg+xml','.ico':'image/x-icon','.html':'text/html','.mp4':'video/mp4'}
const bridge = `<script>
(() => {
 const config=${JSON.stringify(config)};
 const listeners=new Map();
 const events=new EventSource('/fixture/events');
 events.onmessage=event=>{const message=JSON.parse(event.data);for(const handler of listeners.get('event')||[])handler(JSON.stringify(message));};
 window.go={core:{Bind:{Config:()=>Promise.resolve({code:1,message:'ok',data:config}),AppInfo:()=>Promise.resolve({code:1,message:'ok',data:{AppName:'res-downloader',Version:'3.1.3',Description:'UI preview',Copyright:'',IsProxy:true,SessionToken:'local-ui-fixture',Host:'127.0.0.1',Port:'${port}',StartupError:''}}),ResetApp:()=>location.reload()}}};
 window.runtime={Environment:()=>Promise.resolve({buildType:'production',platform:'windows',arch:'amd64'}),EventsOnMultiple:(name,handler)=>{const handles=listeners.get(name)||[];handles.push(handler);listeners.set(name,handles);return ()=>listeners.set(name,handles.filter(item=>item!==handler));},EventsOff:()=>{},BrowserOpenURL:url=>window.open(url,'_blank'),ClipboardSetText:async text=>{await navigator.clipboard.writeText(text);return true;},WindowFullscreen:()=>document.documentElement.requestFullscreen(),WindowUnfullscreen:()=>document.exitFullscreen(),WindowMinimise:()=>{},Quit:()=>{},LogPrint:()=>{},LogError:()=>{}};
 if(new URLSearchParams(location.search).get('reset')==='1' || !sessionStorage.getItem('resd-ui-seeded')){localStorage.setItem('resources-data',JSON.stringify(new URLSearchParams(location.search).get('empty')==='1' ? [] : ${JSON.stringify(rows)}));localStorage.setItem('resources-type',JSON.stringify({res:['all']}));sessionStorage.setItem('resd-ui-seeded','1');}
 window.addEventListener('error',event=>fetch('/fixture/client-error',{method:'POST',body:String(event.message)}));
 window.addEventListener('unhandledrejection',event=>fetch('/fixture/client-error',{method:'POST',body:String(event.reason)}));
})();
</script>`
const server = createServer(async (req,res) => {
 try {
  const url = new URL(req.url,base)
  if(url.pathname==='/fixture/capture-state'){res.setHeader('Content-Type','application/json');res.end(JSON.stringify({captureTypes}));return;}
  if(url.pathname==='/fixture/inject' && req.method==='POST'){
   const row={...rows[0],Id:'fixture-injected',UrlSign:'sign-injected',Description:'新抓取的视频验证',Classify:'video',Status:'ready',SavePath:''};
   if(capturing && (captureTypes.includes('all') || captureTypes.includes(row.Classify))){rows.push(row);send('newResources',row);}
   res.end('ok');return;
  }
  if(url.pathname==='/fixture/events'){res.writeHead(200,{'Content-Type':'text/event-stream','Cache-Control':'no-cache','Connection':'keep-alive'});res.write(': connected\n\n');clients.add(res);req.on('close',()=>clients.delete(res));return;}
  if(url.pathname==='/fixture/client-error'){let text='';for await(const chunk of req)text+=chunk;console.error('CLIENT ERROR:',text);res.end('ok');return;}
  if(url.pathname==='/fixture/reference.png'){res.setHeader('Content-Type','image/png');res.end(await readFile(resolve(root,'docs/design-assets/design-reference.png')));return;}
  if(url.pathname==='/fixture/capture.png'){res.setHeader('Content-Type','image/png');res.end(await readFile(resolve(root,'docs/design-assets/home-after-qa.png')));return;}
  if(url.pathname==='/fixture/before.png'){res.setHeader('Content-Type','image/png');res.end(await readFile(resolve(root,'docs/design-assets/home-full-capture.png')));return;}
  if(url.pathname==='/fixture/compare' || url.pathname==='/fixture/compare-before'){
   const after=url.pathname==='/fixture/compare';res.setHeader('Content-Type','text/html');res.end(`<html><body style="margin:0;background:#eee;font-family:Segoe UI,sans-serif"><main style="display:grid;grid-template-columns:1600px 1600px;width:3200px;gap:0"><section><h2 style="height:40px;margin:0;padding:6px 20px;box-sizing:border-box">Source design</h2><img src="/fixture/reference.png" width="1600" height="1000" /></section><section><h2 style="height:40px;margin:0;padding:6px 20px;box-sizing:border-box">${after?'Implementation after fixes':'Implementation before fixes'}</h2><img src="${after?'/fixture/capture.png':'/fixture/before.png'}" width="1600" height="1000" /></section></main></body></html>`);return;
  }
  if(url.pathname==='/fixture/cover.png'){res.setHeader('Content-Type','image/png');res.end(await readFile(resolve(root,'docs/design-assets/preview-fixture.png')));return;}
  if(url.pathname==='/fixture/blank.png'){res.setHeader('Content-Type','image/png');res.end(await readFile(resolve(root,'build/appicon.png')));return;}
  if(url.pathname.startsWith('/api/')){
   let data={};if(req.method==='POST'){let body='';for await(const chunk of req)body+=chunk;data=body?JSON.parse(body):{};}
   let result=null
   if(url.pathname==='/api/preview-prepare'){
    const failure=new URL(req.headers.referer||base).searchParams.get('preview-error');
    if(failure){res.setHeader('Content-Type','application/json');res.end(JSON.stringify({code:0,message:'Preview failed',data:{reason:failure,status:403}}));return;}
   }
   if(url.pathname==='/api/preview-prepare' && data.decodeStr)previewKeys.set(data.Url,Buffer.from(data.decodeStr,'base64'))
   if(url.pathname==='/api/set-type')captureTypes=String(data.type||'').split(',')
   if(url.pathname==='/api/tasks')result=(req.headers.referer||'').includes('empty=1')?[]:rows.filter(row=>['pending','running'].includes(row.Status))
   if(url.pathname==='/api/install')result={isPass:true}
   if(url.pathname==='/api/set-config')Object.assign(config,data)
   if(url.pathname==='/api/get-config')result=config
   if(url.pathname==='/api/proxy-open'){capturing=true;result={value:true}}
   if(url.pathname==='/api/proxy-unset'){capturing=false;result={value:false}}
   if(url.pathname==='/api/open-directory')result={folder:'Downloads'}
   if(url.pathname==='/api/open-file')result={file:''}
   if(url.pathname==='/api/batch-export')result={file_name:'Downloads/export.txt'}
   if(url.pathname==='/api/download'){
    const row=rows.find(row=>row.Id===data.Id);if(row){row.Status='running';send('downloadProgress',{Id:row.Id,Status:'running',SavePath:'',Message:'38%'});setTimeout(()=>{row.Status='done';row.SavePath=`Downloads/resource-${row.Id}.mp4`;send('downloadProgress',{Id:row.Id,Status:'done',SavePath:row.SavePath,Message:'complete'})},1000)}
   }
   if(url.pathname==='/api/cancel'){const row=rows.find(row=>row.Id===data.id);if(row){row.Status='ready';send('downloadProgress',{Id:row.Id,Status:'ready',SavePath:'',Message:'cancelled'})}}
   if(url.pathname==='/api/preview'){
    const raw=url.searchParams.get('url')||'';
    const isImage=raw.endsWith('cover.png');
    const isEncrypted=raw.endsWith('resource-3.mp4');
    const media=await readFile(resolve(root,isImage?'docs/design-assets/preview-fixture.png':isEncrypted?'docs/design-assets/encrypted-fixture.mp4':'docs/design-assets/video-fixture.mp4')); 
    const key=previewKeys.get(raw);if(key){for(let i=0;i<media.length && i<key.length;i++)media[i]^=key[i]}
    const range=/bytes=(\d+)-(\d*)/.exec(req.headers.range||'');
    res.setHeader('Content-Type',isImage?'image/png':'video/mp4');res.setHeader('Accept-Ranges','bytes');
    if(range){const start=Number(range[1]);const end=Math.min(range[2]?Number(range[2]):media.length-1,media.length-1);res.writeHead(206,{'Content-Range':`bytes ${start}-${end}/${media.length}`,'Content-Length':end-start+1});res.end(media.subarray(start,end+1));return;}
    res.setHeader('Content-Length',media.length);res.end(media);return;
   }
   res.setHeader('Content-Type','application/json');res.end(JSON.stringify({code:1,message:'ok',data:result}));return
  }
  const filepath=resolve(dist,'.'+url.pathname)
  if(!filepath.startsWith(dist+sep) && filepath!==dist){res.writeHead(403);res.end();return;}
  if(url.pathname==='/' || url.pathname==='/index.html'){if(url.searchParams.get('reset')==='1'){rows.splice(0,rows.length,...JSON.parse(JSON.stringify(initialRows)));capturing=true;}res.setHeader('Content-Type','text/html; charset=utf-8');res.end((await readFile(resolve(dist,'index.html'),'utf8')).replace('<head>','<head>'+bridge));return;}
  res.setHeader('Content-Type',mime[extname(filepath)]||'application/octet-stream');res.end(await readFile(filepath))
 } catch(error){console.error(String(error));res.writeHead(404);res.end('Not found')}
})
server.listen(port,'127.0.0.1',()=>console.log(`Isolated UI preview: ${base} (sample resources; no certificates or system proxy changes)`))
