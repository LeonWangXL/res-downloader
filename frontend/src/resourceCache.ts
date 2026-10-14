export function safeResource(item:Record<string,any>):Record<string,any> {
  const other={...item.OtherData}
  try {
    const headers=JSON.parse(other.headers || '{}')
    other.headers=JSON.stringify(Object.fromEntries(Object.entries(headers).filter(([name])=>['user-agent','referer','origin','accept'].includes(name.toLowerCase()))))
  } catch {delete other.headers}
  return {...item,OtherData:other}
}
export function restoreResources(raw:string|null):any[] {
  if(!raw)return []
  try {
    const value=JSON.parse(raw)
    if(!Array.isArray(value))return []
    return value.slice(-5000).filter(item=>typeof item?.Id==='string' && typeof item?.Url==='string').map(item=>{
      const interrupted=['running','pending'].includes(item.Status)
      return {...safeResource(item),Status:interrupted?'ready':item.Status,Message:'',SavePath:interrupted?'':item.SavePath}
    })
  } catch {return []}
}
