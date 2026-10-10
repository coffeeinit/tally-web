<script setup lang="ts">
import { nextTick, ref, watch } from 'vue';
import { state, ask, fail } from '../store';

const input = ref('');
const body = ref<HTMLElement | null>(null);

const suggestions = [
  'Summarise this month in five bullets',
  'Top debtors by outstanding',
  'Sales vouchers above 100000',
  'Draft a journal for depreciation of 12500',
  'Which ledgers have a zero balance?'
];

function providerLabel(): string {
  const s = state.aiSettings;
  if (!s) return 'not configured';
  return s.model || 'no model';
}

function configured(): boolean {
  const s = state.aiSettings;
  if (!s) return false;
  if (s.apiKeySet) return true;
  return s.baseUrl.indexOf('localhost') >= 0 || s.baseUrl.indexOf('127.0.0.1') >= 0;
}

async function send(text?: string) {
  const question = (text === undefined ? input.value : text).trim();
  if (!question || state.busy) return;
  input.value = '';
  try {
    await ask(question);
  } catch (err) {
    fail(err);
  }
}

function scrollDown() {
  nextTick(() => {
    if (body.value) body.value.scrollTop = body.value.scrollHeight;
  });
}

watch(() => state.chat.length, scrollDown);
watch(() => state.chat.map((entry) => entry.text.length + entry.steps.length).join('|'), scrollDown);
</script>

<template>
  <aside class="dock">
    <div class="dock-head">
      <h3>Copilot</h3>
      <span class="pill" :class="configured() ? 'accent' : 'bad'">{{ providerLabel() }}</span>
      <span v-if="state.aiSettings && state.aiSettings.allowWrites" class="pill bad">can write</span>
      <span v-else class="pill">read only</span>
      <div class="grow" style="flex: 1"></div>
      <button class="btn sm ghost" @click="state.settingsOpen = true">⚙</button>
      <button class="btn sm ghost" @click="state.aiOpen = false">✕</button>
    </div>

    <div class="dock-body" ref="body">
      <div v-if="state.chat.length === 0" class="msg">
        <div class="who">getting started</div>
        <div class="bubble muted">
          The copilot runs on your own provider and token. It reads the company you have
          selected and can create vouchers only if you allow it.
        </div>
        <div class="suggestions">
          <button v-for="s in suggestions" :key="s" class="chip" @click="send(s)">{{ s }}</button>
        </div>
      </div>

      <div v-for="entry in state.chat" :key="entry.id" class="msg" :class="entry.role">
        <div class="who">{{ entry.role === 'user' ? 'you' : 'copilot' }}</div>

        <div v-if="entry.steps.length" class="steps">
          <div v-for="(step, index) in entry.steps" :key="index" class="step" :class="{ err: step.isError }">
            <div><span class="tool">{{ step.tool }}</span><span class="args"> {{ step.args }}</span></div>
            <div v-if="step.preview" class="preview">{{ step.preview }}</div>
          </div>
        </div>

        <div v-if="entry.text" class="bubble">{{ entry.text }}</div>
        <div v-else-if="entry.pending" class="bubble faint"><span class="spinner"></span> working…</div>
      </div>
    </div>

    <div class="dock-foot">
      <div v-if="!configured()" class="faint" style="font-size: 12px; margin-bottom: 7px">
        Add your token in AI settings to start.
      </div>
      <div class="composer">
        <textarea
          v-model="input"
          rows="1"
          placeholder="Ask about the books…  (Enter to send, Shift+Enter for a new line)"
          @keydown.enter.exact.prevent="send()"
        ></textarea>
        <button class="btn primary" :disabled="state.busy || !input.trim()" @click="send()">Send</button>
      </div>
    </div>
  </aside>
</template>
