<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { state, loadInstances, loadCompanies, loadAiSettings, clearError } from './store';
import SideRail from './components/SideRail.vue';
import TopBar from './components/TopBar.vue';
import AiDock from './components/AiDock.vue';
import AiSettingsModal from './components/AiSettingsModal.vue';
import CompaniesView from './views/CompaniesView.vue';
import InvoiceView from './views/InvoiceView.vue';
import DayBookView from './views/DayBookView.vue';
import LedgersView from './views/LedgersView.vue';
import StockView from './views/StockView.vue';
import ReportsView from './views/ReportsView.vue';
import ComposeView from './views/ComposeView.vue';

const view = ref('invoice');

onMounted(async () => {
  await loadInstances();
  await loadCompanies();
  await loadAiSettings();
});
</script>

<template>
  <div class="app" :class="{ 'dock-closed': !state.aiOpen }">
    <SideRail :view="view" @change="view = $event" />
    <TopBar />

    <main class="main">
      <InvoiceView v-if="view === 'invoice'" />
      <DayBookView v-else-if="view === 'daybook'" />
      <LedgersView v-else-if="view === 'ledgers'" />
      <StockView v-else-if="view === 'stock'" />
      <ReportsView v-else-if="view === 'reports'" />
      <ComposeView v-else-if="view === 'compose'" />
      <CompaniesView v-else />
    </main>

    <AiDock v-if="state.aiOpen" />
    <AiSettingsModal v-if="state.settingsOpen" />

    <div v-if="state.error" class="error-bar">
      <span style="flex: 1">{{ state.error }}</span>
      <button class="btn sm ghost" @click="clearError()">dismiss</button>
    </div>
    <div v-if="state.toast" class="toast">{{ state.toast }}</div>
  </div>
</template>
