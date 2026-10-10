<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { api, type Voucher } from '../api';
import { state, fail } from '../store';
import DataTable from '../components/DataTable.vue';

const vouchers = ref<Voucher[]>([]);
const loading = ref(false);
const from = ref('');
const to = ref('');
const type = ref('');
const party = ref('');

const types = ['', 'Sales', 'Purchase', 'Receipt', 'Payment', 'Journal', 'Contra', 'Credit Note', 'Debit Note'];

async function load() {
  loading.value = true;
  try {
    const res = await api.vouchers({
      instance: state.defaultInstance,
      company: state.company,
      type: type.value,
      from: from.value,
      to: to.value,
      party: party.value,
      limit: 500
    });
    vouchers.value = res.vouchers || [];
  } catch (err) {
    fail(err);
    vouchers.value = [];
  } finally {
    loading.value = false;
  }
}

const rows = computed<Record<string, string>[]>(() =>
  vouchers.value.map((v) => ({
    date: v.date,
    type: v.type,
    number: v.number,
    party: v.party,
    narration: v.narration,
    amount: String(v.amount)
  }))
);

const total = computed(() => vouchers.value.reduce((sum, v) => sum + (v.amount || 0), 0));

function money(n: number): string {
  return n.toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

onMounted(load);
watch(() => [state.defaultInstance, state.company], load);
</script>

<template>
  <div class="view-head">
    <h2>Day Book</h2>
    <span class="sub">{{ state.company || 'no company selected' }}</span>
    <div style="flex: 1"></div>
    <input v-model="from" type="date" title="From" />
    <input v-model="to" type="date" title="To" />
    <select v-model="type">
      <option v-for="t in types" :key="t" :value="t">{{ t || 'All voucher types' }}</option>
    </select>
    <input v-model="party" placeholder="Party contains…" style="width: 170px" @keydown.enter="load()" />
    <button class="btn primary" :disabled="loading" @click="load()">{{ loading ? 'Loading…' : 'Run' }}</button>
  </div>

  <div class="view-body">
    <div class="stats">
      <div class="stat"><div class="label">Vouchers</div><div class="value">{{ vouchers.length }}</div></div>
      <div class="stat"><div class="label">Total value</div><div class="value">{{ money(total) }}</div></div>
      <div class="stat"><div class="label">Voucher types</div><div class="value">{{ new Set(rows.map(r => r.type)).size }}</div></div>
      <div class="stat"><div class="label">Source</div><div class="value" style="font-size: 15px">{{ state.defaultInstance }}</div></div>
    </div>

    <DataTable
      :columns="['date', 'type', 'number', 'party', 'narration', 'amount']"
      :rows="rows"
      :numeric="['amount']"
      empty="Nothing matched. Try widening the dates or clearing the filters."
    />
  </div>
</template>
