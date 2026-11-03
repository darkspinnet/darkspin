<script setup>
import { RefreshCw } from '@lucide/vue'
import { formatControlTime } from '../control_time'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { DialogTitle } from '@/components/ui/dialog'
import LauncherDialog from './LauncherDialog.vue'
import { ListRemoteControlSessions, RevokeRemoteControlSession, RevokeOtherRemoteControlSessions } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, panel:Object, isBlocked:Boolean })
const emit = defineEmits(['busy', 'invalidated'])
const sessions = ref([])
const isBusy = ref(false)
const isLoaded = ref(false)
const confirmation = ref('')
const selectedID = ref('')
const isSelectedCurrent = ref(false)
const currentPassword = ref('')
const error = ref('')
const notice = ref('')
const operations = computed(() => props.panel.server.supported_operations || [])
const isRevokeSupported = computed(() => operations.value.includes('account.sessions.revoke'))
const hasOtherSessions = computed(() => sessions.value.some(session => !session.is_current))
let generation = 0
let isDisposed = false
watch([isBusy, confirmation], () => emit('busy', isBusy.value || !!confirmation.value), { flush:'sync' })

function isCurrent(current, profile) {
  return !isDisposed && current === generation && props.profile?.serverAddress === profile.serverAddress && props.profile?.loginName === profile.loginName
}
function failed(failure) {
  error.value = String(failure).includes('controlPanel:outcome_unknown')
    ? 'The session request outcome is unknown. Refresh active launcher sessions before trying again.'
    : panelError(failure)
  if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
}
async function reload() {
  if (isBusy.value || props.isBlocked || confirmation.value || !operations.value.includes('account.sessions.read')) return
  const current = generation
  const profile = { ...props.profile }
  isBusy.value = true
  error.value = ''
  isLoaded.value = false
  sessions.value = []
  try {
    const result = await ListRemoteControlSessions(profile.serverAddress, profile.loginName)
    if (!isCurrent(current, profile)) return
    sessions.value = result.sessions || []
    isLoaded.value = true
  } catch (failure) { if (isCurrent(current, profile)) failed(failure) }
  finally { if (isCurrent(current, profile)) isBusy.value = false }
}
function confirm(action, session = null) {
  if (isBusy.value || props.isBlocked || !isLoaded.value || !isRevokeSupported.value) return
  confirmation.value = action
  selectedID.value = session?.id || ''
  isSelectedCurrent.value = !!session?.is_current
  currentPassword.value = ''
  error.value = ''
  notice.value = ''
}
function cancel() {
  if (isBusy.value) return
  confirmation.value = ''
  selectedID.value = ''
  currentPassword.value = ''
  error.value = ''
}
async function submit() {
  const action = confirmation.value
  if (isBusy.value || props.isBlocked || !currentPassword.value || !['single', 'others'].includes(action)) return
  const sessionID = selectedID.value
  if (action === 'single' && !sessionID) return
  const current = generation
  const profile = { ...props.profile }
  const password = currentPassword.value
  currentPassword.value = ''
  isBusy.value = true
  error.value = ''
  try {
    const result = action === 'single'
      ? await RevokeRemoteControlSession(profile.serverAddress, profile.loginName, password, sessionID)
      : await RevokeOtherRemoteControlSessions(profile.serverAddress, profile.loginName, password)
    if (!isCurrent(current, profile)) return
    confirmation.value = ''
    selectedID.value = ''
    if (result.is_current) {
      emit('invalidated', 'This launcher session was revoked. Sign in again to open the control panel.')
      return
    }
    notice.value = 'The server revoked the selected launcher sessions. Refresh to see active sessions.'
    sessions.value = []
    isLoaded.value = false
  } catch (failure) {
    if (!isCurrent(current, profile)) return
    // Do not replay a mutation, including one whose response was lost.
    confirmation.value = ''
    selectedID.value = ''
    sessions.value = []
    isLoaded.value = false
    failed(failure)
  } finally { if (isCurrent(current, profile)) isBusy.value = false }
}
function displayDate(timestamp, isDaily = false) { return formatControlTime(timestamp, isDaily) }
watch(() => [props.profile?.serverAddress, props.profile?.loginName], () => {
  generation++
  sessions.value = []
  isLoaded.value = false
  confirmation.value = ''
  selectedID.value = ''
  currentPassword.value = ''
  error.value = ''
  notice.value = ''
  isBusy.value = false
  void reload()
}, { immediate:true, flush:'sync' })
onBeforeUnmount(() => { isDisposed = true; currentPassword.value = ''; sessions.value = []; emit('busy', false) })
</script>

<template>
  <section class="session-view">
    <div class="session-heading"><h3>{{ $t('ACTIVE LAUNCHER SESSIONS') }}</h3><Button variant="outline" :disabled="isBusy || isBlocked || !!confirmation" @click="reload" size="icon" :aria-label="$t('REFRESH')" :title="$t('REFRESH')"><RefreshCw :size="16" aria-hidden="true" /></Button></div>
    <p>{{ $t('These sessions provide control panel access and expire when the server restarts. Revoking them does not disconnect games already admitted to the server.') }}</p>
    <p v-if="error" role="alert">{{ $t(error) }}</p>
    <p v-if="notice" role="status">{{ $t(notice) }}</p>
    <p v-if="isBusy" role="status">{{ $t('WORKING...') }}</p>
    <template v-if="isLoaded">
      <p v-if="!sessions.length">{{ $t('No active launcher sessions were returned.') }}</p>
      <article v-for="session in sessions" :key="session.id" class="session-card">
        <h4>{{ $t(session.is_current ? 'THIS LAUNCHER SESSION' : 'OTHER LAUNCHER SESSION') }}</h4>
        <dl>
          <dt>{{ $t('SESSION ID') }}</dt><dd>{{ session.id }}</dd>
          <dt>{{ $t('CREATED AT') }}</dt><dd>{{ $t(displayDate(session.created_at)) }}</dd>
          <dt>{{ $t('LAST USED AT') }}</dt><dd>{{ $t(displayDate(session.last_used_at)) }}</dd>
          <dt>{{ $t('EXPIRES AT') }}</dt><dd>{{ $t(displayDate(session.expires_at)) }}</dd>
        </dl>
        <Button v-if="isRevokeSupported" variant="outline" :disabled="isBusy || isBlocked || !!confirmation" @click="confirm('single', session)">{{ $t('REVOKE SESSION') }}</Button>
      </article>
      <Button v-if="isRevokeSupported" variant="outline" :disabled="isBusy || isBlocked || !!confirmation || !hasOtherSessions" @click="confirm('others')">{{ $t('REVOKE ALL OTHER SESSIONS') }}</Button>
    </template>
    <p v-else-if="!isBusy">{{ $t('Refresh to read active launcher sessions before revoking access.') }}</p>
  </section>
  <LauncherDialog v-if="confirmation" :is-dismissible="!isBusy" @close="cancel">
    <form class="notice-card session-confirmation" @submit.prevent="submit">
      <DialogTitle>{{ $t(confirmation === 'others' ? 'REVOKE ALL OTHER SESSIONS?' : 'REVOKE LAUNCHER SESSION?') }}</DialogTitle>
      <p>{{ panel.profile.display_name }} · {{ profile.serverAddress }}</p>
      <p v-if="confirmation === 'single'">{{ $t('Revoke session {0}?', [selectedID]) }}</p>
      <p v-if="confirmation === 'single' && isSelectedCurrent">{{ $t('Revoking this session ends your control panel access. You will need to sign in again.') }}</p>
      <p v-if="confirmation === 'others'">{{ $t('Keep this launcher session and revoke every other active control panel session for this account.') }}</p>
      <p>{{ $t('Enter your current password to revoke launcher access.') }}</p>
      <Input v-model="currentPassword" type="password" autocomplete="current-password" :aria-label="$t('CURRENT PASSWORD')" :disabled="isBusy" />
      <div class="session-confirmation-actions"><Button type="button" variant="outline" :disabled="isBusy" @click="cancel">{{ $t('CANCEL') }}</Button><Button type="submit" variant="destructive" :disabled="isBusy || !currentPassword">{{ $t(isBusy ? 'WORKING...' : 'CONFIRM') }}</Button></div>
    </form>
  </LauncherDialog>
</template>

<style scoped>
.session-view, .session-card, .session-confirmation { display:flex; flex-direction:column; gap:12px; }
.session-heading, .session-confirmation-actions { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.session-card { padding:16px; border:1px solid var(--border); border-radius:8px; }
.session-card dl { display:grid; grid-template-columns:auto minmax(0, 1fr); gap:6px 16px; }
.session-card dd { overflow-wrap:anywhere; }
.session-view p, .session-card dt { color:var(--muted-foreground); }
.session-view > button, .session-card button { align-self:flex-start; }
</style>
