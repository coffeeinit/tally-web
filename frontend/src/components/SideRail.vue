<script setup lang="ts">
defineProps<{ view: string }>();
const emit = defineEmits<{ (e: 'change', value: string): void }>();

// Inline SVG rather than an icon font: no dependency, and it renders the same
// everywhere.
const items = [
  { id: 'invoice', label: 'New invoice', path: 'M6 3h9l4 4v14H6zM14 3v5h5M9 12h7M9 16h7' },
  { id: 'daybook', label: 'Day Book', path: 'M4 6h16M4 12h16M4 18h10' },
  { id: 'ledgers', label: 'Ledgers', path: 'M5 4h11a3 3 0 0 1 3 3v13H8a3 3 0 0 1-3-3zM8 8h7M8 12h7' },
  { id: 'stock', label: 'Stock items', path: 'M4 8l8-4 8 4v8l-8 4-8-4zM4 8l8 4 8-4M12 12v8' },
  { id: 'reports', label: 'Reports', path: 'M4 20V11M10 20V4M16 20v-6M22 20H2' },
  { id: 'compose', label: 'Payments, journals, raw XML', path: 'M12 5v14M5 12h14' },
  { id: 'companies', label: 'Connections', path: 'M3 10.5 12 3l9 7.5V21H3zM9 21v-7h6v7' }
];
</script>

<template>
  <nav class="rail">
    <div class="rail-logo">T</div>
    <button
      v-for="item in items"
      :key="item.id"
      class="rail-item"
      :class="{ active: view === item.id }"
      :title="item.label"
      :aria-label="item.label"
      @click="emit('change', item.id)"
    >
      <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor"
           stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
        <path :d="item.path" />
      </svg>
    </button>
    <div class="rail-spacer"></div>
  </nav>
</template>
