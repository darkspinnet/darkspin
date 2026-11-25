<script setup>
import { formatControlTime } from '../control_time'
import { onBeforeUnmount, reactive, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { FilterRemoteControlHistory } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object })
const emit = defineEmits(['busy', 'invalidated'])
const filters = reactive(emptyFilters())
const events = ref([])
const nextEventID = ref(0)
const isBusy = ref(false)
const error = ref('')
let appliedFilter = Object.freeze({})
let generation = 0
let isDisposed = false

function emptyFilters() {
  return { targetUserID:'', actorUserID:'', actorType:'', action:'', outcome:'', operationID:'', afterLocal:'', beforeLocal:'' }
}
watch(isBusy, next => emit('busy', next), { flush:'sync' })
function isCurrent(current, profile) {
  return !isDisposed && current === generation && profile.serverAddress === props.profile?.serverAddress && profile.loginName === props.profile?.loginName
}
function accountID(text) {
  if (text === '') return 0
  const id = Number(text)
  return /^\d+$/.test(String(text)) && Number.isSafeInteger(id) && id > 0 ? id : null
}
function localTimestamp(text) {
  if (!text) return 0
  const timestamp = Math.floor(new Date(text).getTime() / 1000)
  return Number.isSafeInteger(timestamp) && timestamp > 0 ? timestamp : null
}
function applyFilters() {
  if (isBusy.value) return
  const targetUserID = accountID(filters.targetUserID)
  const actorUserID = accountID(filters.actorUserID)
  const afterUnix = localTimestamp(filters.afterLocal)
  const beforeUnix = localTimestamp(filters.beforeLocal)
  if (targetUserID === null || actorUserID === null) {
    error.value = 'Enter a positive whole account ID or leave the field blank.'
    return
  }
  if (afterUnix === null || beforeUnix === null || (beforeUnix > 0 && beforeUnix <= afterUnix)) {
    error.value = 'Enter valid local times with the end after the start, or leave the fields blank.'
    return
  }
  appliedFilter = Object.freeze({ target_user_id:targetUserID, actor_user_id:actorUserID, actor_type:filters.actorType.trim(), action:filters.action.trim(), outcome:filters.outcome.trim(), operation_id:filters.operationID.trim(), after_unix:afterUnix, before_unix:beforeUnix })
  generation++
  events.value = []
  nextEventID.value = 0
  void loadHistory()
}
function clearFilters() {
  if (isBusy.value) return
  Object.assign(filters, emptyFilters())
  applyFilters()
}
function filterOperation(operationID) {
  if (isBusy.value) return
  Object.assign(filters, emptyFilters(), { operationID })
  applyFilters()
}
async function loadHistory(isMore = false) {
  if (isBusy.value || (isMore && !nextEventID.value)) return
  const current = generation
  const profile = { serverAddress:props.profile.serverAddress, loginName:props.profile.loginName }
  const req = { ...appliedFilter, before_event_id:isMore ? nextEventID.value : 0 }
  isBusy.value = true
  error.value = ''
  try {
    const result = await FilterRemoteControlHistory(profile.serverAddress, profile.loginName, req)
    if (!isCurrent(current, profile)) return
    events.value = isMore ? [...events.value, ...(result.events || [])] : result.events || []
    nextEventID.value = result.next_event_id || 0
  } catch (failure) {
    if (!isCurrent(current, profile)) return
    error.value = panelError(failure)
    if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) {
      events.value = []
      nextEventID.value = 0
      emit('invalidated', error.value)
    }
  } finally { if (isCurrent(current, profile)) isBusy.value = false }
}
function eventDate(timestamp) { return formatControlTime(timestamp > 0 ? timestamp * 1000 : null) }
function actorLabel(actorType) {
  if (actorType === 'local_operator') return 'LOCAL OPERATOR'
  if (actorType === 'account') return 'ACCOUNT'
  if (actorType === 'system') return 'SYSTEM'
  return actorType
}
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  isBusy.value = false
  Object.assign(filters, emptyFilters())
  applyFilters()
}, { immediate:true, flush:'sync' })
onBeforeUnmount(() => { isDisposed = true; generation++; emit('busy', false); events.value = [] })
</script>

<template>
  <section class="history-view">
    <form @submit.prevent="applyFilters">
      <div class="history-filters">
        <label>{{ $t('TARGET ACCOUNT ID') }}<Input v-model="filters.targetUserID" inputmode="numeric" :disabled="isBusy" autocomplete="off" /></label>
        <label>{{ $t('ACTOR ACCOUNT ID') }}<Input v-model="filters.actorUserID" inputmode="numeric" :disabled="isBusy" autocomplete="off" /></label>
        <label>{{ $t('ACTOR TYPE') }}<Input v-model="filters.actorType" list="control-history-actor-types" :maxlength="32" :disabled="isBusy" autocomplete="off" /></label>
        <label>{{ $t('ACTION') }}<Input v-model="filters.action" :maxlength="80" :disabled="isBusy" autocomplete="off" /></label>
        <label>{{ $t('OUTCOME') }}<Input v-model="filters.outcome" list="control-history-outcomes" :maxlength="32" :disabled="isBusy" autocomplete="off" /></label>
        <label>{{ $t('OPERATION ID') }}<Input v-model="filters.operationID" :maxlength="128" :disabled="isBusy" autocomplete="off" /></label>
        <label>{{ $t('START (LOCAL TIME)') }}<Input v-model="filters.afterLocal" type="datetime-local" :disabled="isBusy" /></label>
        <label>{{ $t('END (LOCAL TIME)') }}<Input v-model="filters.beforeLocal" type="datetime-local" :disabled="isBusy" /></label>
      </div>
      <datalist id="control-history-actor-types"><option value="local_operator">{{ $t('LOCAL OPERATOR') }}</option><option value="account">{{ $t('ACCOUNT') }}</option><option value="system">{{ $t('SYSTEM') }}</option></datalist>
      <datalist id="control-history-outcomes"><option value="accepted" /><option value="completed" /><option value="failed" /></datalist>
      <p>{{ $t('Blank fields include all events. Text filters match exact codes. Times use this device’s local timezone. The start is inclusive and the end is exclusive.') }}</p>
      <p>{{ $t('Outcome codes include accepted, completed, and failed.') }}</p>
      <p>{{ $t('Load more uses the last applied filters. Apply filters to update the results.') }}</p>
      <p>{{ $t('Accepted records show intent, not completion. Without a terminal outcome record, the outcome is unknown.') }}</p>
      <div class="history-actions"><Button type="submit" variant="outline" :disabled="isBusy">{{ $t('APPLY FILTERS') }}</Button><Button type="button" variant="outline" :disabled="isBusy" @click="clearFilters">{{ $t('CLEAR FILTERS') }}</Button></div>
    </form>
    <p v-if="error" role="alert">{{ $t(error) }}</p>
    <div class="history-table-scroll"><table>
      <thead><tr><th>{{ $t('TIME (LOCAL)') }}</th><th>{{ $t('ACTOR') }}</th><th>{{ $t('TARGET') }}</th><th>{{ $t('ACTION') }}</th><th>{{ $t('REASON') }}</th><th>{{ $t('OUTCOME') }}</th></tr></thead>
      <tbody><tr v-for="event in events" :key="event.id"><td>{{ $t(eventDate(event.occurred_at_unix)) }}</td><td>{{ $t(actorLabel(event.actor_type)) }} · {{ event.actor_user_id }}</td><td>{{ event.target_user_id }}</td><td>{{ event.action }}<button v-if="event.operation_id" type="button" class="history-operation" :disabled="isBusy" :title="$t('Show all events for this operation')" @click="filterOperation(event.operation_id)">{{ event.operation_id }}</button></td><td>{{ event.reason }}</td><td>{{ event.outcome }}</td></tr></tbody>
    </table></div>
    <p v-if="!events.length && !isBusy">{{ $t('No admin activity found.') }}</p>
    <p v-if="isBusy" role="status">{{ $t('LOADING...') }}</p>
    <Button v-if="nextEventID" variant="outline" :disabled="isBusy" @click="loadHistory(true)">{{ $t('LOAD MORE') }}</Button>
  </section>
</template>

<style scoped>
.history-view, .history-view form { display:flex; flex-direction:column; gap:14px; }
.history-filters { display:grid; grid-template-columns:repeat(auto-fit,minmax(200px,1fr)); gap:12px; }
.history-filters label { display:flex; flex-direction:column; gap:6px; font-size:11px; font-weight:600; }
.history-actions { display:flex; flex-wrap:wrap; gap:10px; }
.history-view p { font-size:12px; color:var(--muted-foreground); }
.history-table-scroll { overflow:auto; }
table { width:100%; border-collapse:collapse; text-align:left; font-size:12px; }
th, td { padding:10px; border-bottom:1px solid var(--border); vertical-align:top; overflow-wrap:anywhere; }
.history-operation { display:block; color:var(--muted-foreground); text-align:left; text-decoration:underline; cursor:pointer; overflow-wrap:anywhere; }
.history-operation:focus-visible { outline:2px solid var(--ring); }
.history-operation:disabled { cursor:default; opacity:.5; }
</style>
