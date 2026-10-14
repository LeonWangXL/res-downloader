<template>
  <aside class="app-sidebar">
    <Screen v-if="envInfo.platform !== 'darwin'" />
    <div v-else class="mac-window-inset" />
    <div class="brand-block">
      <img src="@/assets/image/logo.png" :alt="t('workspace.brand')" class="brand-mark" />
      <span class="brand-name">{{ t('workspace.brand') }}</span>
      <span v-if="showUpdate" class="brand-update">{{ t('workspace.update_available') }}</span>
    </div>
    <nav class="sidebar-navigation" :aria-label="t('workspace.navigation')">
      <RouterLink to="/index" class="sidebar-link" active-class="is-active">
        <NIcon :component="ScanOutline" /><span>{{ t('menu.index') }}</span>
      </RouterLink>
      <RouterLink to="/setting" class="sidebar-link" active-class="is-active">
        <NIcon :component="SettingsOutline" /><span>{{ t('menu.setting') }}</span>
      </RouterLink>
    </nav>
    <div class="sidebar-utilities">
      <button class="sidebar-link" @click="BrowserOpenURL('https://github.com/putyy/res-downloader')"><NIcon :component="LogoGithub" /><span>GitHub</span></button>
      <button class="sidebar-link" @click="updateConfig({Locale: store.globalConfig.Locale === 'zh' ? 'en' : 'zh'})"><NIcon :component="LanguageOutline" /><span>{{ t('menu.locale') }}</span></button>
      <button class="sidebar-link" @click="updateConfig({Theme: store.globalConfig.Theme === 'darkTheme' ? 'lightTheme' : 'darkTheme'})"><NIcon :component="store.globalConfig.Theme === 'darkTheme' ? SunnyOutline : MoonOutline" /><span>{{ t('menu.theme') }}</span></button>
      <button class="sidebar-link" @click="showAppInfo = true"><NIcon :component="InformationCircleOutline" /><span>{{ t('menu.about') }}</span></button>
    </div>
    <Footer v-model:showModal="showAppInfo" />
  </aside>
</template>

<script setup lang="ts">
import {NIcon} from 'naive-ui'
import {computed, onMounted, ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {ScanOutline, SettingsOutline, LogoGithub, LanguageOutline, SunnyOutline, MoonOutline, InformationCircleOutline} from '@vicons/ionicons5'
import {useIndexStore} from '@/stores'
import {BrowserOpenURL} from '../../../wailsjs/runtime'
import Footer from '@/components/Footer.vue'
import Screen from '@/components/Screen.vue'
import request from '@/api/request'
import {compareVersions} from '@/func'

const {t} = useI18n()
const store = useIndexStore()
const envInfo = computed(() => store.envInfo)
const showAppInfo = ref(false)
const showUpdate = ref(false)
const updateConfig = async (patch: object) => {
  try { await store.setConfig(patch) } catch(error) { window.$message?.error(String(error)) }
}
onMounted(() => {
  request({url: 'https://res.putyy.com/version.json?v=' + Date.now(), method: 'get'})
    .then(res => { showUpdate.value = compareVersions(res.version, store.appInfo.Version) === 1 })
    .catch(() => { showUpdate.value = false })
})
</script>
