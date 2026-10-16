import test from 'node:test'
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import ts from 'typescript'
async function loadModule(path){
 const source=await readFile(new URL(path,import.meta.url),'utf8')
 const {outputText}=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2020}})
 return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
}
const {shouldAttachSessionToken}=await loadModule('../src/api/session.ts')
const {safeResource,restoreResources,dedupeResources}=await loadModule('../src/resourceCache.ts')
test('session token stays on the local API origin',()=>{
 const base='http://127.0.0.1:8899'
 assert.equal(shouldAttachSessionToken('/api/download',base),true)
 for(const url of ['https://res.putyy.com/version.json','//evil.example/api','http://127.0.0.1:8900/api'])assert.equal(shouldAttachSessionToken(url,base),false)
 assert.equal(shouldAttachSessionToken('/api/download',undefined),false)
})
test('resource persistence/export excludes login headers without mutating live headers',()=>{
 const item={Id:'one',Url:'http://example/media',OtherData:{headers:JSON.stringify({'Cookie':['secret'],'Authorization':['token'],'User-Agent':['agent'],'Referer':['source']})}}
 const saved=safeResource(item)
 const headers=JSON.parse(saved.OtherData.headers)
 assert.equal(headers.Cookie,undefined);assert.equal(headers.Authorization,undefined)
 assert.deepEqual(headers['User-Agent'],['agent']);assert.deepEqual(headers.Referer,['source'])
 assert.match(item.OtherData.headers,/secret/)
})
test('damaged history is recoverable and interrupted tasks do not stay running',()=>{
 assert.deepEqual(restoreResources('broken'),[]);assert.deepEqual(restoreResources('{}'),[])
 const restored=restoreResources(JSON.stringify([{Id:'one',Url:'http://example/file',Status:'running',SavePath:'12%'}]))
 assert.equal(restored[0].Status,'ready');assert.equal(restored[0].SavePath,'')
})

test('application attribution survives resource persistence and legacy history stays unknown',()=>{
 const row={Id:'app-one',Url:'https://example/file',SourceApp:'WeChat.exe',SourcePID:1234,Status:'ready'}
 const restored=restoreResources(JSON.stringify([safeResource(row)]))
 assert.equal(restored[0].SourceApp,'WeChat.exe');assert.equal(restored[0].SourcePID,1234)
 const legacy=restoreResources(JSON.stringify([{Id:'legacy',Url:'https://example/old'}]))
 assert.equal(legacy[0].SourceApp,undefined)
})

test('URL deduplication keeps active tasks and completed paths across repeated imports',()=>{
 const url='https://example/video.mp4?token=a%2Bb&signature=original'
 const completed={Id:'done',Url:url,Status:'done',SavePath:'Downloads/video.mp4'}
 const imported={Id:'imported',Url:url,Status:'ready',SavePath:''}
 assert.deepEqual(dedupeResources([completed,imported,imported]),[completed])
 const active={Id:'active',Url:url,Status:'running',Message:'40%'}
 assert.deepEqual(dedupeResources([imported,completed,active]),[active])
 assert.equal(imported.Status,'ready')
 const other={Id:'different-signature',Url:url+'&quality=high',Status:'ready'}
 assert.deepEqual(dedupeResources([imported,other]),[imported,other])
 assert.deepEqual(dedupeResources([{Id:'a',Url:'https://example/a'},{Id:'b',Url:'https://example/b'}]).map(row=>row.Id),['a','b'])
})

test('duplicate history is repaired without losing a completed download',()=>{
 const restored=restoreResources(JSON.stringify([
  {Id:'first',Url:'https://example/video',Status:'ready'},
  {Id:'saved',Url:'https://example/video',Status:'done',SavePath:'Downloads/video.mp4'},
  {Id:'second-url',Url:'https://example/video?token=second',Status:'ready'},
 ]))
 assert.equal(restored.length,2)
 assert.equal(restored[0].Id,'saved');assert.equal(restored[0].SavePath,'Downloads/video.mp4')
})

const {previewURL,canDownload,canPreview,isActiveTask,encryptedPreviewSource,previewContentType}=await loadModule('../src/resourcePresentation.ts')
test('inline preview preserves signed URLs and authenticates only the local preview endpoint',()=>{
 const raw='https://example.com/file.mp4?token=a%2Bb&name=travel+video'
 const preview=new URL(previewURL(raw,'http://127.0.0.1:8899','local token'))
 assert.equal(preview.origin,'http://127.0.0.1:8899')
 assert.equal(preview.searchParams.get('url'),raw)
 assert.equal(preview.searchParams.get('token'),'local token')
})
test('resource actions preserve existing unsupported types and active task cancellation',()=>{
 for(const kind of ['video','audio','image','pdf'])assert.equal(canDownload(kind),true)
 for(const kind of ['live','m3u8'])assert.equal(canDownload(kind),false)
 assert.equal(canPreview('image'),true);assert.equal(canPreview('pdf'),false)
 for(const status of ['running','pending'])assert.equal(isActiveTask(status),true)
 for(const status of ['ready','done','error'])assert.equal(isActiveTask(status),false)
})

test('encrypted preview preserves every signed query parameter and its encoding',()=>{
 const raw='https://video.qq.com/file.mp4?encfilekey=key&token=a%2Bb&X-snsvideoflag=high&signature=keep+exact'
 assert.equal(encryptedPreviewSource(raw),raw)
 const parsed=new URL(encryptedPreviewSource(raw))
 assert.equal(parsed.searchParams.get('token'),'a+b')
 assert.equal(parsed.searchParams.get('encfilekey'),'key')
 assert.equal(parsed.searchParams.get('X-snsvideoflag'),'high')
 assert.equal(parsed.searchParams.get('signature'),'keep exact')
})

test('preview chooses a playable source type for generic MIME and playlists',()=>{
 assert.equal(previewContentType({ContentType:'application/octet-stream',Classify:'video',Suffix:'.mp4'}),'video/mp4')
 assert.equal(previewContentType({ContentType:'VIDEO/MP4; charset=UTF-8',Classify:'video'}),'video/mp4')
 assert.equal(previewContentType({ContentType:'text/plain',Classify:'m3u8'}),'application/x-mpegURL')
 assert.equal(previewContentType({Classify:'audio',Suffix:'.m4a'}),'audio/mp4')
})
