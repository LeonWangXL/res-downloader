<template>
  <aside class="preview-panel" :aria-label="t('workspace.resource_preview')">
    <div class="preview-panel-heading">
      <h2>{{ t('workspace.resource_preview') }}</h2>
      <button class="icon-button" :aria-label="t('workspace.collapse_preview')" @click="emit('collapse')"><NIcon :component="ChevronForwardOutline" /></button>
    </div>
    <template v-if="row">
      <div class="preview-surface">
        <NImage v-if="row.Classify === 'image'" :src="imageURL" :alt="row.Description || t('index.image')" object-fit="contain" class="resource-image" />
        <Preview v-else-if="playing && previewable" :key="row.Id" :show-modal="playing" :preview-row="row" />
        <template v-else>
          <img v-if="row.CoverUrl && !coverFailed" :src="row.CoverUrl" alt="" class="resource-cover" @error="coverFailed = true" />
          <NIcon v-else :component="row.Classify === 'audio' ? MusicalNotesOutline : VideocamOutline" class="preview-placeholder-icon" />
          <button v-if="previewable" class="preview-play" :aria-label="t('workspace.play_preview')" @click="playing = true"><NIcon :component="PlayOutline" /></button>
          <span v-else class="preview-placeholder-text">{{ t('workspace.preview_unavailable') }}</span>
        </template>
      </div>
      <div class="preview-title-row">
        <h3>{{ row.Description || t('workspace.description_empty') }}</h3>
        <NPopover trigger="click" placement="bottom-end">
          <template #trigger><button class="icon-button" :aria-label="t('workspace.edit_description')"><NIcon :component="CreateOutline" /></button></template>
          <NInput :value="row.Description" :placeholder="t('index.description')" @update:value="emit('description', $event)" />
        </NPopover>
      </div>
      <dl class="resource-metadata">
        <dt>{{ t('workspace.source') }}</dt><dd>{{ row.Domain || '—' }}</dd>
        <dt>{{ t('index.type') }}</dt><dd>{{ typeLabel }}</dd>
        <dt>{{ t('index.resource_size') }}</dt><dd>{{ formatSize(row.Size) }}</dd>
        <dt>{{ t('index.status') }}</dt><dd><ResourceStatus :row="row" /></dd>
        <dt>{{ t('workspace.link') }}</dt><dd class="metadata-url"><span :title="row.Url">{{ row.Url }}</span><button class="icon-button" :aria-label="t('index.copy_link')" @click="emit('action', row, 'copy')"><NIcon :component="CopyOutline" /></button></dd>
      </dl>
      <div class="preview-primary-actions">
        <NButton type="primary" :disabled="!active && !downloadable" @click="emit('action', row, active ? 'cancel' : 'down')"><template #icon><NIcon :component="active ? CloseOutline : DownloadOutline" /></template>{{ t(active ? 'index.cancel_down' : 'workspace.download_this') }}</NButton>
        <NButton @click="emit('action', row, 'copy')"><template #icon><NIcon :component="LinkOutline" /></template>{{ t('index.copy_link') }}</NButton>
        <NDropdown trigger="click" :options="moreOptions" @select="emit('action', row, $event)"><NButton class="preview-more" :aria-label="t('index.more_operation')"><NIcon :component="EllipsisHorizontalOutline" /></NButton></NDropdown>
      </div>
      <p v-if="!downloadable" class="preview-support-note">{{ t('index.download_no_tip') }}</p>
      <div class="resource-path">
        <div class="path-label"><NIcon :component="FolderOpenOutline" /><span>{{ t('index.save_path') }}</span></div>
        <button v-if="row.Status === 'done' && row.SavePath" class="saved-path" :title="row.SavePath" @click="emit('action', row, 'folder')">{{ row.SavePath }}</button>
        <div v-else class="saved-path muted">{{ t('workspace.download_path_hint') }}</div>
      </div>
      <button v-if="row.DecodeKey" class="decrypt-action" @click="emit('action', row, 'decode')"><NIcon :component="LockOpenOutline" />{{ t('workspace.decrypt_wechat') }}</button>
    </template>
    <div v-else class="preview-empty">
      <NIcon :component="VideocamOutline" />
      <h3>{{ t('workspace.preview_empty') }}</h3>
      <p>{{ t('workspace.preview_empty_hint') }}</p>
    </div>
  </aside>
</template>
<script setup lang="ts">
import {computed, ref, watch, onDeactivated} from 'vue'
import {NIcon, NButton, NImage, NPopover, NInput, NDropdown} from 'naive-ui'
import {ChevronForwardOutline, PlayOutline, VideocamOutline, MusicalNotesOutline, DownloadOutline, CloseOutline, CopyOutline, LinkOutline, EllipsisHorizontalOutline, FolderOpenOutline, LockOpenOutline, CreateOutline} from '@vicons/ionicons5'
import {useI18n} from 'vue-i18n'
import type {appType} from '@/types/app'
import {formatSize} from '@/func'
import {canDownload, canPreview, isActiveTask, previewURL} from '@/resourcePresentation'
import Preview from './Preview.vue'
import ResourceStatus from './ResourceStatus.vue'
const props = defineProps<{row?: appType.MediaInfo, previewRequest?: number}>()
const emit = defineEmits(['action', 'collapse', 'description'])
const {t} = useI18n()
const playing = ref(false)
const coverFailed = ref(false)
const imageURL = computed(() => previewURL(props.row?.Url || '', window.$baseUrl, window.$sessionToken))
const typeLabel = computed(() => props.row ? t(`index.${props.row.Classify}`, props.row.Classify) : '')
const previewable = computed(() => !!props.row && canPreview(props.row.Classify))
const downloadable = computed(() => !!props.row && canDownload(props.row.Classify))
const active = computed(() => !!props.row && isActiveTask(props.row.Status))
const moreOptions = computed(() => [
  {key: 'open', label: t('index.open_link')},
  {key: 'json', label: t('index.copy_data')},
  {key: 'delete', label: t('index.delete_row'), disabled: active.value},
])
watch(() => [props.row?.Id, props.previewRequest], (current, previous) => {
  playing.value = !!current[1] && current[1] !== previous?.[1] && previewable.value
  coverFailed.value = false
}, {immediate: true})
onDeactivated(() => { playing.value = false })
</script>
