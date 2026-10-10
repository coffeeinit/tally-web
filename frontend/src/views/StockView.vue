<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { api, type StockItem } from '../api';
import { state, fail, notify } from '../store';
import DataTable from '../components/DataTable.vue';

const items = ref<StockItem[]>([]);
const search = ref('');
const loading = ref(false);
const busy = ref(false);
const form = reactive({ name: '', parent: 'Primary', base_unit: 'Nos', opening_qty: 0, opening_rate: 0, gst_rate: 18, hsn_code: '' });

async function load(): Promise<void> {
  loading.value = true;
  try {
    const res = await api.stockItems(state.defaultInstance, state.company);
    items.value = res.stock_items || [];
  } catch (err) {
    fail(err);
    items.value = [];
  } finally {
    loading.value = false;
  }
}

const filtered = computed(() => {
  const needle = search.value.trim().toLowerCase();
  if (!needle) return items.value;
  return items.value.filter(
    (s) => s.name.toLowerCase().indexOf(needle) >= 0 || (s.parent || '').toLowerCase().indexOf(needle) >= 0
  );
});

const rows = computed<Record<string, string>[]>(() =>
  filtered.value.map((s) => ({
    name: s.name,
    parent: s.parent || '',
    unit: s.base_unit || '',
    qty: String(s.closing_qty || 0),
    rate: String(s.closing_rate || 0),
    value: String(s.closing_value || 0)
  }))
);

const totalValue = computed(() => filtered.value.reduce((sum, s) => sum + (s.closing_value || 0), 0));

function money(v: number): string {
  return v.toLocaleString('en-IN', { minimumFractionDigits: 0, maximumFractionDigits: 0 });
}

async function create(): Promise<void> {
  busy.value = true;
  try {
    const res = await api.createStockItem({
      instance: state.defaultInstance,
      company: state.company,
      name: form.name,
      parent: form.parent,
      base_unit: form.base_unit,
      opening_qty: Number(form.opening_qty) || 0,
      opening_rate: Number(form.opening_rate) || 0,
      gst_rate: Number(form.gst_rate) || 0,
      hsn_code: form.hsn_code
    });
    notify(res.detail || 'Stock item accepted');
    form.name = '';
    await load();
  } catch (err) {
    fail(err);
  } finally {
    busy.value = false;
  }
}

onMounted(load);
watch(() => [state.defaultInstance, state.company], load);
</script>

<template>
  <div class="view-head">
    <h2>Stock items</h2>
    <span class="sub">{{ filtered.length }} of {{ items.length }} items</span>
    <div style="flex: 1"></div>
    <input v-model="search" placeholder="Search item or group…" style="width: 240px" />
    <button class="btn" :disabled="loading" @click="load()">{{ loading ? 'Loading…' : 'Reload' }}</button>
  </div>

  <div class="view-body">
    <div class="stats">
      <div class="stat"><div class="label">Items</div><div class="value">{{ items.length }}</div></div>
      <div class="stat"><div class="label">Closing value</div><div class="value">{{ money(totalValue) }}</div></div>
    </div>

    <DataTable
      :columns="['name', 'parent', 'unit', 'qty', 'rate', 'value']"
      :rows="rows"
      :numeric="['qty', 'rate', 'value']"
      empty="No inventory masters came back. Inventory may be disabled in this company."
    />

    <div class="section-title">Create a stock item</div>
    <div class="card">
      <div class="grid-row" style="grid-template-columns: minmax(0,2fr) minmax(0,1fr) 110px 110px 110px 110px; gap: 10px">
        <div class="field" style="margin: 0">
          <label>Name</label>
          <input v-model="form.name" placeholder="PVC Pipe 4in" />
        </div>
        <div class="field" style="margin: 0">
          <label>Group</label>
          <input v-model="form.parent" />
        </div>
        <div class="field" style="margin: 0">
          <label>Unit</label>
          <input v-model="form.base_unit" />
        </div>
        <div class="field" style="margin: 0">
          <label>Opening qty</label>
          <input v-model="form.opening_qty" type="number" step="0.01" />
        </div>
        <div class="field" style="margin: 0">
          <label>Rate</label>
          <input v-model="form.opening_rate" type="number" step="0.01" />
        </div>
        <div class="field" style="margin: 0">
          <label>GST %</label>
          <input v-model="form.gst_rate" type="number" step="0.01" />
        </div>
      </div>
      <div class="grid-row" style="grid-template-columns: 160px auto; gap: 10px; margin-top: 12px; align-items: end">
        <div class="field" style="margin: 0">
          <label>HSN / SAC</label>
          <input v-model="form.hsn_code" />
        </div>
        <div>
          <button
            class="btn primary"
            :disabled="busy || !form.name.trim() || !state.writesAllowedAtServer"
            @click="create()"
          >{{ busy ? 'Sending…' : 'Create in Tally' }}</button>
        </div>
      </div>
    </div>
  </div>
</template>
