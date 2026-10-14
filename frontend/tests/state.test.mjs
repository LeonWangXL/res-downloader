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
const {safeResource,restoreResources}=await loadModule('../src/resourceCache.ts')
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
