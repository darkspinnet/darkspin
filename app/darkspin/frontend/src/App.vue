<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { Authorize, Cancel, GetStatus, Patch, Play, SetAccount, SetSkipCinematic, SignOut } from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'

const status = ref({ state:'idle', message:'Ready', account:'', manifestUrl:'', gameDirectory:'', version:'', progress:0, requiredFile:0, deleteFile:0, downloadByte:0, isAuthenticated:false, isAutoPlayRequested:false, isPatchEnabled:false, isCinematicSkipped:false })
const account = ref(localStorage.getItem('darkspin.account') || '')
const isAutoPatch = ref(localStorage.getItem('darkspin.autoPatch') !== 'false')
const isAutoPlay = ref(localStorage.getItem('darkspin.autoPlay') === 'true')
const isSkipCinematic = ref(localStorage.getItem('darkspin.skipCinematic') !== 'false')
const log = ref([])
const automaticPhase = ref('')
const isBusy = computed(() => ['authorizing','checking','patching','launching'].includes(status.value.state))
const isPlayEnabled = computed(() => !isBusy.value)
const progressStyle = computed(() => ({ width:`${Math.max(0, Math.min(100, status.value.progress || 0))}%` }))
const authLabel = computed(() => status.value.isAuthenticated ? 'Signed in' : 'OAuth required')

watch(account, value => localStorage.setItem('darkspin.account', value))
watch(isAutoPatch, value => localStorage.setItem('darkspin.autoPatch', String(value)))
watch(isAutoPlay, value => localStorage.setItem('darkspin.autoPlay', String(value)))
watch(isSkipCinematic, value => localStorage.setItem('darkspin.skipCinematic', String(value)))

onMounted(async () => {
  EventsOn('darkspin:status', async next => {
    status.value = { ...status.value, ...next }
    if (automaticPhase.value === 'patching' && next.state === 'ready') {
      automaticPhase.value = 'launching'
      await play()
    }
  })
  EventsOn('darkspin:log', line => { log.value = [line, ...log.value].slice(0, 60) })
  status.value = { ...status.value, ...(await GetStatus()) }
  if (status.value.account) account.value = status.value.account
  isAutoPlay.value = isAutoPlay.value || status.value.isAutoPlayRequested
  isSkipCinematic.value = isSkipCinematic.value || status.value.isCinematicSkipped
  if (isAutoPlay.value) await automaticStart()
})

async function signIn() {
  await SetAccount(account.value)
  try {
    await Authorize(account.value)
    status.value = { ...status.value, ...(await GetStatus()) }
    if (isAutoPlay.value && automaticPhase.value === '') await continueAutomaticStart()
  } catch (error) { log.value = [String(error), ...log.value] }
}
async function signOut() { await SignOut() }
async function patch() {
  if (isBusy.value) { await Cancel(); return }
  await Patch()
}
async function play() {
  await SetAccount(account.value)
  await SetSkipCinematic(isSkipCinematic.value)
  await Play()
}
async function automaticStart() {
  await SetSkipCinematic(isSkipCinematic.value)
  automaticPhase.value = 'authorizing'
  if (account.value && !status.value.isAuthenticated) {
    await signIn()
    status.value = { ...status.value, ...(await GetStatus()) }
    if (!status.value.isAuthenticated) { automaticPhase.value = ''; return }
  }
  await continueAutomaticStart()
}
async function continueAutomaticStart() {
  if (isAutoPatch.value && status.value.isPatchEnabled) {
    automaticPhase.value = 'patching'
    await Patch()
    return
  }
  automaticPhase.value = 'launching'
  await play()
}
</script>

<template>
  <main class="shell">
    <header class="hero">
      <div><p class="eyebrow">RESURRECTION CAPSULE COMMUNITY NETWORK</p><h1>Dark Spin</h1><p class="subtitle">Patch. Authenticate. Enter the Helix.</p></div>
      <div class="hero-actions">
        <div class="version">DARK SPIN {{ status.version }}</div>
        <button class="play" :disabled="!isPlayEnabled" @click="play">PLAY <span>&rsaquo;</span></button>
      </div>
    </header>
    <section class="grid">
      <article class="panel">
        <div class="panel-heading"><span class="step">01</span><div><h2>Identity</h2><p>{{ authLabel }}</p></div></div>
        <label class="field-label" for="account">Account</label>
        <input id="account" v-model="account" :disabled="isBusy" placeholder="pilot@example.com">
        <div class="button-row">
          <button v-if="!status.isAuthenticated" class="secondary" :disabled="isBusy || !account" @click="signIn">Sign in with OAuth</button>
          <button v-else class="secondary" :disabled="isBusy" @click="signOut">Sign out</button>
          <span class="auth-dot" :class="{ online:status.isAuthenticated }"></span>
        </div>
      </article>
      <article class="panel">
        <div class="panel-heading"><span class="step">02</span><div><h2>Game files</h2><p>{{ status.isPatchEnabled ? 'Manifest connected' : 'Bundled installation' }}</p></div></div>
        <div class="metrics"><div><strong>{{ status.requiredFile }}</strong><span>updates</span></div><div><strong>{{ status.deleteFile }}</strong><span>removals</span></div><div><strong>{{ status.gameDirectory ? 'READY' : 'MISSING' }}</strong><span>install</span></div></div>
        <button class="secondary full" :disabled="isBusy || !status.isPatchEnabled" @click="patch">{{ isBusy && status.state === 'patching' ? 'Cancel patch' : 'Check & patch' }}</button>
      </article>
    </section>
    <section class="status-panel">
      <div class="status-line"><span>{{ status.state }}</span><strong>{{ status.message }}</strong></div>
      <div class="progress"><div :style="progressStyle"></div></div>
      <div v-if="log.length" class="log"><p v-for="line in log" :key="line">{{ line }}</p></div>
    </section>
    <footer>
      <div class="toggles"><label><input v-model="isAutoPatch" type="checkbox"> Auto patch</label><label><input v-model="isAutoPlay" type="checkbox"> Auto play after OAuth</label><label><input v-model="isSkipCinematic" type="checkbox"> Skip cinematics</label></div>
    </footer>
  </main>
</template>

<style scoped>
.hero-actions {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.hero-actions .play {
  min-width: 190px;
  padding: 12px 18px;
}

@media (max-width: 800px) {
  .hero-actions {
    align-items: flex-end;
    flex-direction: column;
  }

  .hero-actions .play {
    width: auto;
  }
}
</style>
