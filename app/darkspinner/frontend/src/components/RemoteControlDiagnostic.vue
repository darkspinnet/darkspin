<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Download, RefreshCw, Trash2 } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { DialogTitle } from '@/components/ui/dialog'
import LauncherDialog from './LauncherDialog.vue'
import { ListRemoteControlDiagnostics, DownloadRemoteControlDiagnostic, DeleteRemoteControlDiagnostic, OpenControlDiagnosticFolder } from '../platform'
import { panelError } from '../control_panel'
import { formatControlTime } from '../control_time'

const props = defineProps({ profile:Object, panel:Object })
const emit = defineEmits(['busy', 'invalidated'])
const kind = ref('log')
const files = ref([])
const next = ref('')
const isBusy = ref(false)
const isLoaded = ref(false)
const pendingFile = ref(null)
const error = ref('')
const savedPath = ref('')
const folderLabels = {
  log: 'OPEN LOG FOLDER ↗',
  bug: 'OPEN BUG FOLDER ↗',
  snapshot: 'OPEN SNAPSHOT FOLDER ↗',
}
const operations = computed(() => props.panel?.server?.supported_operations || [])
let generation = 0
let isDisposed = false

watch(isBusy, next => emit('busy', next), { flush:'sync' })
watch(() => [props.profile?.serverAddress, props.profile?.loginName, kind.value], () => {
  generation++
  files.value = []
  next.value = ''
  isLoaded.value = false
  pendingFile.value = null
  error.value = ''
  savedPath.value = ''
  isBusy.value = false
  void read()
}, { flush:'sync' })

function isCurrent(current) { return !isDisposed && current === generation }
function failed(failure) {
  error.value = panelError(failure)
  if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
}
async function read(isMore = false) {
  if (isBusy.value || !props.profile || (isMore && !next.value)) return
  const current = generation
  const profile = { ...props.profile }
  const requestedKind = kind.value
  const after = isMore ? next.value : ''
  isBusy.value = true
  error.value = ''
  if (!isMore) { files.value = []; next.value = ''; isLoaded.value = false }
  try {
    const result = await ListRemoteControlDiagnostics(profile.serverAddress, profile.loginName, requestedKind, after)
    if (!isCurrent(current)) return
    files.value = isMore ? [...files.value, ...(result.files || [])] : result.files || []
    next.value = result.next || ''
    isLoaded.value = true
  } catch (failure) { if (isCurrent(current)) failed(failure) }
  finally { if (isCurrent(current)) isBusy.value = false }
}
function switchKind(requestedKind) {
  if (isBusy.value || pendingFile.value) return
  kind.value = requestedKind
}
async function download(file) {
  if (isBusy.value || !operations.value.includes('admin.diagnostics.download')) return
  const current = generation
  const profile = { ...props.profile }
  const requestedKind = kind.value
  isBusy.value = true
  error.value = ''
  savedPath.value = ''
  try {
    const result = await DownloadRemoteControlDiagnostic(profile.serverAddress, profile.loginName, requestedKind, file.name)
    if (isCurrent(current)) savedPath.value = result.path
  } catch (failure) { if (isCurrent(current)) failed(failure) }
  finally { if (isCurrent(current)) isBusy.value = false }
}
function confirmDelete(file) {
  if (isBusy.value || !operations.value.includes('admin.diagnostics.delete')) return
  pendingFile.value = { kind:kind.value, name:file.name }
  error.value = ''
}
function cancelDelete() {
  if (isBusy.value) return
  pendingFile.value = null
  error.value = ''
}
async function deleteFile() {
  const file = pendingFile.value
  if (!file || isBusy.value || !operations.value.includes('admin.diagnostics.delete')) return
  const current = generation
  const profile = { ...props.profile }
  isBusy.value = true
  error.value = ''
  try {
    await DeleteRemoteControlDiagnostic(profile.serverAddress, profile.loginName, file.kind, file.name)
    if (!isCurrent(current)) return
    files.value = files.value.filter(entry => entry.name !== file.name)
    pendingFile.value = null
  } catch (failure) { if (isCurrent(current)) failed(failure) }
  finally { if (isCurrent(current)) isBusy.value = false }
}
async function openFolder() {
  if (isBusy.value || !savedPath.value) return
  const current = generation
  isBusy.value = true
  error.value = ''
  try { await OpenControlDiagnosticFolder(props.profile.serverAddress) }
  catch (failure) { if (isCurrent(current)) failed(failure) }
  finally { if (isCurrent(current)) isBusy.value = false }
}
function size(bytes) { return Number(bytes).toLocaleString() }
onBeforeUnmount(() => { isDisposed = true; generation++; emit('busy', false) })
void read()
</script>

<template>
  <section class="diagnostic-view" :aria-label="$t('DIAGNOSTIC')" :aria-busy="isBusy">
    <Tabs :model-value="kind" @update:model-value="switchKind">
      <header>
        <TabsList :aria-label="$t('Diagnostic files')">
          <TabsTrigger value="log" :disabled="isBusy || !!pendingFile">{{ $t('LOG') }}</TabsTrigger>
          <TabsTrigger value="bug" :disabled="isBusy || !!pendingFile">{{ $t('BUG') }}</TabsTrigger>
          <TabsTrigger value="snapshot" :disabled="isBusy || !!pendingFile">{{ $t('SNAPSHOT') }}</TabsTrigger>
        </TabsList>
        <Button variant="outline" size="icon" :disabled="isBusy" :aria-label="$t('REFRESH')" :title="$t('REFRESH')" @click="read(false)"><RefreshCw :size="16" aria-hidden="true" /></Button>
      </header>
      <TabsContent :value="kind" class="diagnostic-files">
        <p v-if="error && !pendingFile" role="alert">{{ $t(error) }}</p>
        <p v-if="isBusy" role="status">{{ $t('LOADING...') }}</p>
        <div v-if="savedPath" class="saved-file" role="status"><p>{{ $t('Saved to {0}', [savedPath]) }}</p><Button variant="outline" :disabled="isBusy" @click="openFolder">{{ $t(folderLabels[kind]) }}</Button></div>
        <div v-if="files.length" class="table-scroll"><table>
          <thead><tr><th>{{ $t('NAME') }}</th><th>{{ $t('SIZE (BYTES)') }}</th><th>{{ $t('MODIFIED (LOCAL)') }}</th><th>{{ $t('ACTION') }}</th></tr></thead>
          <tbody><tr v-for="file in files" :key="file.name">
            <td>{{ file.name }}</td><td>{{ size(file.size) }}</td><td>{{ formatControlTime(file.modified_at) }}</td>
            <td><div class="file-actions">
              <Button v-if="operations.includes('admin.diagnostics.download')" variant="outline" size="icon" :disabled="isBusy" :aria-label="$t('Download {0}', [file.name])" :title="$t('Download {0}', [file.name])" @click="download(file)"><Download :size="16" aria-hidden="true" /></Button>
              <Button v-if="operations.includes('admin.diagnostics.delete')" variant="destructive" size="icon" :disabled="isBusy" :aria-label="$t('Delete {0}', [file.name])" :title="$t('Delete {0}', [file.name])" @click="confirmDelete(file)"><Trash2 :size="16" aria-hidden="true" /></Button>
            </div></td>
          </tr></tbody>
        </table></div>
        <p v-else-if="isLoaded && !isBusy">{{ $t('No files.') }}</p>
        <Button v-if="next" variant="outline" :disabled="isBusy" @click="read(true)">{{ $t('LOAD MORE') }}</Button>
      </TabsContent>
    </Tabs>
    <LauncherDialog v-if="pendingFile" :is-dismissible="!isBusy" @close="cancelDelete">
      <form class="notice-card diagnostic-confirmation" @submit.prevent="deleteFile">
        <DialogTitle>{{ $t('DELETE {0}?', [pendingFile.name]) }}</DialogTitle>
        <p>{{ $t('This permanently deletes the file from the server.') }}</p>
        <p v-if="error" role="alert">{{ $t(error) }}</p>
        <div class="confirmation-actions"><Button type="button" variant="outline" :disabled="isBusy" @click="cancelDelete">{{ $t('CANCEL') }}</Button><Button type="submit" variant="destructive" :disabled="isBusy">{{ $t(isBusy ? 'WORKING...' : 'DELETE') }}</Button></div>
      </form>
    </LauncherDialog>
  </section>
</template>

<style scoped>
.diagnostic-view, .diagnostic-files, .diagnostic-confirmation { display:flex; flex-direction:column; gap:14px; }
header, .confirmation-actions { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.table-scroll { overflow:auto; }
table { width:100%; border-collapse:collapse; text-align:left; font-size:12px; }
th, td { padding:10px; border-bottom:1px solid var(--border); vertical-align:top; overflow-wrap:anywhere; }
.file-actions { display:flex; gap:8px; }
p { font-size:12px; color:var(--muted-foreground); }
.saved-file { display:flex; flex-wrap:wrap; align-items:center; gap:12px; overflow-wrap:anywhere; }
</style>
