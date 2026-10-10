<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { api } from '../api';
import { state, fail, notify } from '../store';

const tab = ref('voucher');
const busy = ref(false);

const voucherTypes = ['Sales', 'Purchase', 'Receipt', 'Payment', 'Journal', 'Contra', 'Credit Note', 'Debit Note'];

const voucher = reactive({
  type: 'Sales',
  date: new Date().toISOString().slice(0, 10),
  party: '',
  narration: '',
  entries: [
    { ledger: '', amount: 0, is_debit: true },
    { ledger: '', amount: 0, is_debit: false }
  ]
});

const ledger = reactive({ name: '', parent: 'Sundry Debtors' });
const raw = ref('');

const debitTotal = computed(() =>
  voucher.entries.filter((e) => e.is_debit).reduce((sum, e) => sum + (Number(e.amount) || 0), 0)
);
const creditTotal = computed(() =>
  voucher.entries.filter((e) => !e.is_debit).reduce((sum, e) => sum + (Number(e.amount) || 0), 0)
);
const balanced = computed(() => Math.abs(debitTotal.value - creditTotal.value) < 0.005);

function addEntry(isDebit: boolean) {
  voucher.entries.push({ ledger: '', amount: 0, is_debit: isDebit });
}

function removeEntry(index: number) {
  voucher.entries.splice(index, 1);
}

async function submitVoucher() {
  busy.value = true;
  try {
    const res = await api.createVoucher({
      instance: state.defaultInstance,
      company: state.company,
      type: voucher.type,
      date: voucher.date,
      party: voucher.party,
      narration: voucher.narration,
      entries: voucher.entries.map((e) => ({ ledger: e.ledger, amount: Number(e.amount) || 0, is_debit: e.is_debit }))
    });
    notify(res.detail || 'Voucher accepted');
  } catch (err) {
    fail(err);
  } finally {
    busy.value = false;
  }
}

async function submitLedger() {
  busy.value = true;
  try {
    const res = await api.createLedger({
      instance: state.defaultInstance,
      company: state.company,
      name: ledger.name,
      parent: ledger.parent
    });
    notify(res.detail || 'Ledger accepted');
    ledger.name = '';
  } catch (err) {
    fail(err);
  } finally {
    busy.value = false;
  }
}

async function submitRaw() {
  busy.value = true;
  try {
    const res = await api.rawXml({ instance: state.defaultInstance, company: state.company, xml: raw.value });
    notify('Tally replied with ' + (res.result ? res.result.length : 0) + ' characters');
  } catch (err) {
    fail(err);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="view-head">
    <h2>Compose</h2>
    <span class="sub">Writes go to Tally, so Tally validates them and the audit trail stays intact</span>
    <div style="flex: 1"></div>
    <span class="pill" :class="state.writesAllowedAtServer ? 'accent' : 'bad'">
      {{ state.writesAllowedAtServer ? 'writes enabled' : 'server is read-only' }}
    </span>
  </div>

  <div class="view-body">
    <div class="suggestions" style="margin-bottom: 14px">
      <button class="chip" :class="{ accent: tab === 'voucher' }" @click="tab = 'voucher'">Voucher</button>
      <button class="chip" :class="{ accent: tab === 'ledger' }" @click="tab = 'ledger'">Ledger</button>
      <button class="chip" :class="{ accent: tab === 'raw' }" @click="tab = 'raw'">Raw XML</button>
    </div>

    <div v-if="tab === 'voucher'" class="card">
      <h3>Create a voucher</h3>
      <div class="grid2" style="margin-top: 10px">
        <div class="field">
          <label>Type</label>
          <select v-model="voucher.type">
            <option v-for="t in voucherTypes" :key="t" :value="t">{{ t }}</option>
          </select>
        </div>
        <div class="field">
          <label>Date</label>
          <input v-model="voucher.date" type="date" />
        </div>
        <div class="field">
          <label>Party ledger</label>
          <input v-model="voucher.party" placeholder="Sharma Enterprises" />
        </div>
        <div class="field">
          <label>Narration</label>
          <input v-model="voucher.narration" placeholder="Being goods sold" />
        </div>
      </div>

      <div class="section-title">Entries</div>
      <div v-for="(entry, index) in voucher.entries" :key="index" style="display: flex; gap: 8px; margin-bottom: 8px">
        <input v-model="entry.ledger" placeholder="Ledger name" style="flex: 2" />
        <input v-model="entry.amount" type="number" step="0.01" placeholder="Amount" style="flex: 1" />
        <select v-model="entry.is_debit" style="width: 110px">
          <option :value="true">Debit</option>
          <option :value="false">Credit</option>
        </select>
        <button class="btn sm ghost" @click="removeEntry(index)">✕</button>
      </div>

      <div style="display: flex; gap: 8px; margin: 10px 0">
        <button class="btn sm" @click="addEntry(true)">+ Debit</button>
        <button class="btn sm" @click="addEntry(false)">+ Credit</button>
      </div>

      <div class="stats" style="margin-bottom: 12px">
        <div class="stat"><div class="label">Debits</div><div class="value">{{ debitTotal.toFixed(2) }}</div></div>
        <div class="stat"><div class="label">Credits</div><div class="value">{{ creditTotal.toFixed(2) }}</div></div>
        <div class="stat">
          <div class="label">Balanced</div>
          <div class="value" :style="{ color: balanced ? 'var(--pos)' : 'var(--neg)' }">{{ balanced ? 'yes' : 'no' }}</div>
        </div>
      </div>

      <button class="btn primary" :disabled="busy || !balanced || !voucher.party" @click="submitVoucher()">
        {{ busy ? 'Sending…' : 'Create voucher in Tally' }}
      </button>
    </div>

    <div v-else-if="tab === 'ledger'" class="card">
      <h3>Create a ledger</h3>
      <div class="grid2" style="margin-top: 10px">
        <div class="field">
          <label>Ledger name</label>
          <input v-model="ledger.name" placeholder="Acme Industries" />
        </div>
        <div class="field">
          <label>Parent group</label>
          <input v-model="ledger.parent" placeholder="Sundry Debtors" />
        </div>
      </div>
      <button class="btn primary" :disabled="busy || !ledger.name" @click="submitLedger()">
        {{ busy ? 'Sending…' : 'Create ledger in Tally' }}
      </button>
    </div>

    <div v-else class="card">
      <h3>Raw Tally XML</h3>
      <div class="hint faint" style="margin-bottom: 8px">
        The escape hatch. Anything this UI has no screen for is still reachable from here.
      </div>
      <textarea v-model="raw" rows="10" placeholder="<ENVELOPE>…</ENVELOPE>"></textarea>
      <div style="margin-top: 10px">
        <button class="btn primary" :disabled="busy || !raw.trim()" @click="submitRaw()">
          {{ busy ? 'Sending…' : 'Send to Tally' }}
        </button>
      </div>
    </div>
  </div>
</template>
