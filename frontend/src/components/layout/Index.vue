<template>
  <div class="app-shell">
    <Sider />
    <main class="app-main">
      <RouterView v-slot="{Component, route}">
        <KeepAlive><component v-if="route.meta.keepAlive" :is="Component" /></KeepAlive>
        <component v-if="!route.meta.keepAlive" :is="Component" />
      </RouterView>
    </main>
  </div>
</template>
<script setup lang="ts">
import Sider from './Sider.vue'
</script>
