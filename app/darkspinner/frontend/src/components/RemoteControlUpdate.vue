<script setup>
import { formatControlTime } from '../control_time'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { GetRemoteControlUpdate, CheckRemoteControlUpdate } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, panel:Object })
const emit = defineEmits(['busy', 'invalidated', 'observation'])
const state = ref(null)
const isBusy = ref(false)
const error = ref('')
let generation = 0
let isDisposed = false
const operations = computed(() => props.panel.server.supported_operations || [])
const channel = computed(() => ({
  configured:'CONFIGURED SOURCE',
}[state.value?.channel] || 'UNKNOWN'))
watch(isBusy, next => emit('busy', next), { flush:'sync' })
watch(state, next => emit('observation', next), { flush:'sync' })
function isCurrent(current, profile) {
  return !isDisposed && current === generation && profile?.serverAddress === props.profile?.serverAddress && profile?.loginName === props.profile?.loginName
}
function failed(failure) {
  state.value = null
  error.value = panelError(failure)
  if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
}
async function request(isCheck = false) {
  if (isBusy.value || !operations.value.includes('admin.update.read') || !props.profile) return
  if (isCheck && (!operations.value.includes('admin.update.check') || !state.value?.is_check_supported || state.value?.is_check_in_progress)) return
  const current = generation
  const profile = props.profile
  isBusy.value = true
  error.value = ''
  try {
    const result = isCheck
      ? await CheckRemoteControlUpdate(profile.serverAddress, profile.loginName)
      : await GetRemoteControlUpdate(profile.serverAddress, profile.loginName)
    if (isCurrent(current, profile)) state.value = result
  } catch (failure) { if (isCurrent(current, profile)) failed(failure) }
  finally { if (isCurrent(current, profile)) isBusy.value = false }
}
function failureLabel(code) {
  return {
    check_failed:'The update check failed. No current update status is known.',
    check_timeout:'The update check timed out. No current update status is known.',
    check_cancelled:'The update check was cancelled. No current update status is known.',
  }[code] || 'The update check failed. No current update status is known.'
}
function displayDate(timestamp, isDaily = false) { return formatControlTime(timestamp, isDaily) }
function reload() { void request() }
function check() { void request(true) }
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  state.value = null
  error.value = ''
  isBusy.value = false
  reload()
}, { immediate:true, flush:'sync' })
onBeforeUnmount(() => { isDisposed = true; emit('busy', false); state.value = null })
defineExpose({ reload, check })
</script>

<template>
  <section class="update-view">
    <p v-if="error" role="alert">{{ $t(error) }}</p>
    <details v-if="state" class="manage-detail">
      <summary>{{ $t('UPDATE DETAILS') }}</summary>
      <dl>
        <dt>{{ $t('RUNNING SERVER BUILD') }}</dt><dd>{{ state.installed_build_id || $t('UNKNOWN') }}</dd>
        <dt>{{ $t('SUPERVISOR VERSION') }}</dt><dd>{{ state.bootstrap_version || $t('UNKNOWN') }}</dd>
        <dt>{{ $t('SUPERVISOR BUILD') }}</dt><dd>{{ state.bootstrap_build_id || $t('UNKNOWN') }}</dd>
        <dt>{{ $t('UPDATE CHANNEL') }}</dt><dd>{{ $t(channel) }}</dd>
        <dt>{{ $t('LAST UPDATE CHECK') }}</dt><dd>{{ $t(displayDate(state.checked_at)) }}</dd>
        <dt>{{ $t('CANDIDATE VERSION') }}</dt><dd>{{ state.candidate_version || $t('UNKNOWN') }}</dd>
        <dt>{{ $t('CANDIDATE BUILD') }}</dt><dd>{{ state.candidate_build_id || $t('UNKNOWN') }}</dd>
      </dl>
      <p v-if="state.check_status === 'failed'" role="alert">{{ $t(failureLabel(state.failure_code)) }}</p>
      <p v-if="state.is_check_in_progress">{{ $t('A check is in progress. Refresh to read its result.') }}</p>
      <p v-if="!state.is_check_supported">{{ $t('This deployment does not support update checks.') }}</p>
    </details>
    <p v-else-if="!isBusy">{{ $t('Update information is unavailable. Refresh or sign in again; no update status is assumed.') }}</p>
  </section>
</template>

<style scoped>
.update-view { display:flex; flex-direction:column; gap:14px; }
.manage-detail > summary { cursor:pointer; font-size:12px; font-weight:600; }
.manage-detail[open] > summary { margin-bottom:14px; }
.update-view dl { display:grid; grid-template-columns:minmax(140px,1fr) 2fr; gap:10px; }
.update-view dd { overflow-wrap:anywhere; }
.update-view p { color:var(--muted-foreground); font-size:12px; }
</style>
