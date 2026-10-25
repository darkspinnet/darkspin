<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { Button } from '@/components/ui/button'
import { ComboboxRoot, ComboboxAnchor, ComboboxInput, ComboboxTrigger, ComboboxPortal, ComboboxContent, ComboboxViewport, ComboboxItem, ComboboxEmpty } from 'reka-ui'
import { Check, Clipboard, ChevronDown } from '@lucide/vue'
import { ClipboardSetText, GetLuaConsole, KillLuaSession, RunLuaScript, StopLuaScript } from '../platform'

const emit = defineEmits(['state'])
const state = ref({ scripts:[], lines:[], phase:'idle', isActive:false, elapsedMs:0 })
const selected = ref('')
const search = ref('')
const isPickerOpen = ref(false)
const error = ref('')
const isBusy = ref(false)
const isKilling = ref(false)
const consoleElement = ref(null)
const isFollowing = ref(true)
const copiedSequence = ref(null)
let timer = null
let isRefreshing = false
const isRunning = computed(() => ['starting', 'running', 'stopping'].includes(state.value.phase))
const canRun = computed(() => !isBusy.value && !isKilling.value && !isRunning.value && !!selected.value && !state.value.inventoryError)
const canStop = computed(() => !isBusy.value && isRunning.value && !!state.value.isActive)
const canKill = computed(() => !isKilling.value && !!state.value.isActive)
const consoleError = computed(() => {
  if (error.value || state.value.inventoryError) return error.value || state.value.inventoryError
  const failure = state.value.failure || ''
  const output = (state.value.lines || []).map(line => line.text).join('\n')
  return failure && !output.includes(failure) ? failure : ''
})
const selectedName = computed(() => state.value.scripts?.find(script => script.path === selected.value)?.name || '')
const filteredScripts = computed(() => (state.value.scripts || []).filter(script => script.name.toLowerCase().includes(search.value.toLowerCase())))
function openPicker(isOpen) { isPickerOpen.value = isOpen; if (isOpen) search.value = '' }
const resultLabel = computed(() => {
  const phase = state.value.phase || 'idle'
  const label = phase === 'passed' ? 'Finished' : phase === 'interrupted' ? 'Stopped' : phase.charAt(0).toUpperCase() + phase.slice(1)
  return state.value.elapsedMs > 0 ? `${label} · ${(state.value.elapsedMs / 1000).toFixed(2)}s` : label
})

async function refresh() {
  if (isRefreshing) return
  isRefreshing = true
  try {
    state.value = await GetLuaConsole()
    emit('state', state.value)
    if (!selected.value) selected.value = state.value.script || state.value.scripts?.[0]?.path || ''
    await nextTick()
    if (isFollowing.value && consoleElement.value) consoleElement.value.scrollTop = consoleElement.value.scrollHeight
  } catch (failure) { error.value = String(failure) }
  finally { isRefreshing = false }
}

async function run() {
  isBusy.value = true
  error.value = ''
  try { await RunLuaScript(selected.value); isFollowing.value = true; await refresh() }
  catch (failure) { error.value = String(failure) }
  finally { isBusy.value = false }
}

async function stop() {
  isBusy.value = true
  error.value = ''
  try { await StopLuaScript(); await refresh() }
  catch (failure) { error.value = String(failure) }
  finally { isBusy.value = false }
}

async function kill() {
  if (isKilling.value) return
  isKilling.value = true
  isBusy.value = true
  error.value = ''
  try { await KillLuaSession(); await refresh() }
  catch (failure) { error.value = String(failure) }
  finally { isBusy.value = false; isKilling.value = false }
}

function followScroll() {
  const element = consoleElement.value
  if (element) isFollowing.value = element.scrollHeight - element.scrollTop - element.clientHeight < 32
}

async function copyOutput() {
  const failureMessage = consoleError.value
  error.value = ''
  const snapshot = state.value
  const output = [
    `Scenario: ${snapshot.script || 'none'}`,
    `Result: ${resultLabel.value}`,
    ...(snapshot.runId ? [`Session: ${snapshot.runId}`] : []),
    '',
    ...(snapshot.lines || []).map(line => line.text),
    ...(failureMessage ? [failureMessage] : []),
  ].join('\n')
  try {
    const isCopied = await ClipboardSetText(output)
    if (isCopied === false) throw new Error('Clipboard copy failed')
    copiedSequence.value = snapshot.lines?.at(-1)?.sequence ?? 0
  } catch (failure) { error.value = String(failure) }
}
onMounted(() => { void refresh(); timer = window.setInterval(() => void refresh(), 1000) })
onBeforeUnmount(() => { if (timer !== null) window.clearInterval(timer) })
defineExpose({ run, stop, kill, isBusy, canRun, canStop, canKill })
</script>

<template>
  <section class="lua-page management-frame" aria-label="Lua scenario runner">
    <Teleport to="#lua-scenario-picker" defer><div class="lua-controls">
      <div class="lua-select">
        <ComboboxRoot v-model="selected" :open="isPickerOpen" :open-on-click="true" :open-on-focus="true" :ignore-filter="true" :disabled="isBusy || isRunning || !!state.inventoryError" @update:open="openPicker">
          <ComboboxAnchor as-child><div class="lua-picker-anchor">
            <ComboboxInput v-model="search" :disabled="isBusy || isRunning || !!state.inventoryError" :display-value="() => selectedName" as-child><input aria-label="Search Lua scenarios" placeholder="Search scenarios…" class="lua-picker-input" /></ComboboxInput>
            <ComboboxTrigger as-child><button aria-label="Show Lua scenarios" class="lua-picker-toggle"><ChevronDown :size="16" /></button></ComboboxTrigger>
          </div></ComboboxAnchor>
          <ComboboxPortal><ComboboxContent position="popper" align="start" :side-offset="4" :collision-padding="8" as-child><div class="lua-picker-content">
            <ComboboxViewport as-child><div class="lua-picker-viewport">
              <ComboboxItem v-for="script in filteredScripts" :key="script.path" :value="script.path" as-child><div class="lua-picker-item">{{ script.name }}</div></ComboboxItem>
              <ComboboxEmpty as-child><p class="lua-picker-empty">No matching scenarios.</p></ComboboxEmpty>
            </div></ComboboxViewport>
          </div></ComboboxContent></ComboboxPortal>
        </ComboboxRoot>
      </div>
    </div></Teleport>
    <div ref="consoleElement" class="lua-console" role="log" aria-label="Scenario activity and failure traces" tabindex="0" @scroll="followScroll">
      <p v-if="!state.lines?.length" class="lua-empty">Activity, completion time and failure traces will appear here.</p>
      <div v-for="line in state.lines || []" :key="line.sequence" class="lua-line">{{ line.text }}</div>
      <p v-if="consoleError" class="lua-error" role="alert">{{ consoleError }}</p>
    </div>
    <div class="lua-footer">
      <p class="lua-status" :class="state.phase" role="status">{{ resultLabel }}</p>
      <Button variant="ghost" size="icon" :disabled="!state.lines?.length && !consoleError" :aria-label="copiedSequence === state.lines?.at(-1)?.sequence ? 'Output copied' : 'Copy output'" :title="copiedSequence === state.lines?.at(-1)?.sequence ? 'Output copied' : 'Copy output'" @click="copyOutput"><Check v-if="copiedSequence === state.lines?.at(-1)?.sequence" :size="16" /><Clipboard v-else :size="16" /></Button>
    </div>
  </section>
</template>

<style scoped>
.lua-page { display:flex; flex-direction:column; gap:12px; }
.lua-controls { display:flex; align-items:center; width:100%; }
.lua-select { flex:1 1 240px; min-width:180px; }
.lua-picker-anchor { display:flex; width:100%; min-height:40px; border:1px solid var(--border); border-radius:6px; background:var(--background); }
.lua-picker-input { flex:1; min-width:0; width:100%; padding:8px 10px; outline:none; font-size:12px; background:transparent; color:var(--foreground); }
.lua-picker-toggle { display:flex; align-items:center; padding:0 10px; flex-shrink:0; }
.lua-picker-content { z-index:100; width:var(--reka-combobox-trigger-width); min-width:180px; max-width:var(--reka-combobox-content-available-width); max-height:min(260px,var(--reka-combobox-content-available-height)); overflow:hidden; padding:4px; border:1px solid var(--border); border-radius:6px; background:var(--popover); color:var(--popover-foreground); box-shadow:0 8px 24px #0008; }
.lua-picker-viewport { min-height:0; overflow-y:auto; }
.lua-picker-empty { padding:8px 10px; color:var(--muted-foreground); font-size:12px; }
.lua-picker-item { padding:6px 10px; border-radius:4px; font-size:12px; cursor:pointer; }
.lua-picker-item[data-highlighted] { background:var(--accent); color:var(--accent-foreground); }
.lua-footer { display:flex; align-items:center; justify-content:space-between; gap:12px; flex-shrink:0; }
.lua-status { color:var(--muted-foreground); font-size:12px; }
.lua-status.passed { color:#7fd6a0; }
.lua-status.failed { color:#f49b9b; }
.lua-error { color:#f49b9b; white-space:pre-wrap; overflow-wrap:anywhere; }
.lua-console { flex:1; min-height:150px; overflow:auto; border:1px solid var(--border); border-radius:8px; background:#070e14; padding:12px; font:11px/1.6 Consolas,monospace; }
.lua-line { white-space:pre-wrap; overflow-wrap:anywhere; }
.lua-empty { color:#8196a5; }
</style>
