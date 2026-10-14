<template>
  <div class="resource-status" :class="`status-${row.Status}`">
    <NTooltip :disabled="!row.Message || (row.Status === 'running' && progress !== null)">
      <template #trigger>
        <span class="status-label" :aria-label="label"><NIcon :component="statusIcon" /><span class="status-name">{{ statusName }}</span><span v-if="row.Status === 'running' && progress !== null" class="status-percent">{{ progress }}%</span></span>
      </template>
      {{ row.Message }}
    </NTooltip>
    <div v-if="row.Status === 'running' && progress !== null" class="download-progress" role="progressbar" :aria-label="t('index.running')" :aria-valuenow="progress" aria-valuemin="0" aria-valuemax="100"><span :style="{width: `${progress}%`}" /></div>
  </div>
</template>
<script setup lang="ts">
import {computed} from 'vue'
import {NIcon, NTooltip} from 'naive-ui'
import {CheckmarkCircleOutline, TimeOutline, EllipseOutline, AlertCircleOutline, ArrowDownCircleOutline} from '@vicons/ionicons5'
import {useI18n} from 'vue-i18n'
import type {appType} from '@/types/app'
const props = defineProps<{row: appType.MediaInfo}>()
const {t} = useI18n()
const progress = computed(() => {
  if(!props.row.Message?.endsWith('%')) return null
  const value = Number.parseFloat(props.row.Message)
  return Number.isFinite(value) ? Math.min(100, Math.max(0, value)) : null
})
const statusIcon = computed(() => ({done: CheckmarkCircleOutline, pending: TimeOutline, error: AlertCircleOutline, running: ArrowDownCircleOutline}[props.row.Status] || EllipseOutline))
const statusName = computed(() => t(`index.${props.row.Status || 'ready'}`))
const label = computed(() => {
  const status = props.row.Status || 'ready'
  const text = t(`index.${status}`)
  return status === 'running' && progress.value !== null ? `${text} ${progress.value}%` : text
})
</script>
