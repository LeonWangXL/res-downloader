<template>
  <div class="row-actions" @click.stop>
    <NTooltip><template #trigger><button class="icon-button" :aria-label="t('index.preview')" @click="action('preview')"><NIcon :component="PlayOutline" /></button></template>{{ t('index.preview') }}</NTooltip>
    <NTooltip><template #trigger><button class="icon-button" :disabled="!active && unsupported" :aria-label="t(active ? 'index.cancel_down' : 'index.direct_download')" @click="action(active ? 'cancel' : 'down')"><NIcon :component="active ? CloseOutline : DownloadOutline" /></button></template>{{ t(active ? 'index.cancel_down' : 'index.direct_download') }}</NTooltip>
    <NTooltip><template #trigger><button class="icon-button destructive" :disabled="active" :aria-label="t('index.delete_row')" @click="action('delete')"><NIcon :component="TrashOutline" /></button></template>{{ t('index.delete_row') }}</NTooltip>
    <NTooltip><template #trigger><button class="icon-button" :aria-label="t('index.copy_link')" @click="action('copy')"><NIcon :component="CopyOutline" /></button></template>{{ t('index.copy_link') }}</NTooltip>
    <NDropdown trigger="click" :options="menuOptions" @select="action"><button class="icon-button" :aria-label="t('index.more_operation')"><NIcon :component="EllipsisHorizontalOutline" /></button></NDropdown>
  </div>
</template>
<script setup lang="ts">
import {computed, h} from 'vue'
import {NIcon, NTooltip, NDropdown} from 'naive-ui'
import {PlayOutline, DownloadOutline, CloseOutline, TrashOutline, CopyOutline, EllipsisHorizontalOutline, OpenOutline, LockOpenOutline, CodeSlashOutline} from '@vicons/ionicons5'
import {useI18n} from 'vue-i18n'
import type {appType} from '@/types/app'
const {t} = useI18n()
const props = defineProps<{row: appType.MediaInfo, index: number}>()
const emit = defineEmits(['action'])
const active = computed(() => ['running', 'pending'].includes(props.row.Status))
const unsupported = computed(() => ['live', 'm3u8'].includes(props.row.Classify))
const menuOptions = computed(() => [
  {key: 'open', label: t('index.open_link'), icon: () => h(NIcon, {component: OpenOutline})},
  ...(props.row.DecodeKey ? [{key: 'decode', label: t('index.video_decode'), icon: () => h(NIcon, {component: LockOpenOutline})}] : []),
  {key: 'json', label: t('index.copy_data'), icon: () => h(NIcon, {component: CodeSlashOutline})},
])
const action = (type: string) => emit('action', props.row, props.index, type)
</script>
