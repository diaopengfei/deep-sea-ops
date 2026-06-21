<template>
  <div class="app">
    <ServerListView />
  </div>
</template>

<script setup lang="ts">
import ServerListView from './views/ServerListView.vue'
</script>

<style scoped>
.app {
  padding: 20px;
  max-width: 900px;
  margin: 0 auto;
}
</style>