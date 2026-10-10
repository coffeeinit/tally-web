import { reactive } from 'vue';
import { api, chatStream, type AiSettings, type Company, type InstanceInfo } from './api';

export interface ChatStep { tool: string; args: string; preview?: string; isError?: boolean }
export interface ChatEntry {
  id: number;
  role: 'user' | 'assistant';
  text: string;
  steps: ChatStep[];
  pending?: boolean;
}

interface State {
  instances: InstanceInfo[];
  defaultInstance: string;
  company: string;
  companies: Company[];
  aiSettings: AiSettings | null;
  writesAllowedAtServer: boolean;
  chat: ChatEntry[];
  aiOpen: boolean;
  settingsOpen: boolean;
  busy: boolean;
  error: string;
  toast: string;
}

export const state = reactive<State>({
  instances: [],
  defaultInstance: '',
  company: localStorage.getItem('twui.company') || '',
  companies: [],
  aiSettings: null,
  writesAllowedAtServer: false,
  chat: [],
  aiOpen: true,
  settingsOpen: false,
  busy: false,
  error: '',
  toast: ''
});

let chatSeq = 1;

export function notify(message: string): void {
  state.toast = message;
  window.setTimeout(() => {
    if (state.toast === message) state.toast = '';
  }, 4000);
}

export function fail(err: unknown): void {
  state.error = err instanceof Error ? err.message : String(err);
}

export function clearError(): void {
  state.error = '';
}

export async function loadInstances(): Promise<void> {
  const res = await api.instances();
  state.instances = res.instances;
  state.defaultInstance = res.default;
}

export async function loadCompanies(): Promise<void> {
  try {
    const res = await api.companies(state.defaultInstance);
    state.companies = res.companies || [];
    if (!state.company && state.companies.length > 0) setCompany(state.companies[0].name);
  } catch (err) {
    state.companies = [];
    fail(err);
  }
}

export function setCompany(name: string): void {
  state.company = name;
  localStorage.setItem('twui.company', name);
}

export async function useInstance(id: string): Promise<void> {
  await api.setDefault(id);
  state.defaultInstance = id;
  state.company = '';
  localStorage.removeItem('twui.company');
  await loadCompanies();
}

export async function loadAiSettings(): Promise<void> {
  try {
    const res = await api.aiSettings();
    state.aiSettings = res.settings;
    state.writesAllowedAtServer = res.writesAllowedAtServer;
  } catch (err) {
    fail(err);
  }
}

export function newChatEntry(role: 'user' | 'assistant', text: string): ChatEntry {
  const entry: ChatEntry = { id: chatSeq++, role, text, steps: [] };
  state.chat.push(entry);
  return entry;
}

export async function ask(question: string): Promise<void> {
  if (!question.trim()) return;
  newChatEntry('user', question.trim());
  const answer = newChatEntry('assistant', '');
  answer.pending = true;

  const history = state.chat
    .filter((entry) => !entry.pending && entry.text)
    .map((entry) => ({ role: entry.role, content: entry.text }));

  state.busy = true;
  try {
    await chatStream(
      { messages: history, instance: state.defaultInstance, company: state.company },
      (event) => {
        if (event.type === 'assistant' && event.text) answer.text = event.text;
        if (event.type === 'tool_call') {
          answer.steps.push({ tool: event.tool || 'tool', args: event.args || '' });
        }
        if (event.type === 'tool_result') {
          const last = answer.steps[answer.steps.length - 1];
          if (last) {
            last.preview = event.preview;
            last.isError = event.isError;
          }
        }
        if (event.type === 'error') {
          const line = 'Error: ' + (event.text || 'unknown error');
          answer.text = answer.text ? answer.text + String.fromCharCode(10, 10) + line : line;
        }
      }
    );
  } catch (err) {
    answer.text = 'Error: ' + (err instanceof Error ? err.message : String(err));
  } finally {
    answer.pending = false;
    state.busy = false;
  }
}
