<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { api, type Ledger } from '../api';
import { state, fail } from '../store';
import DataTable from '../components/DataTable.vue';

const ledgers = ref<Ledger[]>([]);
const search = ref('');
const loading = ref(false);

async function load() {
  loading.value = true;
  try {
    const res = await api.ledgers(state.defaultInstance, state.company);
    ledgers.value = res.ledgers || [];
  } catch (err) {
    fail(err);
    ledgers.value = [];
  } finally {
    loading.value = false;
  }
}

const filtered = computed(() => {
  const needle = search.value.trim().toLowerCase();
  if (!needle) return ledgers.value;
  return ledgers.value.filter(
    (l) => l.name.toLowerCase().indexOf(needle) >= 0 || (l.parent || '').toLowerCase().indexOf(needle) >= 0
  );
});

const rows = computed<Record<string, string>[]>(() =>
  filtered.value.map((l) => {
    const balance = l.closing_balance || 0;
    return {
      name: l.name,
      parent: l.parent || '',
      debit: balance > 0 ? String(balance) : '',
      credit: balance < 0 ? String(Math.abs(balance)) : '',
      closing: String(balance)
    };
  })
);

const debitTotal = computed(() => filtered.value.reduce((s, l) => s + Math.max(l.closing_balance || 0, 0), 0));
const creditTotal = computed(() => filtered.value.reduce((s, l) => s + Math.max(-(l.closing_balance || 0), 0), 0));
const difference = computed(() => debitTotal.value - creditTotal.value);

function money(n: number): string {
  return n.toLocaleString('en-IN', { minimumFractionDigits: 0, maximumFractionDigits: 0 });
}

onMounted(load);
watch(() => [state.defaultInstance, state.company], load);
</script>

<template>
  <div class="view-head">
    <h2>Ledgers</h2>
    <span class="sub">{{ filtered.length }} of {{ ledgers.length }} accounts</span>
    <div style="flex: 1"></div>
    <input v-model="search" placeholder="Search name or group…" style="width: 260px" />
    <button class="btn" :disabled="loading" @click="load()">{{ loading ? 'Loading…' : 'Reload' }}</button>
  </div>

  <div class="view-body">
    <div class="stats">
      <div class="stat"><div class="label">Debit total</div><div class="value" style="color: var(--pos)">{{ money(debitTotal) }}</div></div>
      <div class="stat"><div class="label">Credit total</div><div class="value" style="color: var(--neg)">{{ money(creditTotal) }}</div></div>
      <div class="stat">
        <div class="label">Difference</div>
        <div class="value" :style="{ color: Math.abs(difference) < 1 ? 'var(--pos)' : 'var(--accent)' }">{{ money(difference) }}</div>
      </div>
    </div>

    <div v-if="Math.abs(difference) >= 1" class="hint faint" style="margin-bottom: 8px">
      A non-zero difference usually means opening balances are involved. The file engine cannot decode them;
      read the live Tally instance for balance-derived reports.
    </div>

    <DataTable
      :columns="['name', 'parent', 'debit', 'credit', 'closing']"
      :rows="rows"
      :numeric="['debit', 'credit', 'closing']"
      empty="No ledgers came back."
    />
  </div>
</template>
