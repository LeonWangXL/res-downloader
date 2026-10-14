export function shouldAttachSessionToken(requestUrl:string,baseUrl:string|undefined):boolean {
  if(!baseUrl)return false
  try {return new URL(requestUrl,baseUrl).origin===new URL(baseUrl).origin} catch {return false}
}
