<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card } from '@/components/ui/card'
import { BrowseModSporeDirectory, DetectModSporePath, GetModConfiguration, SetModSporePath } from '../platform'

const sourcePath = ref('')
const isBusy = ref(false)
const isBrowsing = ref(false)
const directory = ref({ path:'', parent:'', directories:[], executables:[] })
const browsePath = ref('')
const error = ref('')
const message = ref('')
let saveDelay = null
let isSavePending = false

async function refresh() {
  const configuration = await GetModConfiguration()
  sourcePath.value = configuration.sporePath
  error.value = configuration.sourceError
  message.value = configuration.sporePath && !configuration.sourceError
    ? 'Spore folder found.'
    : configuration.sporePath ? '' : 'Spore folder not found. Enter a path, browse, or use Detect.'
}

function cancelScheduledSave() {
  if (saveDelay !== null) window.clearTimeout(saveDelay)
  saveDelay = null
  isSavePending = false
}

function scheduleSave() {
  cancelScheduledSave()
  isSavePending = true
  error.value = ''
  message.value = 'Checking Spore folder…'
  saveDelay = window.setTimeout(flushSave, 600)
}

function flushSave() {
  if (saveDelay !== null) window.clearTimeout(saveDelay)
  saveDelay = null
  if (!isSavePending || isBusy.value) return
  isSavePending = false
  void perform(save)
}

async function perform(operation) {
  if (isBusy.value) return
  isBusy.value = true
  error.value = ''
  message.value = 'Checking Spore folder…'
  try { await operation() }
  catch (failure) { error.value = String(failure?.message || failure) }
  finally {
    isBusy.value = false
    if (saveDelay === null) flushSave()
  }
}

async function browse(path) {
  directory.value = await BrowseModSporeDirectory(path)
  browsePath.value = directory.value.path
  isBrowsing.value = true
  message.value = 'Select SporeApp.exe to check and save its folder.'
}

async function detect() {
  cancelScheduledSave()
  sourcePath.value = await DetectModSporePath()
  await refresh()
  isBrowsing.value = false
  if (!error.value) message.value = 'Spore folder detected and saved.'
}

async function save() {
  if (!sourcePath.value.trim()) {
    message.value = ''
    error.value = 'Spore folder not found. Enter a path, browse, or use Detect.'
    return
  }
  await SetModSporePath(sourcePath.value)
  await refresh()
  isBrowsing.value = false
  if (!error.value) message.value = 'Spore folder found and saved.'
}

async function selectSource(path) {
  cancelScheduledSave()
  sourcePath.value = path
  await save()
}

onMounted(() => perform(refresh))
onBeforeUnmount(flushSave)
</script>

<template>
  <Card class="management-card spore-configuration">
    <label for="config-spore-path"><strong>SPORE FOLDER</strong></label>
    <Input id="config-spore-path" v-model="sourcePath" :disabled="isBusy" aria-describedby="config-spore-status" @update:model-value="scheduleSave" />
    <p id="config-spore-status" :class="{ 'spore-error':error }" :role="error ? 'alert' : 'status'" aria-live="polite">{{ error || message }}</p>
    <div class="spore-actions">
      <Button variant="outline" :disabled="isBusy" @click="perform(detect)">DETECT</Button>
      <Button variant="outline" :disabled="isBusy" @click="perform(() => browse(sourcePath))">BROWSE</Button>
    </div>
    <div v-if="isBrowsing" class="spore-browser">
      <label for="config-spore-browse">FIND SPOREAPP.EXE</label>
      <div class="spore-actions">
        <Input id="config-spore-browse" v-model="browsePath" :disabled="isBusy" @keyup.enter="perform(() => browse(browsePath))" />
        <Button variant="outline" :disabled="isBusy" @click="perform(() => browse(browsePath))">GO</Button>
      </div>
      <button type="button" :disabled="isBusy" @click="perform(() => browse(directory.parent))">↑ PARENT FOLDER</button>
      <div class="spore-directory-list">
        <button v-for="path in directory.directories" :key="path" type="button" :disabled="isBusy" @click="perform(() => browse(path))">▸ {{ path.split(/[\\/]/).pop() }}</button>
        <button v-for="path in directory.executables" :key="path" type="button" :disabled="isBusy" @click="perform(() => selectSource(path))">SELECT {{ path.split(/[\\/]/).pop() }}</button>
      </div>
    </div>
  </Card>
</template>

<style scoped>
.spore-configuration, .spore-browser { display:flex; flex-direction:column; gap:12px; min-width:0; }
.spore-configuration label { font-size:11px; letter-spacing:.08em; }
.spore-configuration p { color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; }
.spore-actions { display:flex; align-items:center; flex-wrap:wrap; gap:12px; }
.spore-actions input { flex:1; min-width:120px; }
.spore-browser button { color:var(--primary); font-size:11px; text-align:left; cursor:pointer; overflow-wrap:anywhere; }
.spore-directory-list { display:flex; flex-direction:column; gap:12px; max-height:240px; overflow:auto; padding:12px; background:var(--muted); }
.spore-configuration .spore-error { color:#ff9b8c; }
</style>
