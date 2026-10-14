<template>
  <NConfigProvider class="h-full" :theme="theme" :locale="uiLocale" :theme-overrides="themeOverrides">
    <NaiveProvider>
      <RouterView v-if="ready"/>
 <div v-else class="p-8">{{ startupMessage }}</div>
    </NaiveProvider>
    <NGlobalStyle/>
    <NModalProvider/>
  </NConfigProvider>
</template>

<script setup lang="ts">
import NaiveProvider from '@/components/NaiveProvider.vue'
import {darkTheme, lightTheme, zhCN, enUS} from 'naive-ui'
import type {GlobalThemeOverrides} from 'naive-ui'
import {useIndexStore} from "@/stores"
import {computed, onMounted, ref} from "vue"
import {useEventStore} from "@/stores/event"
import type {appType} from "@/types/app"
import {useI18n} from 'vue-i18n'

const ready=ref(false)
const startupMessage=ref("正在启动 / Starting…")
const store = useIndexStore()
const eventStore = useEventStore()
const {locale} = useI18n()

const theme = computed(() => {
  if (store.globalConfig.Theme === "darkTheme") {
    document.documentElement.classList.add('dark');
    return darkTheme
  }
  document.documentElement.classList.remove('dark');
  return lightTheme
})

const themeOverrides = computed<GlobalThemeOverrides>(() => {
  const dark = store.globalConfig.Theme === 'darkTheme'
  return {
    common: {primaryColor: '#047857', primaryColorHover: '#065f46', primaryColorPressed: '#064e3b', primaryColorSuppl: '#047857', borderRadius: '6px', fontFamily: '"Segoe UI", "Microsoft YaHei", "PingFang SC", sans-serif', fontSize: '15px'},
    Button: {heightMedium: '40px', fontWeight: '500', textColorPrimary: '#ffffff', textColorHoverPrimary: '#ffffff', textColorPressedPrimary: '#ffffff', textColorFocusPrimary: '#ffffff'},
    Input: {heightMedium: '44px', border: dark ? '1px solid #42474f' : '1px solid #dce1e6'},
    Select: {peers: {InternalSelection: {heightMedium: '44px'}}},
    DataTable: {thColor: dark ? '#292e35' : '#f7f8fa', tdColor: dark ? '#20252b' : '#ffffff', tdColorHover: dark ? '#283e37' : '#f4faf7', borderColor: dark ? '#353b43' : '#edf0f3', thTextColor: dark ? '#c7cdd5' : '#576071', thFontWeight: '500', thPaddingMedium: '13px 14px', tdPaddingMedium: '11px 14px'},
  }
})

const uiLocale = computed(() => {
  locale.value = store.globalConfig.Locale
  if (store.globalConfig.Locale === "zh") {
    return zhCN
  }
  return enUS
})

onMounted(async () => {
  try { await store.init() } catch(error) { startupMessage.value=String(error);return }

  eventStore.init()
 ready.value=true
  eventStore.addHandle({
    type: "message",
    event: (res: appType.Message) => {
      switch (res?.code) {
        case 0:
          window.$message?.error(res.message)
          break
        case 1:
          window.$message?.success(res.message)
          break
      }
    }
  })
})
</script>
