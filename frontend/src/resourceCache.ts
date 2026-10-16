export function safeResource(item:Record<string,any>):Record<string,any> {
  const other={...item.OtherData}
  try {
    const headers=JSON.parse(other.headers || '{}')
    other.headers=JSON.stringify(Object.fromEntries(Object.entries(headers).filter(([name])=>['user-agent','referer','origin','accept'].includes(name.toLowerCase()))))
  } catch {delete other.headers}
  return {...item,OtherData:other}
}
export function dedupeResources<T extends Record<string,any>>(items:T[]):T[] {
  const result:T[] = []
  const addresses = new Map<string,number>()
  const priority = (item:T) => ['running','pending'].includes(item.Status) ? 3 : item.Status === 'done' ? 2 : item.Status === 'ready' ? 1 : 0
  for(const item of items) {
    const address = typeof item.Url === 'string' ? item.Url : ''
    const index = address ? addresses.get(address) : undefined
    if(index === undefined) {
      if(address) addresses.set(address,result.length)
      result.push(item)
    } else if(priority(item) > priority(result[index])) result[index] = item
  }
  return result
}
export function restoreResources(raw:string|null):any[] {
  if(!raw)return []
  try {
    const value=JSON.parse(raw)
    if(!Array.isArray(value))return []
    return dedupeResources(value.filter(item=>typeof item?.Id==='string' && typeof item?.Url==='string').map(item=>{
      const interrupted=['running','pending'].includes(item.Status)
      return {...safeResource(item),Status:interrupted?'ready':item.Status,Message:'',SavePath:interrupted?'':item.SavePath}
    })).slice(-5000)
  } catch {return []}
}
