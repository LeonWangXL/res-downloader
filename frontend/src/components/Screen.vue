<template>
  <div class="window-controls">
    <button class="window-control close" :aria-label="t('components.screen_close')" :title="t('components.screen_close')" @click="Quit"><NIcon :component="CloseOutline" /></button>
    <button class="window-control minimize" :aria-label="t('components.screen_minimize')" :title="t('components.screen_minimize')" @click="WindowMinimise"><NIcon :component="RemoveOutline" /></button>
    <button class="window-control maximize" :aria-label="t(isMaximized ? 'components.screen_restore' : 'components.screen_maximize')" :title="t(isMaximized ? 'components.screen_restore' : 'components.screen_maximize')" @click="maximizeWindow"><NIcon :component="isMaximized ? ContractOutline : ExpandOutline" /></button>
  </div>
</template>
<script setup lang="ts">
import {ref} from 'vue'
import {NIcon} from 'naive-ui'
import {CloseOutline, RemoveOutline, ExpandOutline, ContractOutline} from '@vicons/ionicons5'
import {Quit, WindowFullscreen, WindowMinimise, WindowUnfullscreen} from '../../wailsjs/runtime'
import {useI18n} from 'vue-i18n'
const {t} = useI18n()
const isMaximized = ref(false)
const maximizeWindow = () => {
  isMaximized.value = !isMaximized.value
  if(isMaximized.value) WindowFullscreen()
  else WindowUnfullscreen()
}
</script>
