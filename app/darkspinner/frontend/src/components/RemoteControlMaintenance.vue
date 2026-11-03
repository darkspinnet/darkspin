<script setup>
import { formatControlTime } from '../control_time'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { DialogTitle } from '@/components/ui/dialog'
import LauncherDialog from './LauncherDialog.vue'
import ConfigHelp from './ConfigHelp.vue'
import { GetRemoteControlMaintenance, StartRemoteControlMaintenance, CancelRemoteControlMaintenance } from '../platform'
import { panelError, maintenancePhaseLabel, maintenanceActionLabel } from '../control_panel'

const props = defineProps({ profile:Object, panel:Object })
const emit = defineEmits(['busy', 'invalidated', 'maintenance', 'observation'])
const state = ref(null)
const isBusy = ref(false)
const error = ref('')
const reason = ref('')
const warningMinutes = ref(30)
const confirmation = ref('')
const confirmationID = ref('')
const observedTime = ref(Date.now())
let generation = 0
let timer = null
let deadlineTimer = null
let isDisposed = false
const operations = computed(() => props.panel.server.supported_operations || [])
const isReasonValid = computed(() => !!reason.value.trim() && new TextEncoder().encode(reason.value).length <= 512 && !/\p{Cc}/u.test(reason.value))
const isTimingValid = computed(() => Number.isInteger(warningMinutes.value) && warningMinutes.value >= 0 && warningMinutes.value <= 1440)
const isCancellable = computed(() => {
  if (!state.value?.is_active || !['preparing', 'awaiting_activation', 'warning', 'draining', 'held', 'failed'].includes(state.value.phase)) return false
  if (state.value.action !== 'hold' || !['warning', 'draining'].includes(state.value.phase)) return true
  const deadline = Date.parse(state.value.deadline)
  return !Number.isFinite(deadline) || deadline > observedTime.value
})
watch(isBusy, next => emit('busy', next), { flush:'sync' })
watch(state, next => emit('observation', next), { flush:'sync' })
function isCurrent(current, profile) {
  return !isDisposed && current === generation && props.profile?.serverAddress === profile?.serverAddress && props.profile?.loginName === profile?.loginName
}
function apply(result) {
  observedTime.value = Date.now()
  window.clearTimeout(deadlineTimer)
  const remaining = Date.parse(result.deadline) - observedTime.value
  if (result.action === 'hold' && remaining > 0 && ['warning', 'draining'].includes(result.phase)) deadlineTimer = window.setTimeout(updateObservedTime, remaining)
  state.value = result
  emit('maintenance', result)
}
function failed(failure) {
  error.value = panelError(failure)
  if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) {
    state.value = null
    confirmation.value = ''
    emit('invalidated', 'Sign in again after the server is available to read the maintenance outcome.')
  }
}
async function reload() {
  if (isBusy.value || document.visibilityState === 'hidden') return
  const current = generation
  const profile = props.profile
  isBusy.value = true
  error.value = ''
  try {
    const result = await GetRemoteControlMaintenance(profile.serverAddress, profile.loginName)
    if (isCurrent(current, profile)) apply(result)
  } catch (failure) {
    if (!isCurrent(current, profile)) return
    state.value = null
    confirmation.value = ''
    failed(failure)
  } finally { if (isCurrent(current, profile)) isBusy.value = false }
}
function confirm(action) {
  if (isBusy.value) return
  confirmation.value = action
  confirmationID.value = state.value?.id || ''
  error.value = ''
}
function cancelConfirmation() {
  if (!isBusy.value) confirmation.value = ''
}
async function submit() {
  if (isBusy.value || !state.value) return
  const action = confirmation.value
  const current = generation
  const profile = props.profile
  const operationID = confirmationID.value
  if (action !== 'cancel' && (!isReasonValid.value || !isTimingValid.value || state.value.is_active)) return
  isBusy.value = true
  error.value = ''
  try {
    const result = action === 'cancel'
      ? await CancelRemoteControlMaintenance(profile.serverAddress, profile.loginName, operationID)
      : await StartRemoteControlMaintenance(profile.serverAddress, profile.loginName, { action, warning_minutes:warningMinutes.value, reason:reason.value })
    if (!isCurrent(current, profile)) return
    confirmation.value = ''
    apply(result)
  } catch (failure) {
    if (!isCurrent(current, profile)) return
    confirmation.value = ''
    failed(failure)
    // Never retry a mutation after an uncertain outcome; read current state.
    isBusy.value = false
    await reload()
    if (isCurrent(current, profile)) error.value = panelError(failure)
  } finally { if (isCurrent(current, profile)) isBusy.value = false }
}
function displayDate(timestamp, isDaily = false) { return formatControlTime(timestamp, isDaily) }
function failureLabel(code) {
  return { update_failed:'The update failed.', startup_failed:'The server could not start.', rollback_failed:'Rollback failed.' }[code] || 'The operation reported a failure.'
}
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  window.clearTimeout(deadlineTimer)
  state.value = null
  reason.value = ''
  error.value = ''
  confirmation.value = ''
  warningMinutes.value = 30
  isBusy.value = false
  void reload()
}, { immediate:true, flush:'sync' })
function updateObservedTime() { observedTime.value = Date.now() }
function poll() { updateObservedTime(); void reload() }
timer = window.setInterval(poll, 5000)
onBeforeUnmount(() => { isDisposed = true; window.clearInterval(timer); window.clearTimeout(deadlineTimer); emit('busy', false); state.value = null })
defineExpose({ reload })
</script>

<template>
  <section class="maintenance-view">
    <h3>{{ $t('SERVER OPERATIONS') }}</h3>
    <p v-if="error" role="alert">{{ $t(error) }}</p>
    <template v-if="state">
      <div v-if="operations.includes('admin.maintenance.write')" class="maintenance-controls">
        <div class="maintenance-fields">
          <label for="maintenance-reason"><span>{{ $t('REASON') }}</span><Input id="maintenance-reason" v-model="reason" maxlength="512" :disabled="isBusy || state.is_active" /></label>
          <label for="maintenance-warning"><span>{{ $t('MAXIMUM DRAIN TIME (MINUTES)') }}</span><Input id="maintenance-warning" v-model.number="warningMinutes" type="number" min="0" max="1440" step="1" :disabled="isBusy || state.is_active" /></label>
        </div>
        <div class="maintenance-actions">
          <span class="maintenance-action"><Button variant="outline" :disabled="isBusy || state.is_active || !isReasonValid || !isTimingValid" @click="confirm('hold')">{{ $t('HOLD IN MAINTENANCE') }}</Button><ConfigHelp text="Blocks new gameplay and lets matches finish until the deadline, then disconnects remaining players. The server stays running and closed until you cancel maintenance." /></span>
          <span class="maintenance-action"><Button variant="outline" :disabled="isBusy || state.is_active || !isReasonValid || !isTimingValid || !state.is_restart_supported || !operations.includes('admin.maintenance.restart')" @click="confirm('restart')">{{ $t('RESTART') }}</Button><ConfigHelp text="Drains matches and restarts the currently installed version." /></span>
          <span class="maintenance-action"><Button variant="outline" :disabled="isBusy || state.is_active || !isReasonValid || !isTimingValid || !state.is_update_supported || !operations.includes('admin.maintenance.update')" @click="confirm('update')">{{ $t('UPDATE WITH BACKUP') }}</Button><ConfigHelp text="Drains matches, stops the server, backs it up, checks and installs an update, then starts it again." /></span>
          <Button variant="outline" :disabled="isBusy || !isCancellable" @click="confirm('cancel')">{{ $t('CANCEL MAINTENANCE') }}</Button>
        </div>
        <p v-if="!state.is_restart_supported || !state.is_update_supported">{{ $t('Restart and update require support from this deployment. Unsupported actions remain disabled.') }}</p>
      </div>
      <details class="manage-detail">
        <summary>{{ $t('OPERATION DETAILS') }}</summary>
        <p>{{ $t('New game sessions and matches are blocked immediately. Current matches may finish until the deadline; lobbies do not delay the operation.') }}</p>
      <dl>
        <dt>{{ $t('OPERATION') }}</dt><dd>{{ state.id || $t('NONE') }}</dd>
        <dt>{{ $t('ACTION') }}</dt><dd>{{ $t(state.id ? maintenanceActionLabel(state.action) : 'NONE') }}</dd>
        <dt>{{ $t('PHASE') }}</dt><dd>{{ $t(state.id ? maintenancePhaseLabel(state.phase) : 'NONE') }}</dd>
        <dt>{{ $t('LATEST DEADLINE') }}</dt><dd>{{ $t(displayDate(state.deadline)) }}</dd>
        <dt>{{ $t('REASON') }}</dt><dd>{{ state.reason || $t('NONE') }}</dd>
        <dt>{{ $t('NEW GAME ADMISSION') }}</dt><dd>{{ $t(state.is_admission_blocked ? 'BLOCKED' : 'ALLOWED') }}</dd>
        <dt>{{ $t('WARNING DELIVERIES') }}</dt><dd>{{ state.delivered }}</dd>
      </dl>
      <section v-if="state.result" class="maintenance-result">
        <h4>{{ $t('OBSERVED OPERATION RESULT') }}</h4>
        <dl>
          <dt>{{ $t('OPERATION') }}</dt><dd>{{ state.result.operation_id }}</dd>
          <dt>{{ $t('PHASE') }}</dt><dd>{{ $t(maintenancePhaseLabel(state.result.phase)) }}</dd>
          <dt>{{ $t('OBSERVED AT') }}</dt><dd>{{ $t(displayDate(state.result.updated_at)) }}</dd>
          <dt>{{ $t('INSTALLED VERSION') }}</dt><dd>{{ state.result.installed_version || $t('UNKNOWN') }}</dd>
          <dt>{{ $t('CANDIDATE VERSION') }}</dt><dd>{{ state.result.candidate_version || $t('UNKNOWN') }}</dd>
        </dl>
        <p v-if="state.result.is_success === true">{{ $t('The operation completed successfully.') }}</p>
        <template v-else-if="state.result.is_success === false">
          <p v-if="state.result.phase === 'healthy'">{{ $t('The requested operation failed, but the installed server recovered.') }}</p>
          <p v-else-if="state.result.phase === 'rolled_back'">{{ $t('The requested operation failed. The server reports a rollback.') }}</p>
          <p>{{ $t(failureLabel(state.result.failure_code)) }}</p>
        </template>
        <p v-else>{{ $t('No final operation outcome has been observed.') }}</p>
      </section>
      <p v-else-if="state.id && ['restart', 'update'].includes(state.action)">{{ $t('No final restart or update outcome has been observed.') }}</p>
      <p v-if="state.requested_operation_id && state.requested_operation_id !== state.id">{{ $t('Previously requested operation: {0}. The server has not reported its outcome here.', [state.requested_operation_id]) }}</p>
      <p>{{ $t('Repeating a request keeps the active operation and its original deadline. Choose a different action only after cancelling the current operation.') }}</p>
      </details>
    </template>
    <p v-else-if="!isBusy">{{ $t('Maintenance state is unavailable. Refresh or sign in again after the server is available; no previous outcome is assumed.') }}</p>
  </section>
  <LauncherDialog v-if="confirmation" :is-dismissible="!isBusy" @close="cancelConfirmation">
    <form class="notice-card maintenance-confirmation" @submit.prevent="submit">
      <DialogTitle>{{ $t(confirmation === 'cancel' ? 'CANCEL MAINTENANCE?' : 'START MAINTENANCE?') }}</DialogTitle>
      <p>{{ profile.serverAddress }}</p>
      <p v-if="confirmation === 'cancel'">{{ $t('Cancel only operation {0} and reopen admission if it has not started stopping.', [confirmationID]) }}</p>
      <template v-else>
        <p>{{ $t(maintenanceActionLabel(confirmation)) }} · {{ reason }}</p>
        <p>{{ $t('Maximum time for current matches to finish: {0} minutes. The operation can proceed immediately when no matches remain.', [warningMinutes]) }}</p>
        <p v-if="confirmation === 'hold'">{{ $t('The server will remain closed to new gameplay until this maintenance operation is cancelled. Remaining players may be disconnected at the deadline.') }}</p>
        <p v-else>{{ $t('When current matches finish, or by this deadline, the server will restart or update and disconnect remaining players.') }}</p>
        <p>{{ $t('After a restart or update, sign in again to read the operation outcome.') }}</p>
      </template>
      <div class="maintenance-confirmation-actions"><Button type="button" variant="outline" :disabled="isBusy" @click="cancelConfirmation">{{ $t('CANCEL') }}</Button><Button type="submit" :disabled="isBusy">{{ $t(isBusy ? 'WORKING...' : 'CONFIRM') }}</Button></div>
    </form>
  </LauncherDialog>
</template>

<style scoped>
.maintenance-view, .maintenance-controls, .maintenance-confirmation { display:flex; flex-direction:column; gap:14px; }
.maintenance-confirmation-actions { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.maintenance-view dl { display:grid; grid-template-columns:minmax(140px,1fr) 2fr; gap:10px; }
.maintenance-view dd { overflow-wrap:anywhere; }
.maintenance-view p { color:var(--muted-foreground); font-size:12px; }
.maintenance-fields { display:grid; grid-template-columns:repeat(auto-fit,minmax(min(100%,220px),1fr)); gap:14px; max-width:760px; }
.maintenance-fields label { display:flex; flex-direction:column; gap:6px; }
.maintenance-actions { display:flex; flex-wrap:wrap; gap:10px; }
.maintenance-action { display:inline-flex; align-items:center; }
.manage-detail > summary { cursor:pointer; font-size:12px; font-weight:600; }
.manage-detail[open] > summary { margin-bottom:14px; }
</style>
