<script setup lang="ts">
import { onMounted, watch } from 'vue';
import { state, useInstance, setCompany, loadCompanies, fail, notify } from '../store';

async function pick(id: string) {
  try {
    await useInstance(id);
    notify('Now using ' + id);
  } catch (err) {
    fail(err);
  }
}

onMounted(loadCompanies);
watch(() => state.defaultInstance, loadCompanies);
</script>

<template>
  <div class="view-head">
    <h2>Connections</h2>
    <span class="sub">A running Tally over its XML port, on-disk files through the TallyWeb backend, or the built-in demo</span>
    <div style="flex: 1"></div>
    <button class="btn" @click="loadCompanies()">Refresh</button>
  </div>

  <div class="view-body">
    <div class="section-title">Tally instances</div>
    <div class="cards">
      <div
        v-for="inst in state.instances"
        :key="inst.id"
        class="card"
        :class="{ selected: inst.id === state.defaultInstance }"
      >
        <div class="row">
          <h3>{{ inst.label }}</h3>
          <span class="pill" :class="inst.health.ok ? 'ok' : 'bad'">
            <span class="dot"></span>{{ inst.health.ok ? 'online' : 'offline' }}
          </span>
        </div>
        <div class="kv"><span>id</span><span>{{ inst.id }}</span></div>
        <div class="kv"><span>kind</span><span>{{ inst.kind }}</span></div>
        <div class="kv"><span>product</span><span>{{ inst.product }}</span></div>
        <div class="kv"><span>address</span><span>{{ inst.address }}</span></div>
        <div class="kv" v-if="inst.health.message"><span>status</span><span>{{ inst.health.message }}</span></div>
        <div style="margin-top: 10px">
          <button class="btn sm primary" :disabled="inst.id === state.defaultInstance" @click="pick(inst.id)">
            {{ inst.id === state.defaultInstance ? 'In use' : 'Use this' }}
          </button>
        </div>
      </div>
    </div>

    <div class="section-title">Companies on {{ state.defaultInstance }}</div>
    <div class="cards">
      <div
        v-for="c in state.companies"
        :key="c.id"
        class="card"
        :class="{ selected: c.name === state.company }"
        style="cursor: pointer"
        @click="setCompany(c.name)"
      >
        <h3>{{ c.name }}</h3>
        <div class="kv"><span>folder</span><span>{{ c.folder || c.id }}</span></div>
      </div>
      <div v-if="state.companies.length === 0" class="card">
        <div class="empty">
          <strong>No companies</strong>
          This instance is not reachable. Check that Tally is running with its XML server enabled,
          or that the TallyWeb backend is up on its port.
        </div>
      </div>
    </div>
  </div>
</template>
