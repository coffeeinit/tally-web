<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { api, type Ledger, type StockItem } from '../api';
import { state, fail, notify } from '../store';

// The four documents a trading business raises every day. The party sits on one
// side of the books, the tax ledgers on the other, and the goods are allocated
// to a sales or purchase ledger.
const docTypes = [
  { type: 'Sales', label: 'Tax Invoice', party: 'debtor', side: 'debit' },
  { type: 'Purchase', label: 'Purchase', party: 'creditor', side: 'credit' },
  { type: 'Credit Note', label: 'Credit Note', party: 'debtor', side: 'debit' },
  { type: 'Debit Note', label: 'Debit Note', party: 'creditor', side: 'credit' }
];

const docType = ref('Sales');
const head = reactive({
  date: new Date().toISOString().slice(0, 10),
  party: '',
  narration: '',
  voucher_number: '',
  place_of_supply: ''
});

interface Line { stock_item: string; quantity: number; rate: number; unit: string; ledger: string; discount: number }
interface TaxRow { ledger: string; amount: number }

const items = ref<Line[]>([]);
const taxes = ref<TaxRow[]>([]);
const ledgers = ref<Ledger[]>([]);
const stock = ref<StockItem[]>([]);
const busy = ref(false);
const gstRate = ref(18);
const interState = ref(false);

const current = computed(() => docTypes.find((d) => d.type === docType.value) || docTypes[0]);
const partyIsDebit = computed(() => current.value.side === 'debit');
const taxIsDebit = computed(() => !partyIsDebit.value);

function round(v: number): number {
  return Math.round(v * 100) / 100;
}

function lineAmount(line: Line): number {
  return round((Number(line.quantity) || 0) * (Number(line.rate) || 0) - (Number(line.discount) || 0));
}

const itemTotal = computed(() => round(items.value.reduce((sum, l) => sum + lineAmount(l), 0)));
const taxTotal = computed(() => round(taxes.value.reduce((sum, t) => sum + (Number(t.amount) || 0), 0)));
const grandTotal = computed(() => round(itemTotal.value + taxTotal.value));

const partyOptions = computed(() => {
  const list = ledgers.value.filter((l) => new RegExp(current.value.party, 'i').test(l.parent || ''));
  return (list.length ? list : ledgers.value).slice(0, 800);
});

const itemLedgerOptions = computed(() =>
  ledgers.value.filter((l) => /sales accounts|purchase accounts/i.test(l.parent || ''))
);

function defaultItemLedger(): string {
  const want = partyIsDebit.value ? /sales/i : /purchase/i;
  const found = itemLedgerOptions.value.find((l) => want.test(l.parent || ''));
  return found ? found.name : (itemLedgerOptions.value[0]?.name || '');
}

function addLine(): void {
  items.value.push({ stock_item: '', quantity: 1, rate: 0, unit: 'Nos', ledger: defaultItemLedger(), discount: 0 });
}

function removeLine(index: number): void {
  items.value.splice(index, 1);
}

function applyUnit(line: Line): void {
  const found = stock.value.find((s) => s.name === line.stock_item);
  if (found) {
    if (found.base_unit) line.unit = found.base_unit;
    if (!line.rate && found.closing_rate) line.rate = found.closing_rate;
  }
}

function addTax(): void {
  taxes.value.push({ ledger: '', amount: 0 });
}

function applyGst(): void {
  const base = itemTotal.value;
  const tax = round((base * (Number(gstRate.value) || 0)) / 100);
  if (interState.value) {
    taxes.value = [{ ledger: 'IGST', amount: tax }];
  } else {
    const half = round(tax / 2);
    taxes.value = [
      { ledger: 'CGST', amount: half },
      { ledger: 'SGST', amount: round(tax - half) }
    ];
  }
}

async function load(): Promise<void> {
  try {
    const [l, s] = await Promise.all([
      api.ledgers(state.defaultInstance, state.company),
      api.stockItems(state.defaultInstance, state.company)
    ]);
    ledgers.value = l.ledgers || [];
    stock.value = s.stock_items || [];
    if (items.value.length === 0) addLine();
  } catch (err) {
    fail(err);
  }
}

async function submit(): Promise<void> {
  if (!head.party.trim()) {
    fail(new Error('a party is required'));
    return;
  }
  busy.value = true;
  try {
    const entries = [
      { ledger: head.party.trim(), amount: grandTotal.value, is_debit: partyIsDebit.value },
      ...taxes.value
        .filter((t) => t.ledger.trim() && Number(t.amount) !== 0)
        .map((t) => ({ ledger: t.ledger.trim(), amount: round(Number(t.amount)), is_debit: taxIsDebit.value }))
    ];
    const payload = {
      instance: state.defaultInstance,
      company: state.company,
      type: docType.value,
      date: head.date,
      party: head.party.trim(),
      narration: head.narration,
      voucher_number: head.voucher_number,
      place_of_supply: head.place_of_supply,
      entries,
      items: items.value
        .filter((l) => l.stock_item.trim() && Number(l.quantity) > 0)
        .map((l) => ({
          stock_item: l.stock_item.trim(),
          quantity: Number(l.quantity),
          rate: Number(l.rate),
          unit: l.unit,
          ledger: l.ledger || defaultItemLedger(),
          discount: Number(l.discount) || 0,
          amount: lineAmount(l)
        }))
    };
    const res = await api.createVoucher(payload);
    notify(res.detail || 'Voucher accepted by Tally');
    items.value = [];
    taxes.value = [];
    head.narration = '';
    head.voucher_number = '';
    addLine();
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
    <h2>{{ current.label }}</h2>
    <span class="sub">{{ state.company || 'no company selected' }}</span>
    <div style="flex: 1"></div>
    <span class="pill" :class="state.writesAllowedAtServer ? 'accent' : 'bad'">
      {{ state.writesAllowedAtServer ? 'can post to Tally' : 'server is read-only' }}
    </span>
    <span v-if="!state.writesAllowedAtServer" class="faint" style="font-size: 11.5px">start with --allow-writes</span>
  </div>

  <div class="view-body">
    <div class="doc-tabs" style="margin-bottom: 14px">
      <button
        v-for="d in docTypes"
        :key="d.type"
        class="btn sm"
        :class="{ primary: d.type === docType }"
        @click="docType = d.type"
      >{{ d.label }}</button>
    </div>

    <div class="grid-row" style="grid-template-columns: 130px minmax(0,1fr) 150px 150px; margin-bottom: 16px">
      <div class="field" style="margin: 0">
        <label>Date</label>
        <input v-model="head.date" type="date" />
      </div>
      <div class="field" style="margin: 0">
        <label>Party</label>
        <input v-model="head.party" list="party-list" :placeholder="partyIsDebit ? 'Customer ledger' : 'Supplier ledger'" />
        <datalist id="party-list">
          <option v-for="p in partyOptions" :key="p.name" :value="p.name" />
        </datalist>
      </div>
      <div class="field" style="margin: 0">
        <label>Voucher no.</label>
        <input v-model="head.voucher_number" placeholder="auto" />
      </div>
      <div class="field" style="margin: 0">
        <label>Place of supply</label>
        <input v-model="head.place_of_supply" placeholder="state" />
      </div>
    </div>

    <div class="section-title">Items</div>
    <table class="entry">
      <thead>
        <tr>
          <th style="width: 30%">Stock item</th>
          <th style="width: 10%" class="num">Qty</th>
          <th style="width: 8%">Unit</th>
          <th style="width: 13%" class="num">Rate</th>
          <th style="width: 11%" class="num">Discount</th>
          <th style="width: 20%">Ledger</th>
          <th style="width: 12%" class="num">Amount</th>
          <th style="width: 34px"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(line, index) in items" :key="index">
          <td>
            <input v-model="line.stock_item" list="stock-list" placeholder="item name" @change="applyUnit(line)" />
          </td>
          <td><input v-model="line.quantity" class="num" type="number" step="0.01" /></td>
          <td><input v-model="line.unit" /></td>
          <td><input v-model="line.rate" class="num" type="number" step="0.01" /></td>
          <td><input v-model="line.discount" class="num" type="number" step="0.01" /></td>
          <td>
            <select v-model="line.ledger">
              <option value="">{{ defaultItemLedger() || 'select ledger' }}</option>
              <option v-for="l in itemLedgerOptions" :key="l.name" :value="l.name">{{ l.name }}</option>
            </select>
          </td>
          <td class="amount">{{ lineAmount(line).toFixed(2) }}</td>
          <td><button class="btn sm ghost" @click="removeLine(index)">✕</button></td>
        </tr>
      </tbody>
    </table>
    <datalist id="stock-list">
      <option v-for="s in stock" :key="s.name" :value="s.name" />
    </datalist>

    <div style="margin: 10px 0 18px">
      <button class="btn sm" @click="addLine()">+ Add item</button>
    </div>

    <div class="section-title">Tax</div>
    <div style="display: flex; gap: 8px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 10px">
      <div class="field" style="margin: 0; width: 110px">
        <label>GST rate %</label>
        <input v-model="gstRate" type="number" step="0.01" />
      </div>
      <label class="switch" style="padding-bottom: 7px">
        <input type="checkbox" v-model="interState" />
        Inter-state (IGST)
      </label>
      <button class="btn sm" @click="applyGst()">Apply on items</button>
      <button class="btn sm" @click="addTax()">+ Custom tax line</button>
    </div>

    <table class="entry" v-if="taxes.length">
      <thead>
        <tr>
          <th style="width: 60%">Tax ledger</th>
          <th style="width: 30%" class="num">Amount</th>
          <th style="width: 34px"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(t, index) in taxes" :key="index">
          <td>
            <input v-model="t.ledger" list="tax-list" placeholder="CGST, SGST, IGST" />
          </td>
          <td><input v-model="t.amount" class="num" type="number" step="0.01" /></td>
          <td><button class="btn sm ghost" @click="taxes.splice(index, 1)">✕</button></td>
        </tr>
      </tbody>
    </table>
    <datalist id="tax-list">
      <option value="CGST" /><option value="SGST" /><option value="IGST" />
      <option value="Input CGST" /><option value="Input SGST" /><option value="Input IGST" />
    </datalist>

    <div style="display: flex; gap: 20px; align-items: flex-end; margin-top: 22px; flex-wrap: wrap">
      <div class="field" style="margin: 0; flex: 1; min-width: 260px">
        <label>Narration</label>
        <input v-model="head.narration" placeholder="Being goods sold" />
      </div>
      <div class="totals">
        <div class="row"><span class="muted">Items</span><span>{{ itemTotal.toFixed(2) }}</span></div>
        <div class="row"><span class="muted">Tax</span><span>{{ taxTotal.toFixed(2) }}</span></div>
        <div class="row grand"><span>{{ head.party || 'party' }}</span><span>{{ grandTotal.toFixed(2) }}</span></div>
        <div class="row">
          <span class="muted">posts as</span>
          <span class="muted">{{ partyIsDebit ? 'debit' : 'credit' }}</span>
        </div>
      </div>
    </div>

    <div style="margin-top: 18px">
      <button class="btn primary" :disabled="busy || !state.writesAllowedAtServer || !head.party.trim()" @click="submit()">
        {{ busy ? 'Posting…' : 'Post ' + docType + ' to Tally' }}
      </button>
    </div>
  </div>
</template>
