<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { api, type AiPreset } from '../api';
import { state, loadAiSettings, fail, notify } from '../store';

const presets = ref<AiPreset[]>([]);
const saving = ref(false);
const form = reactive({
  kind: 'openai',
  baseUrl: '',
  model: '',
  apiKey: '',
  maxSteps: 6,
  systemPrompt: '',
  allowWrites: false
});

onMounted(async () => {
  try {
    const res = await api.presets();
    presets.value = res.presets;
  } catch (err) {
    fail(err);
  }
  const current = state.aiSettings;
  if (current) {
    form.kind = current.kind;
    form.baseUrl = current.baseUrl;
    form.model = current.model;
    form.maxSteps = current.maxSteps;
    form.systemPrompt = current.systemPrompt;
    form.allowWrites = current.allowWrites;
  }
});

function applyPreset(preset: AiPreset) {
  form.kind = preset.kind;
  form.baseUrl = preset.baseUrl;
  form.model = preset.model;
}

async function save() {
  saving.value = true;
  try {
    const body: Record<string, unknown> = {
      kind: form.kind,
      baseUrl: form.baseUrl,
      model: form.model,
      maxSteps: Number(form.maxSteps) || 6,
      systemPrompt: form.systemPrompt,
      allowWrites: form.allowWrites
    };
    if (form.apiKey.trim()) body.apiKey = form.apiKey.trim();
    await api.saveAiSettings(body);
    form.apiKey = '';
    await loadAiSettings();
    notify('AI settings saved');
    state.settingsOpen = false;
  } catch (err) {
    fail(err);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <div class="overlay" @click.self="state.settingsOpen = false">
    <div class="modal">
      <h2>AI settings</h2>
      <div class="hint">
        Bring your own model. The token is stored on this machine only and is never sent back to the browser.
      </div>

      <div class="section-title">Quick presets</div>
      <div class="preset-row">
        <button v-for="preset in presets" :key="preset.id" class="chip" @click="applyPreset(preset)">
          {{ preset.label }}
        </button>
      </div>

      <div class="grid2">
        <div class="field">
          <label>Provider API</label>
          <select v-model="form.kind">
            <option value="openai">OpenAI-compatible (OpenAI, DeepSeek, Groq, Ollama, vLLM…)</option>
            <option value="anthropic">Anthropic Messages API</option>
            <option value="gemini">Google Gemini</option>
          </select>
        </div>
        <div class="field">
          <label>Model</label>
          <input v-model="form.model" placeholder="deepseek-chat" />
        </div>
      </div>

      <div class="field">
        <label>Base URL</label>
        <input v-model="form.baseUrl" placeholder="https://api.deepseek.com/v1" />
      </div>

      <div class="grid2">
        <div class="field">
          <label>API token</label>
          <input v-model="form.apiKey" type="password" :placeholder="state.aiSettings && state.aiSettings.apiKeySet ? 'stored (' + (state.aiSettings.apiKeyHint || '') + ') — leave blank to keep' : 'paste your token'" />
        </div>
        <div class="field">
          <label>Max tool steps per question</label>
          <input v-model="form.maxSteps" type="number" min="1" max="20" />
        </div>
      </div>

      <div class="field">
        <label>System prompt</label>
        <textarea v-model="form.systemPrompt" rows="4"></textarea>
      </div>

      <div class="field">
        <label class="switch">
          <input type="checkbox" v-model="form.allowWrites" />
          Allow the assistant to create ledgers and vouchers
        </label>
      </div>

      <div v-if="!state.writesAllowedAtServer" class="pill bad" style="margin-bottom: 12px">
        The server was started read-only. Writes stay blocked until it is started with --allow-writes.
      </div>

      <div style="display: flex; gap: 8px; justify-content: flex-end">
        <button class="btn ghost" @click="state.settingsOpen = false">Cancel</button>
        <button class="btn primary" :disabled="saving" @click="save()">{{ saving ? 'Saving…' : 'Save' }}</button>
      </div>
    </div>
  </div>
</template>
