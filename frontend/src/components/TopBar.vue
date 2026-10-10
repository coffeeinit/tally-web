<script setup lang="ts">
import { state, setCompany, useInstance, loadCompanies, loadAiSettings, fail, notify } from '../store';

async function changeInstance(event: Event) {
  const id = (event.target as HTMLSelectElement).value;
  try {
    await useInstance(id);
    notify('Switched to ' + id);
  } catch (err) {
    fail(err);
  }
}

function changeCompany(event: Event) {
  setCompany((event.target as HTMLSelectElement).value);
}

async function refresh() {
  try {
    await loadCompanies();
    await loadAiSettings();
    notify('Reloaded from the engine');
  } catch (err) {
    fail(err);
  }
}

function active(): { product: string; ok: boolean; message: string } {
  const found = state.instances.find((i) => i.id === state.defaultInstance);
  if (!found) return { product: 'unknown', ok: false, message: 'not probed' };
  return { product: found.product, ok: found.health.ok, message: found.health.message || '' };
}
</script>

<template>
  <header class="topbar">
    <div class="brand">Tally Web UI<small>one binary, one port</small></div>

    <select :value="state.defaultInstance" @change="changeInstance" title="Connection">
      <option v-for="inst in state.instances" :key="inst.id" :value="inst.id">
        {{ inst.label }} ({{ inst.product }})
      </option>
    </select>

    <select :value="state.company" @change="changeCompany" title="Company" style="min-width: 220px">
      <option v-if="state.companies.length === 0" value="">(no companies)</option>
      <option v-for="c in state.companies" :key="c.id" :value="c.name">{{ c.name }}</option>
    </select>

    <span class="pill" :class="active().ok ? 'ok' : 'bad'">
      <span class="dot"></span>{{ active().product }}
    </span>
    <span v-if="active().message" class="faint" style="font-size: 11.5px">{{ active().message }}</span>

    <div class="grow"></div>

    <button class="btn sm" @click="refresh">Refresh</button>
    <button class="btn sm" @click="state.settingsOpen = true">AI settings</button>
    <button class="btn sm" :class="{ primary: !state.aiOpen }" @click="state.aiOpen = !state.aiOpen">
      {{ state.aiOpen ? 'Hide copilot' : 'Show copilot' }}
    </button>
  </header>
</template>
