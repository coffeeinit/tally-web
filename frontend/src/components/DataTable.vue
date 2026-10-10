<script setup lang="ts">
const props = defineProps<{
  columns: string[];
  rows: Record<string, string>[];
  numeric?: string[];
  empty?: string;
}>();

function isNumeric(column: string): boolean {
  if (props.numeric && props.numeric.includes(column)) return true;
  return /amount|balance|debit|credit|value|total|closing|opening/i.test(column);
}

function pretty(column: string, value: string): string {
  if (value === undefined || value === null || value === '') return '';
  if (!isNumeric(column)) return value;
  const n = Number(String(value).replace(/,/g, ''));
  if (!Number.isFinite(n)) return value;
  return n.toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function signed(column: string, value: string): string {
  if (!isNumeric(column)) return '';
  const n = Number(String(value).replace(/,/g, ''));
  if (!Number.isFinite(n) || n === 0) return '';
  return n > 0 ? 'pos' : 'neg';
}

function label(column: string): string {
  return column.replace(/_/g, ' ');
}
</script>

<template>
  <div class="table-wrap">
    <table class="grid">
      <thead>
        <tr>
          <th v-for="column in columns" :key="column" :class="{ num: isNumeric(column) }">{{ label(column) }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, index) in rows" :key="index">
          <td v-for="column in columns" :key="column" :class="{ num: isNumeric(column), [signed(column, row[column])]: true }">
            {{ isNumeric(column) ? pretty(column, row[column]) : (row[column] || '') }}
          </td>
        </tr>
        <tr v-if="rows.length === 0">
          <td :colspan="columns.length">
            <div class="empty"><strong>Nothing to show</strong>{{ empty || 'No rows came back from the engine.' }}</div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
