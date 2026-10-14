<template>
  <div class="inline-media-player">
    <div ref="videoHost" class="video-player-host" />
    <p v-if="errorMessage" class="preview-error" role="alert">{{ errorMessage }}</p>
    <button v-if="errorMessage" class="preview-retry" @click="start">{{ t('workspace.preview_retry') }}</button>
  </div>
</template>
<script setup lang="ts">
import {ref, watch, nextTick, onUnmounted, onDeactivated} from 'vue'
import videojs from 'video.js'
import type Player from 'video.js/dist/types/player'
import 'video.js/dist/video-js.css'
import flvjs from 'flv.js'
import {useI18n} from 'vue-i18n'
import appApi from '@/api/app'
// @ts-ignore
import {getDecryptionArray} from '@/assets/js/decrypt.js'
import {previewURL, encryptedPreviewSource, previewContentType} from '@/resourcePresentation'

const props = defineProps<{showModal: boolean, previewRow: any}>()
const {t} = useI18n()
const videoHost = ref<HTMLElement | null>(null)
const errorMessage = ref('')
let video: HTMLVideoElement | null = null
let player: Player | null = null
let flvPlayer: flvjs.Player | null = null
let generation = 0
function cleanup() {
  generation++
  flvPlayer?.destroy(); flvPlayer = null
  player?.dispose(); player = null
  if(video) { video.pause(); video.removeAttribute('src'); video.load() }
  video = null
  videoHost.value?.replaceChildren()
}
function fail(error: unknown) {
  console.error('Media preview failed:', error instanceof Error ? error.message : video?.error?.code || 'media error')
  const code = player?.error()?.code || video?.error?.code
  errorMessage.value = t(code === 2 ? 'workspace.preview_network' : code === 3 ? 'workspace.preview_decode' : code === 4 ? 'workspace.preview_unsupported' : 'workspace.preview_error')
}
async function start() {
  cleanup()
  errorMessage.value = ''
  const version = generation
  await nextTick()
  if(!props.showModal || !props.previewRow || !videoHost.value || version !== generation) return
  const row = props.previewRow
  const mediaURL = row.DecodeKey ? encryptedPreviewSource(row.Url) : row.Url
  try {
    let decodeStr = ''
    if(row.DecodeKey) {
      const key: Uint8Array = getDecryptionArray(row.DecodeKey)
      // Encode only the prefix key; the full video stays streamed by the local service.
      decodeStr = btoa(Array.from(key, byte => String.fromCharCode(byte)).join(''))
    }
    const result = await appApi.preparePreview({...row, Url: mediaURL, decodeStr, check: row.Classify !== 'live'})
    if(version !== generation) return
    if(result.code !== 1) {
      const reasons: Record<string,string> = {network: 'preview_network', http: 'preview_http', range: 'preview_range', non_media: 'preview_non_media', decrypt: 'preview_decrypt'}
      errorMessage.value = t(`workspace.${reasons[result.data?.reason] || 'preview_error'}`, {status: result.data?.status})
      return
    }
    video = document.createElement('video')
    video.className = 'video-js vjs-default-skin'
    video.controls = true
    video.setAttribute('playsinline', '')
    videoHost.value!.appendChild(video)
    const sourceURL = previewURL(mediaURL, window.$baseUrl, window.$sessionToken)
    if(row.Classify === 'live' && /flv/i.test(row.ContentType)) {
      if(!flvjs.isSupported()) throw new Error('FLV unavailable')
      flvPlayer = flvjs.createPlayer({type: 'flv', url: sourceURL})
      flvPlayer.attachMediaElement(video)
      flvPlayer.on(flvjs.Events.ERROR, fail)
      flvPlayer.load()
      flvPlayer.play()?.catch(fail)
    } else {
      player = videojs(video, {controls: true, autoplay: false, preload: 'metadata', fluid: false})
      player.on('error', fail)
      player.src({src: sourceURL, type: previewContentType(row), withCredentials: true})
      player.play()?.catch(() => {})
    }
  } catch(error) { if(version === generation) fail(error) }
}
watch(() => [props.showModal, props.previewRow?.Id, props.previewRow?.Url], start, {immediate: true, flush: 'post'})
onDeactivated(cleanup)
onUnmounted(cleanup)
</script>
