<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { api, type Report } from '../api';
import { state, fail } from '../store';
import DataTable from '../components/DataTable.vue';

const reports = [
  { id: 'trial_balance', label: 'Trial Balance' },
  { id: 'profit_loss', label: 'Profit and Loss' },
  { id: 'balance_sheet', label: 'Balance Sheet' },
  { id: 'day_book', label: 'Day Book' },
  { id: 'outstanding', label: 'Outstanding' },
  { id: 'ageing', label: 'Ageing Analysis' },
  { id: 'stock_summary', label: 'Stock Summary' },
  { id: 'ledger_balances', label: 'Ledger Balances' },
  { id: 'gst_returns', label: 'GST Returns' }
];

const selected = ref('trial_balance');
const from = ref('');
const to = ref('');
const report = ref<Report | null>(null);
const loading = ref(false);
const showRaw = ref(false);

async function run() {
  loading.value = true;
  report.value = null;
  try {
    const res = await api.report({
      instance: state.defaultInstance,
      company: state.company,
      report: selected.value,
      from: from.value,
      to: to.value
    });
    report.value = res.report;
  } catch (err) {
    fail(err);
  } finally {
    loading.value = false;
  }
}

const columns = computed<string[]>(() => {
  const rows = report.value ? report.value.rows || [] : [];
  const keys: string[] = [];
  for (const row of rows.slice(0, 40)) {
    for (const key of Object.keys(row)) if (keys.indexOf(key) < 0) keys.push(key);
  }
  return keys.length ? keys : ['INFO'];
});

const rows = computed<Record<string, string>[]>(() => (report.value ? report.value.rows || [] : []));

onMounted(run);
watch(() => [state.defaultInstance, state.company], run);
</script>

<template>
  <div class="view-head">
    <h2>Reports</h2>
    <span class="sub">{{ state.company || 'no company selected' }}</span>
    <div style="flex: 1"></div>
    <select v-model="selected">
      <option v-for="r in reports" :key="r.id" :value="r.id">{{ r.label }}</option>
    </select>
    <input v-model="from" type="date" title="From" />
    <input v-model="to" type="date" title="To" />
    <button class="btn primary" :disabled="loading" @click="run()">{{ loading ? 'Running…' : 'Run' }}</button>
  </div>

  <div class="view-body">
    <div v-if="report && report.note" class="hint faint" style="margin-bottom: 10px">{{ report.note }}</div>

    <DataTable
      :columns="columns"
      :rows="rows"
      empty="This report returned no rows. Some reports only exist on a live Tally instance."
    />

    <div v-if="report && report.raw" style="margin-top: 14px">
      <button class="btn sm" @click="showRaw = !showRaw">{{ showRaw ? 'Hide' : 'Show' }} raw Tally XML</button>
      <pre v-if="showRaw" style="max-height: 360px; overflow: auto; background: var(--panel); border: 1px solid var(--line-soft); border-radius: 8px; padding: 12px; font-size: 11.5px">{{ report.raw }}</pre>
    </div>
  </div>
</template>
