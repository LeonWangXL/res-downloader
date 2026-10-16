<template>
  <section class="resource-page">
    <header class="workspace-header">
      <div class="workspace-heading"><h1>{{ t('menu.index') }}</h1><span>{{ t('index.total_resources', {count: data.length}) }}</span></div>
      <div class="capture-controls">
        <span class="capture-indicator" :class="{'is-capturing': isProxy}"><span class="capture-dot" />{{ t(isProxy ? 'workspace.capture_active' : 'workspace.capture_idle') }}</span>
        <NButton @click="isProxy ? close() : open()"><template #icon><NIcon :component="isProxy ? StopOutline : RadioOutline" /></template>{{ t(isProxy ? 'workspace.stop_capture' : 'workspace.start_capture') }}</NButton>
        <NTooltip v-if="!previewOpen"><template #trigger><button class="icon-button" :aria-label="t('workspace.show_preview')" @click="previewOpen = true"><NIcon :component="ExpandOutline" /></button></template>{{ t('workspace.show_preview') }}</NTooltip>
      </div>
    </header>
    <div class="resource-workspace" :class="{'preview-collapsed': !previewOpen}">
      <section class="resource-list-panel">
        <div class="list-toolbar" id="header">
          <div class="resource-search-row">
            <NInput v-model:value="descriptionSearchValue" clearable :placeholder="t('workspace.search_description')" :aria-label="t('workspace.search_description')"><template #prefix><NIcon :component="SearchOutline" /></template></NInput>
            <NInput v-model:value="urlSearchValue" clearable :placeholder="t('workspace.search_url')" :aria-label="t('workspace.search_url')"><template #prefix><NIcon :component="LinkOutline" /></template></NInput>
            <NSelect v-model:value="resourcesType" multiple :max-tag-count="1" :options="classify" :placeholder="t('workspace.all_types')" :aria-label="t('index.type')" />
          </div>
          <div class="resource-action-row">
            <span class="selection-summary" aria-live="polite">{{ t('workspace.selected_count', {count: checkedRowKeysValue.length}) }}</span>
            <NButton type="primary" :disabled="!checkedRowKeysValue.length" @click="batchDown"><template #icon><NIcon :component="DownloadOutline" /></template>{{ t('index.batch_download') }}<span v-if="checkedRowKeysValue.length" class="button-count">({{ checkedRowKeysValue.length }})</span></NButton>
            <NButton v-if="rememberChoice" @click="clear"><template #icon><NIcon :component="TrashOutline" /></template>{{ t('index.clear_list') }}</NButton>
            <NPopconfirm v-else :show-icon="false" @positive-click="() => {rememberChoice = rememberChoiceTmp; clear()}">
              <template #trigger><NButton><template #icon><NIcon :component="TrashOutline" /></template>{{ t('index.clear_list') }}</NButton></template>
              <div class="clear-confirm"><p>{{ t('index.clear_list_tip') }}</p><NCheckbox v-model:checked="rememberChoiceTmp">{{ t('index.remember_clear_choice') }}</NCheckbox></div>
            </NPopconfirm>
            <NDropdown trigger="click" :options="batchOptions" @select="batchAction"><NButton>{{ t('workspace.more') }}<template #icon><NIcon :component="EllipsisHorizontalOutline" /></template></NButton></NDropdown>
          </div>
        </div>
        <div ref="tableRegion" class="table-region">
          <NDataTable class="resource-table" :columns="columns" :data="filteredData" :bordered="false" :max-height="tableHeight" :row-key="rowKey" :row-props="rowProps" :virtual-scroll="true" :header-height="50" :height-for-row="() => 72" :checked-row-keys="checkedRowKeysValue" :scroll-x="1200" @update:filters="updateColumnFilters" @update:checked-row-keys="handleCheck">
            <template #empty><div class="resource-empty"><NIcon :component="ScanOutline" /><h3>{{ t(data.length ? 'workspace.no_matches' : 'workspace.empty_title') }}</h3><p v-if="!data.length">{{ t('workspace.empty_hint') }}</p><NButton v-if="data.length" @click="resetSearch">{{ t('workspace.reset_search') }}</NButton></div></template>
          </NDataTable>
        </div>
      </section>
      <ResourceDetails v-if="previewOpen" :row="selectedRow" :preview-request="previewRequest" @collapse="previewOpen = false" @action="detailAction" @description="updateDescription" />
    </div>
    <footer class="workspace-footer" id="bottom">
      <span>{{ t('workspace.selected_count', {count: checkedRowKeysValue.length}) }}<span v-if="visibleResourceCount !== data.length" class="filtered-count"> · {{ t('workspace.filtered_count', {count: visibleResourceCount}) }}</span></span>
      <div class="workspace-footer-links"><button @click="BrowserOpenURL(certUrl)">{{ t('footer.cert_download') }}</button><button @click="BrowserOpenURL('https://github.com/putyy/res-downloader/issues')">{{ t('footer.help') }}</button><button @click="BrowserOpenURL('https://github.com/putyy/res-downloader/releases')">{{ t('footer.update_log') }}</button></div>
    </footer>
    <ShowLoading :loadingText="loadingText" :isLoading="loading" />
    <ImportJson v-model:showModal="showImport" @submit="handleImport" />
    <Password v-model:showModal="showPassword" @submit="handlePassword" />
  </section>
</template>

<script lang="ts" setup>
import {NButton, NIcon, NInput, NTooltip, NDropdown, NDataTable, NSelect, NPopconfirm, NCheckbox} from 'naive-ui'
import {computed, h, onMounted, onUnmounted, onActivated, nextTick, ref, watch} from 'vue'
import type {DataTableRowKey, DataTableColumns, DataTableFilterState, DataTableBaseColumn} from 'naive-ui'
import type {appType} from '@/types/app'
import {DownloadOutline, SearchOutline, TrashOutline, LinkOutline, EllipsisHorizontalOutline, StopOutline, RadioOutline, ExpandOutline, ScanOutline, CloseOutline, ArrowRedoCircleOutline, ServerOutline} from '@vicons/ionicons5'
import ResourceDetails from '@/components/ResourceDetails.vue'
import ResourceStatus from '@/components/ResourceStatus.vue'
import Action from '@/components/Action.vue'
import ShowLoading from '@/components/ShowLoading.vue'
import ImportJson from '@/components/ImportJson.vue'
import Password from '@/components/Password.vue'
import {useIndexStore} from '@/stores'
import {useEventStore} from '@/stores/event'
import {BrowserOpenURL, ClipboardSetText, Quit} from '../../wailsjs/runtime'
import * as bind from '../../wailsjs/go/core/Bind'
import appApi from '@/api/app'
import {useI18n} from 'vue-i18n'
import {useDialog} from 'naive-ui'
import {DialogOptions} from 'naive-ui/es/dialog/src/DialogProvider'
import {formatSize} from '@/func'
import {safeResource, restoreResources, dedupeResources} from '@/resourceCache'
import {canDownload, isActiveTask} from '@/resourcePresentation'
// @ts-ignore
import {getDecryptionArray} from '@/assets/js/decrypt.js'

const {t} = useI18n()
const eventStore = useEventStore()
const dialog = useDialog()
const isProxy = computed(() => {
  return store.isProxy
})
const certUrl = computed(() => {
  return store.baseUrl + "/api/cert?token=" + encodeURIComponent(window.$sessionToken || "")
})
const data = ref<any[]>([])
const filterClassify = ref<string[]>([])
const filteredData = computed(() => {
  let result = data.value

  if (filterClassify.value.length > 0) {
    result = result.filter(item => filterClassify.value.includes(item.Classify))
  }

  if (descriptionSearchValue.value) {
    result = result.filter(item => item.Description?.toLowerCase().includes(descriptionSearchValue.value.toLowerCase()))
  }

  if (urlSearchValue.value) {
    result = result.filter(item => item.Url?.toLowerCase().includes(urlSearchValue.value.toLowerCase()) || item.Domain?.toLowerCase().includes(urlSearchValue.value.toLowerCase()))
  }

  return result
})

const store = useIndexStore()
const tableHeight = ref(800)
const resourcesType = ref<string[]>(["all"])

const classifyAlias: { [key: string]: any } = {
  image: computed(() => t("index.image")),
  audio: computed(() => t("index.audio")),
  video: computed(() => t("index.video")),
  m3u8: computed(() => t("index.m3u8")),
  live: computed(() => t("index.live")),
  xls: computed(() => t("index.xls")),
  doc: computed(() => t("index.doc")),
  pdf: computed(() => t("index.pdf")),
  stream: computed(() => t("index.stream")),
  font: computed(() => t("index.font"))
}

const classify = ref([
  {
    value: "all",
    label: computed(() => t("workspace.all_types")),
  },
])
const visibleResourceCount = computed(() => filteredData.value.filter(row => columns.value.every(column => {
  if(!('filter' in column) || typeof column.filter !== 'function') return true
  const values = columnFilters.value[String(column.key)]
  return !Array.isArray(values) || !values.length || values.some(value => (column.filter as (value: string | number, row: appType.MediaInfo) => boolean)(value, row))
})).length)

const descriptionSearchValue = ref("")
const urlSearchValue = ref("")
const rememberChoice = ref(false)
const rememberChoiceTmp = ref(false)

const columnFilters = ref<DataTableFilterState>({})
const updateColumnFilters = (filters: DataTableFilterState) => {columnFilters.value = filters}
const textFilter = (key: 'Description' | 'Domain' | 'SavePath'): Partial<DataTableBaseColumn<appType.MediaInfo>> => ({
  filter: (value, row) => String(row[key] || '').toLowerCase().includes(String(value).toLowerCase()),
  filterOptionValues: columnFilters.value[key] as string[] | null || null,
  renderFilterMenu: ({hide}) => h('div', {style: 'padding:12px;width:240px'}, [
    h(NInput, {value: (columnFilters.value[key] as string[])?.[0] || '', clearable: true, placeholder: t(`index.${key === 'Description' ? 'description' : key === 'Domain' ? 'domain' : 'save_path'}`),
      'onUpdate:value': (value: string) => {columnFilters.value = {...columnFilters.value, [key]: value ? [value] : null}}, onKeyup: (event: KeyboardEvent) => {if(event.key === 'Enter')hide()}}),
  ]),
})
const columns = computed<DataTableColumns<appType.MediaInfo>>(() => [
  {type: 'selection', width: 44},
  {
    title: t('workspace.resource_description'), key: 'Description', minWidth: 232, ...textFilter('Description'),
    render: (row: appType.MediaInfo) => h('div', {class: 'resource-description'}, [
      h('button', {class: 'resource-title', title: row.Description || row.Url, onClick: (event: MouseEvent) => {event.stopPropagation(); selectRow(row)}}, row.Description || t('workspace.description_empty')),
      h('div', {class: 'resource-subtitle'}, `${row.Domain || '—'} · ${typeLabel(row.Classify)}`),
    ]),
  },
  {title: t('index.domain'), key: 'Domain', width: 130, ellipsis: {tooltip: true}, ...textFilter('Domain')},
  {title: t('workspace.source_app'), key: 'SourceApp', width: 150, ellipsis: {tooltip: true},
    filterOptions: Array.from(new Set(data.value.map(row => row.SourceApp || ''))).sort().map(value => ({label: value || t('workspace.source_unknown'), value})),
    filterOptionValues: columnFilters.value.SourceApp as string[] | null || null,
    filter: (value, row) => (row.SourceApp || '') === value,
    render: row => h(NTooltip, null, {trigger: () => h('span', row.SourceApp || t('workspace.source_unknown')), default: () => `${t('workspace.source_app_hint')}${row.SourcePID ? ` · PID ${row.SourcePID}` : ''}`}),
  },
  {title: t('index.type'), key: 'Classify', width: 100, filterOptions: classify.value.slice(1).map(item => ({label: typeLabel(item.value), value: item.value})), filterOptionValues: columnFilters.value.Classify as string[] | null || null, filter: (value, row) => row.Classify === value, render: row => typeLabel(row.Classify)},
  {title: t('workspace.size_column'), key: 'Size', width: 118, filterOptions: [{label: '< 10 MB', value: 'small'}, {label: '10–100 MB', value: 'medium'}, {label: '≥ 100 MB', value: 'large'}], filterOptionValues: columnFilters.value.Size as string[] | null || null, filter: (value, row) => value === 'small' ? row.Size < 10 * 1024 * 1024 : value === 'medium' ? row.Size >= 10 * 1024 * 1024 && row.Size < 100 * 1024 * 1024 : row.Size >= 100 * 1024 * 1024, sorter: (a: appType.MediaInfo, b: appType.MediaInfo) => a.Size - b.Size, render: (row: appType.MediaInfo) => h('span', {class: 'resource-size'}, formatSize(row.Size))},
  {title: t('index.status'), key: 'Status', width: 132, filterOptions: ['ready','pending','running','done','error'].map(value => ({label: t(`index.${value}`), value})), filterOptionValues: columnFilters.value.Status as string[] | null || null, filter: (value, row) => row.Status === value, render: (row: appType.MediaInfo) => h(ResourceStatus, {row})},
  {title: t('index.save_path'), key: 'SavePath', width: 160, ellipsis: {tooltip: true}, ...textFilter('SavePath'), render: row => row.SavePath || '—'},
  {title: t('index.operation'), key: 'actions', width: 174, render: (row: appType.MediaInfo, index: number) => h(Action, {row, index, onAction: dataAction})},
])
const previewOpen = ref(true)
const previewRequest = ref(0)
const selectedId = ref<string | null>(null)
const selectedRow = computed(() => data.value.find(item => item.Id === selectedId.value))
const tableRegion = ref<HTMLElement | null>(null)
let resizeObserver: ResizeObserver | undefined
const typeLabel = (type: string) => classifyAlias[type]?.value || type
const selectRow = (row: appType.MediaInfo) => {selectedId.value = row.Id; previewOpen.value = true}
const rowProps = (row: appType.MediaInfo) => ({
  class: [row.Id === selectedId.value ? 'resource-row-selected' : '', checkedRowKeysValue.value.includes(row.Id) ? 'resource-row-checked' : ''].filter(Boolean).join(' '),
  'aria-selected': row.Id === selectedId.value,
  onClick: (event: MouseEvent) => {if(!(event.target as HTMLElement).closest('button, input, [role=checkbox]')) selectRow(row)},
})
const detailAction = (row: appType.MediaInfo, action: string) => dataAction(row, data.value.findIndex(item => item.Id === row.Id), action)
const updateDescription = (description: string) => {if(selectedRow.value){selectedRow.value.Description = description; cacheData()}}
const resetSearch = () => {descriptionSearchValue.value = ''; urlSearchValue.value = ''; resourcesType.value = ['all']; columnFilters.value = {}}
watch(() => [data.value.length, selectedRow.value?.Id], () => {
  if(!selectedRow.value) selectedId.value = data.value[0]?.Id || null
})

const checkedRowKeysValue = ref<DataTableRowKey[]>([])
const loading = ref(false)
const loadingText = ref("")
const showImport = ref(false)
const showPassword = ref(false)
let isOpenProxy = false
let isInstall = false

onMounted(() => {
  try {
    window.addEventListener("resize", resetTableHeight)
    loading.value = true
    handleInstall().then((is:boolean)=>{isInstall=is}).catch(error=>window.$message?.error(String(error))).finally(()=>{loading.value=false})

    checkLoading()
    watch(showPassword, () => {
      if (!showPassword.value) {
        checkLoading()
      }
    })
  } catch (e) {
    window.$message?.error(JSON.stringify(e), {duration: 5000})
  }

  buildClassify()

  const temp = localStorage.getItem("resources-type")
  if (temp) {
    try { const value=JSON.parse(temp).res;if(Array.isArray(value))resourcesType.value=value } catch {localStorage.removeItem("resources-type")}
  }
  // Display filters must never restrict interception of new resources.
  filterClassify.value = resourcesType.value.includes('all') ? [] : resourcesType.value
  appApi.setType(['all']).catch(error=>window.$message?.error(String(error)))

  const cache = localStorage.getItem("resources-data")
  if (cache) {
    data.value=restoreResources(cache)
  }

  const choiceCache = localStorage.getItem("remember-clear-choice")
  if (choiceCache === "1") {
    rememberChoice.value = true
  }

  watch(rememberChoice, (n, o) => {
    if (rememberChoice.value) {
      localStorage.setItem("remember-clear-choice", "1")
    } else {
      localStorage.removeItem("remember-clear-choice")
    }
  })

  appApi.tasks().then((res:appType.Res)=>{
    if(res.code===1 && Array.isArray(res.data)){
      for(const active of res.data){
        const existing=data.value.find(item=>item.Id===active.Id || item.Url===active.Url)
        if(existing){
          if(selectedId.value===existing.Id)selectedId.value=active.Id
          checkedRowKeysValue.value=checkedRowKeysValue.value.map(id=>id===existing.Id?active.Id:id)
          Object.assign(existing,active)
        } else data.value.push(active)
      }
      data.value=dedupeResources(data.value)
      cacheData()
    }
  }).catch(console.error)
  nextTick(() => {
    resetTableHeight()
    if(tableRegion.value){resizeObserver = new ResizeObserver(resetTableHeight); resizeObserver.observe(tableRegion.value)}
  })

  eventStore.addHandle({
    type: "newResources",
    event: (res: appType.MediaInfo) => {
      const existing=data.value.find(item=>item.Id===res.Id || item.Url===res.Url)
      if(existing){
        // Refresh expiring headers/decryption metadata without replacing the visible task.
        Object.assign(existing,res,{
          Id:existing.Id,Status:existing.Status,SavePath:existing.SavePath,Message:existing.Message,
          Description:res.Description || existing.Description,DecodeKey:res.DecodeKey || existing.DecodeKey,
          SourceApp:existing.SourceApp || res.SourceApp,SourcePID:existing.SourcePID || res.SourcePID,
          OtherData:{...existing.OtherData,...res.OtherData},
        })
        cacheData()
        return
      }
      if (store.globalConfig.InsertTail) {
        data.value.push(res)
      } else {
        data.value.unshift(res)
      }
      if(data.value.length>5000){
        const idle=data.value.filter(item=>!['running','pending'].includes(item.Status))
        const removed=store.globalConfig.InsertTail?idle.slice(0,data.value.length-5000):idle.slice(-(data.value.length-5000))
        const ids=new Set(removed.map(item=>item.Id));data.value=data.value.filter(item=>!ids.has(item.Id))
        if(removed.length)appApi.delete({sign:removed.map(item=>item.UrlSign)}).catch(console.error)
      }
      cacheData()
    }
  })

  eventStore.addHandle({
    type: "downloadProgress",
    event: (res: { Id: string, SavePath: string, Status: string, Message: string }) => {
      updateItem(res.Id,item=>{
        item.Status=res.Status
        item.Message=res.Message
        item.SavePath=res.SavePath
      })
      if(['done','error','ready'].includes(res.Status)) cacheData()

    }
  })
})

watch(() => {
  return store.globalConfig.MimeMap
}, () => {
  buildClassify()
})

watch(resourcesType, (n, o) => {
  localStorage.setItem("resources-type", JSON.stringify({res: resourcesType.value}))
  filterClassify.value = resourcesType.value.includes("all") ? [] : resourcesType.value
})

const updateItem = (id: string, updater: (item: any) => void) => {
  const item = data.value.find(i => i.Id === id)
  if (item) updater(item)
}

let cacheTimer:ReturnType<typeof setTimeout>|undefined
function cacheData() {
  if(cacheTimer)clearTimeout(cacheTimer)
  cacheTimer=setTimeout(()=>{try{localStorage.setItem("resources-data",JSON.stringify(data.value.slice(-5000).map(safeResource)))}catch(error){console.error('Resource cache failed',error)}},300)
}
onActivated(()=>resetTableHeight())
onUnmounted(()=>{window.removeEventListener('resize',resetTableHeight);eventStore.removeHandle('newResources');eventStore.removeHandle('downloadProgress');if(cacheTimer)clearTimeout(cacheTimer); resizeObserver?.disconnect()})

const resetTableHeight = () => {
  if(tableRegion.value) tableHeight.value = Math.max(140, tableRegion.value.clientHeight - 52)
}

const buildClassify = () => {
  const mimeMap = store.globalConfig.MimeMap ?? {}
  const seen = new Set()
  classify.value = [
    {value: "all", label: computed(() => t("workspace.all_types"))},
    ...Object.values(mimeMap)
        .filter(({Type}) => {
          if (seen.has(Type)) return false
          seen.add(Type)
          return true
        })
        .map(({Type}) => ({
          value: Type,
          label: classifyAlias[Type] ?? Type,
        })),
  ]
}

const dataAction = (row: appType.MediaInfo, index: number, type: string) => {
  switch (type) {
    case "preview":
      selectRow(row)
      previewRequest.value++
      break
    case "folder":
      if(row.Status === "done" && row.SavePath) appApi.openFolder({filePath: row.SavePath}).catch(error => window.$message?.error(String(error)))
      break
    case "down":
      if(!canDownload(row.Classify)){window.$message?.error(t("index.download_no_tip"));return}
      download(row, index)
      break
    case "cancel":
      cancelRow(row)
      break
    case "copy":
      ClipboardSetText(row.Url).then((is: boolean) => {
        if (is) {
          window?.$message?.success(t("common.copy_success"))
        } else {
          window?.$message?.error(t("common.copy_fail"))
        }
      })
      break
    case "json":
      ClipboardSetText(encodeURIComponent(JSON.stringify(safeResource(row)))).then((is: boolean) => {
        if (is) {
          window?.$message?.success(t("common.copy_success"))
        } else {
          window?.$message?.error(t("common.copy_fail"))
        }
      })
      break
    case "open":
      BrowserOpenURL(row.Url)
      break
    case "decode":
      decodeWxFile(row, index)
      break
    case "delete":
      if (row.Status === "pending" || row.Status === "running") {
        window?.$message?.error(t("index.delete_tip"))
        return
      }
      appApi.delete({sign: [row.UrlSign]}).then(() => {
        data.value=data.value.filter(item=>item.Id!==row.Id)
        cacheData()
      })
      break
  }
}

const rowKey = (row: appType.MediaInfo) => {
  return row.Id
}

const handleCheck = (rowKeys: DataTableRowKey[]) => {
  checkedRowKeysValue.value = rowKeys
}

const batchOptions = computed(() => [
  {key: 'cancel', label: t('index.cancel_down'), disabled: !checkedRowKeysValue.value.length, icon: () => h(NIcon, {component: CloseOutline})},
  {key: 'export', label: t('index.batch_export'), disabled: !checkedRowKeysValue.value.length, icon: () => h(NIcon, {component: ArrowRedoCircleOutline})},
  {key: 'import', label: t('index.batch_import'), icon: () => h(NIcon, {component: ServerOutline})},
  {key: 'urls', label: t('index.export_url'), disabled: !checkedRowKeysValue.value.length, icon: () => h(NIcon, {component: LinkOutline})},
])
const batchAction = (action: string) => {
  if(action === 'cancel') batchCancel()
  if(action === 'export') batchExport()
  if(action === 'import') showImport.value = true
  if(action === 'urls') batchExport('url')
}

const batchDown = async () => {
  if (checkedRowKeysValue.value.length <= 0) {
    window?.$message?.error(t("index.use_data"))
    return
  }

  if (!store.globalConfig.SaveDirectory) {
    window?.$message?.error(t("index.save_path_empty"))
    return
  }

  data.value.forEach((item, index) => {
    if (checkedRowKeysValue.value.includes(item.Id) && item.Classify !== 'live' && item.Classify !== 'm3u8') {
      download(item, index)
    }
  })

  checkedRowKeysValue.value = []
}

const cancelRow=async (row:appType.MediaInfo) => {
  try {const res=await appApi.cancel({id:row.Id});if(res.code===0){window.$message?.error(res.message);return};row.Status='ready';row.SavePath='';row.Message='';cacheData()} catch(error){window.$message?.error(String(error))}
}
const batchCancel=async () => {
  if(!checkedRowKeysValue.value.length){window.$message?.error(t("index.use_data"));return}
  loading.value=true
  try {await Promise.allSettled(data.value.filter(item=>checkedRowKeysValue.value.includes(item.Id)&&['running','pending'].includes(item.Status)).map(cancelRow))}
  finally {loading.value=false;checkedRowKeysValue.value=[]}
}

const batchExport = (type?: string) => {
  if (checkedRowKeysValue.value.length <= 0) {
    window?.$message?.error(t("index.use_data"))
    return
  }

  if (!store.globalConfig.SaveDirectory) {
    window?.$message?.error(t("index.save_path_empty"))
    return
  }

  loadingText.value = t("common.loading")
  loading.value = true

  let jsonData = data.value.filter(item => checkedRowKeysValue.value.includes(item.Id))

  if (type === "url") {
    jsonData = jsonData.map(item => item.Url)
  } else {
    jsonData = jsonData.map(item => encodeURIComponent(JSON.stringify(safeResource(item))))
  }

  appApi.batchExport({content: jsonData.join("\n")}).then((res: appType.Res) => {
    loading.value = false
    if (res.code === 0) {
      window?.$message?.error(res.message)
      return
    }
    window?.$message?.success(t("index.import_success"))
    window?.$message?.info(t("index.save_path") + "：" + res.data?.file_name, {
      duration: 5000
    })
  })
}

const uint8ArrayToBase64 = (bytes: any) => {
  return window.btoa(Array.from(bytes, (byte: any) => String.fromCharCode(byte)).join(''))
}

const download = async (row:appType.MediaInfo,_index:number) => {
  if(!store.globalConfig.SaveDirectory){window.$message?.error(t("index.save_path_empty"));return}
  if(isActiveTask(row.Status))return
  row.Status='pending'
  try {
    const decodeStr=row.DecodeKey?uint8ArrayToBase64(getDecryptionArray(row.DecodeKey)):''
    const res=await appApi.download({...row,decodeStr})
    if(res.code===0)throw new Error(res.message)
  }catch(error){row.Status='error';row.Message=String(error);window.$message?.error(String(error));cacheData()}
}

const open = () => {
  isOpenProxy = true
  store.openProxy().then((res: appType.Res) => {
    if (res.code === 1) {
      return
    }

    if (["darwin", "linux"].includes(store.envInfo.platform)) {
      showPassword.value = true
    } else {
      window.$message?.error(res.message)
    }
  }).catch(error=>window.$message?.error(String(error)))
}

const close = () => {
  store.unsetProxy().catch(error=>window.$message?.error(String(error)))
}

const clear = async () => {
  const newData = [] as any[]
  const signs: string[] = []
  if (checkedRowKeysValue.value.length > 0) {
    data.value.forEach((item, index) => {
      if (checkedRowKeysValue.value.includes(item.Id) && item.Status !== "pending" && item.Status !== "running") {
        signs.push(item.UrlSign)
      } else {
        newData.push(item)
      }
    })
    checkedRowKeysValue.value = []
  } else {
    data.value.forEach((item, index) => {
      if (item.Status === "pending" || item.Status === "running") {
        newData.push(item)
      } else {
        signs.push(item.UrlSign)
      }
    })
  }
  await appApi.delete({sign: signs})
  data.value = newData
  cacheData()
}

const decodeWxFile = (row: appType.MediaInfo, index: number) => {
  if (!row.DecodeKey) {
    window?.$message?.error(t("index.video_decode_no"))
    return
  }
  appApi.openFileDialog().then((res: appType.Res) => {
    if (res.code === 0) {
      window?.$message?.error(res.message)
      return
    }
    if (res.data.file) {
      loadingText.value = t("index.video_decode_loading")
      loading.value = true
      appApi.wxFileDecode({
        ...row,
        filename: res.data.file,
        decodeStr: uint8ArrayToBase64(getDecryptionArray(row.DecodeKey))
      }).then((res: appType.Res) => {
        loading.value = false
        if (res.code === 0) {
          window?.$message?.error(res.message)
          return
        }
        row.SavePath = res.data.save_path
        row.Status = "done"
        cacheData()
        window?.$message?.success(t("index.video_decode_success"))
      })
    }
  })
}

const handleImport = (content: string) => {
  if (!content) {
    window?.$message?.error(t("index.import_empty"))
    return
  }
  let newItems = [] as any[]
  content.split("\n").forEach((line, index) => {
    try {
      let res = JSON.parse(decodeURIComponent(line))
      if (res && res?.Id && typeof res.Url==='string' && typeof res.Suffix==='string') {
        res.Id = crypto.randomUUID()
        res.SavePath = ""
        res.Status = "ready"
        newItems.push(res)
      }
    } catch (e) {
      console.log(e)
    }
  })
  if (newItems.length > 0) {
    // Keep existing IDs, download paths and in-progress tasks on repeated imports.
    data.value = dedupeResources([...data.value, ...newItems])
    cacheData()
  }
  showImport.value = false
}

const handlePassword = async (password: string, isCache: boolean) => {
  const res = await appApi.setSystemPassword({password, isCache})
  if (res.code === 0) {
    window.$message?.error(res.message)
    return
  }

  if (isOpenProxy) {
    showPassword.value = false
    store.openProxy().catch(error=>window.$message?.error(String(error)))
    return
  }

  handleInstall().then((is: boolean) => {
    if (is) {
      showPassword.value = false
    }
  })
}

const handleInstall = async () => {
  isOpenProxy = false
  const res = await appApi.install()
  if (res.code === 1) {
    isInstall=true
    if(store.globalConfig.AutoProxy) await store.openProxy()
    return true
  }

  window.$message?.error(res.message, {duration: 5000})

  if (store.envInfo.platform === "windows" && res.message.includes("Access is denied")) {
    window.$message?.error(t("index.win_install_tip"))
  } else if (["darwin", "linux"].includes(store.envInfo.platform)) {
    showPassword.value = true
  }
  return false
}

const checkLoading = () => {
  setTimeout(() => {
    if (loading.value && !isInstall && !showPassword.value) {
      dialog.warning({
        title: t("index.start_err_tip"),
        content: t("index.start_err_content"),
        positiveText: t("index.start_err_positiveText"),
        negativeText: t("index.start_err_negativeText"),
        draggable: false,
        closeOnEsc: false,
        closable: false,
        maskClosable: false,
        onPositiveClick: () => {
          bind.ResetApp()
        },
        onNegativeClick: () => {
          Quit()
        }
      } as DialogOptions)
    }
  }, 6000)
}
</script>
