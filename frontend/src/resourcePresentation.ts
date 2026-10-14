export function previewURL(rawURL: string, baseURL: string | undefined, token: string | undefined): string {
  return `${baseURL || ''}/api/preview?token=${encodeURIComponent(token || '')}&url=${encodeURIComponent(rawURL)}`
}
export function canDownload(classify: string): boolean {
  return !['live', 'm3u8'].includes(classify)
}
export function canPreview(classify: string): boolean {
  return ['image', 'audio', 'video', 'live', 'm3u8'].includes(classify)
}
export function isActiveTask(status: string): boolean {
  return ['running', 'pending'].includes(status)
}

export function previewContentType(row: {ContentType?: string, Classify?: string, Suffix?: string}): string {
  const mime = (row.ContentType || '').split(';')[0].trim().toLowerCase()
  if(row.Classify === 'm3u8') return 'application/x-mpegURL'
  if(mime && mime !== 'application/octet-stream') return mime
  if(row.Classify === 'video') return row.Suffix === '.webm' ? 'video/webm' : 'video/mp4'
  if(row.Classify === 'audio') return row.Suffix === '.m4a' ? 'audio/mp4' : 'audio/mpeg'
  return mime
}

export function encryptedPreviewSource(rawURL: string): string {
  // Signed CDN URLs and captured header lookup require the original URL verbatim.
  return rawURL
}
