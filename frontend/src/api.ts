// Typed client for the Go backend. Everything the UI shows comes from here,
// and the assistant uses exactly the same endpoints and the same engine.

export interface Company { id: string; name: string; folder?: string }
export interface Ledger { name: string; parent: string; opening_balance: number; closing_balance: number }
export interface Voucher { id: string; number: string; type: string; date: string; party: string; narration: string; amount: number }
export interface StockItem {
  name: string; parent?: string; base_unit?: string;
  closing_qty: number; closing_rate: number; closing_value: number;
  gst_rate?: number; hsn_code?: string;
}
export interface ItemLine {
  stock_item: string; quantity: number; rate: number;
  unit?: string; ledger?: string; discount?: number; amount?: number;
}
export interface Report { name: string; rows: Record<string, string>[]; raw?: string; note?: string }
export interface Health { ok: boolean; product: string; address: string; message?: string }
export interface InstanceInfo {
  id: string; kind: string; label: string; product: string;
  address: string; company?: string; is_default: boolean; health: Health;
}
export interface AiPreset {
  id: string; label: string; kind: string; baseUrl: string;
  model: string; local: boolean; needsKey: boolean; note: string;
}
export interface AiSettings {
  kind: string; baseUrl: string; model: string; maxSteps: number;
  systemPrompt: string; allowWrites: boolean; apiKeySet: boolean; apiKeyHint?: string;
}
export interface AiEvent {
  type: 'status' | 'assistant' | 'tool_call' | 'tool_result' | 'error' | 'done';
  step?: number; text?: string; tool?: string; args?: string; preview?: string; isError?: boolean;
}

const NL = String.fromCharCode(10);

function qs(params: Record<string, string | number | undefined>): string {
  const parts: string[] = [];
  for (const key of Object.keys(params)) {
    const value = params[key];
    if (value === undefined || value === '') continue;
    parts.push(encodeURIComponent(key) + '=' + encodeURIComponent(String(value)));
  }
  return parts.length ? '?' + parts.join('&') : '';
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  const text = await res.text();
  if (!res.ok) {
    let message = text;
    try {
      const parsed = JSON.parse(text);
      if (parsed && parsed.error) message = parsed.error;
    } catch (err) {
      void err;
    }
    throw new Error(message || ('HTTP ' + res.status));
  }
  return text ? (JSON.parse(text) as T) : (undefined as T);
}

export const api = {
  health: () => req<Record<string, unknown>>('/api/health'),
  instances: () => req<{ default: string; instances: InstanceInfo[] }>('/api/instances'),
  setDefault: (id: string) =>
    req<{ default: string }>('/api/instances/default', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id })
    }),

  companies: (instance?: string) =>
    req<{ instance: string; product: string; companies: Company[] }>('/api/companies' + qs({ instance })),
  ledgers: (instance?: string, company?: string) =>
    req<{ count: number; ledgers: Ledger[] }>('/api/ledgers' + qs({ instance, company })),
  vouchers: (p: { instance?: string; company?: string; type?: string; from?: string; to?: string; party?: string; limit?: number }) =>
    req<{ count: number; vouchers: Voucher[] }>('/api/vouchers' + qs(p)),
  report: (p: { instance?: string; company?: string; report: string; from?: string; to?: string }) =>
    req<{ report: Report }>('/api/reports' + qs(p)),
  stockItems: (instance?: string, company?: string) =>
    req<{ count: number; stock_items: StockItem[] }>('/api/stock-items' + qs({ instance, company })),

  createVoucher: (body: Record<string, unknown>) =>
    req<{ ok: boolean; detail?: string }>('/api/vouchers', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }),
  createLedger: (body: Record<string, unknown>) =>
    req<{ ok: boolean; detail?: string }>('/api/ledgers', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }),
  createStockItem: (body: Record<string, unknown>) =>
    req<{ ok: boolean; detail?: string }>('/api/stock-items', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }),
  rawXml: (body: Record<string, unknown>) =>
    req<{ result: string }>('/api/raw-xml', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }),

  presets: () => req<{ presets: AiPreset[] }>('/api/ai/presets'),
  aiSettings: () => req<{ settings: AiSettings; writesAllowedAtServer: boolean }>('/api/ai/settings'),
  saveAiSettings: (body: Record<string, unknown>) =>
    req<{ settings: AiSettings }>('/api/ai/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }),
  aiTools: () => req<{ tools: { name: string; description: string }[] }>('/api/ai/tools')
};

// EventSource cannot POST, so the assistant stream is read by hand.
export async function chatStream(
  payload: { messages: { role: string; content: string }[]; instance?: string; company?: string },
  onEvent: (event: AiEvent) => void,
  signal?: AbortSignal
): Promise<void> {
  const res = await fetch('/api/ai/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
    signal
  });
  if (!res.ok || !res.body) {
    const text = await res.text();
    let message = text;
    try {
      const parsed = JSON.parse(text);
      if (parsed && parsed.error) message = parsed.error;
    } catch (err) {
      void err;
    }
    throw new Error(message || 'the assistant request failed');
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    const frames = buffer.split(NL + NL);
    buffer = frames.pop() ?? '';
    for (const frame of frames) {
      const line = frame.split(NL).find((l) => l.startsWith('data: '));
      if (!line) continue;
      try {
        onEvent(JSON.parse(line.slice(6)) as AiEvent);
      } catch (err) {
        void err;
      }
    }
  }
}