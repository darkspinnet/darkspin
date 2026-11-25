<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { DialogTitle } from '@/components/ui/dialog'
import LauncherDialog from './LauncherDialog.vue'
import RemoteControlServer from './RemoteControlServer.vue'
import RemoteControlManage from './RemoteControlManage.vue'
import RemoteControlHistory from './RemoteControlHistory.vue'
import RemoteControlPersonalActivity from './RemoteControlPersonalActivity.vue'
import RemoteControlLoginHistory from './RemoteControlLoginHistory.vue'
import RemoteControlDiagnostic from './RemoteControlDiagnostic.vue'
import RemoteControlUserAccess from './RemoteControlUserAccess.vue'
import { ListRemoteControlUsers, ModerateRemoteControlUser, TimeoutRemoteControlUser } from '../platform'
import { panelError } from '../control_panel'
import { formatControlTime } from '../control_time'

const props = defineProps({ profile:Object, panel:Object })
const emit = defineEmits(['busy', 'invalidated', 'changed', 'maintenance'])
const tab = ref(initialTab())
const search = ref('')
const users = ref([])
const nextUserID = ref(0)
const isBusy = ref(false)
const isServerBusy = ref(false)
const isManageBusy = ref(false)
const isHistoryBusy = ref(false)
const isReviewBusy = ref(false)
const isDiagnosticBusy = ref(false)
const isAccessBusy = ref(false)
const accessUser = ref(null)
const isAnyBusy = computed(() => isBusy.value || isServerBusy.value || isManageBusy.value || isHistoryBusy.value || isReviewBusy.value || isDiagnosticBusy.value || isAccessBusy.value)
const reviewUser = ref(null)
const loginReviewUser = ref(null)
const error = ref('')
const notice = ref('')
const pendingUser = ref(null)
const pendingAction = ref('')
const reason = ref('')
const timeoutDuration = ref(15)
const timeoutUnit = ref('minute')
const timeoutMinutes = computed(() => Number(timeoutDuration.value) * ({ minute:1, hour:60, day:1440 }[timeoutUnit.value] || 0))
const isTimeoutDurationValid = computed(() => Number.isSafeInteger(timeoutMinutes.value) && timeoutMinutes.value >= 1 && timeoutMinutes.value <= 525600)
const observedTime = ref(Date.now())
function isUserTimedOut(user) { return Number(user.timeout_until_unix) * 1000 > observedTime.value }
function updateObservedTime() { observedTime.value = Date.now() }
const timeoutTimer = window.setInterval(updateObservedTime, 30000)
let isDisposed = false
let generation = 0
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  users.value = []
  reviewUser.value = null
  loginReviewUser.value = null
  pendingUser.value = null
  accessUser.value = null
  reason.value = ''
  error.value = ''
  notice.value = ''
  nextUserID.value = 0
  isBusy.value = false
}, { flush:'sync' })
function isCurrent(current, profile) {
  return !isDisposed && current === generation && profile.serverAddress === props.profile?.serverAddress && profile.loginName === props.profile?.loginName
}
const operations = computed(() => props.panel.server.supported_operations || [])
watch(isAnyBusy, next => emit('busy', next), { flush:'sync' })
function initialTab() {
  const operations = props.panel.server.supported_operations || []
  if (operations.some(operation => ['admin.maintenance.read', 'admin.update.read', 'admin.health.read'].includes(operation))) return 'manage'
  if (operations.some(operation => ['server.identity.read', 'admin.announcements.write', 'admin.matchmaking.read', 'admin.capacity.read'].includes(operation))) return 'server'
  if (props.panel.requested_maintenance_id && operations.includes('admin.maintenance.read')) return 'manage'
  if (operations.includes('admin.diagnostics.list')) return 'diagnostic'
  if (operations.includes('admin.users.list')) return 'users'
  if (operations.includes('admin.history.list')) return 'history'
  return 'manage'
}
function failed(failure) {
  if (isDisposed) return
  users.value = []
  error.value = panelError(failure)
  if (String(failure).includes('controlPanel:outcome_unknown')) {
    pendingUser.value = null
    reason.value = ''
    if (operations.value.includes('admin.history.list')) tab.value = 'history'
  }
  if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
}
async function loadUsers(isMore = false) {
  if (isAnyBusy.value) return
  const current = generation
  const profile = props.profile
  isBusy.value = true
  error.value = ''
  try {
    const result = await ListRemoteControlUsers(profile.serverAddress, profile.loginName, search.value, isMore ? nextUserID.value : 0)
    if (!isCurrent(current, profile)) return
    users.value = isMore ? [...users.value, ...(result.users || [])] : result.users || []
    nextUserID.value = result.next_user_id
  } catch (failure) { if (isCurrent(current, profile)) failed(failure) }
  finally { if (isCurrent(current, profile)) isBusy.value = false }
}
function switchTab(next) {
  if (isAnyBusy.value) return
  reviewUser.value = null
  loginReviewUser.value = null
  tab.value = next
  if (next === 'users') void loadUsers()
}
function review(user) {
  if (isAnyBusy.value) return
  loginReviewUser.value = null
  reviewUser.value = { id:user.id, displayName:user.display_name, loginName:user.login_name }
}
function reviewLoginHistory(user) {
  if (isAnyBusy.value) return
  reviewUser.value = null
  loginReviewUser.value = { id:user.id, displayName:user.display_name, loginName:user.login_name }
}
function editAccess(user) {
  if (isAnyBusy.value || user.login_name === props.profile.loginName) return
  accessUser.value = { ...user }
}
function accessSaved(result) {
  accessUser.value = null
  notice.value = result.is_disconnect_pending ? 'Access settings saved. Disconnecting the active game session is still pending.' : 'Access settings saved.'
  void loadUsers()
}
function confirm(user, action) {
  if (isAnyBusy.value) return
  pendingUser.value = user
  pendingAction.value = action
  reason.value = ''
  error.value = ''
  notice.value = ''
  timeoutDuration.value = 15
  timeoutUnit.value = 'minute'
}
function cancel() {
  if (isBusy.value) return
  pendingUser.value = null
  reason.value = ''
  error.value = ''
}
function moderationNotice(action, user, result) {
  if (action === 'timeout') return result.is_disconnect_pending ? 'The timeout was saved. Disconnecting the active game session is still pending.' : 'The Crogenitor is timed out.'
  if (action === 'unban' && !user.is_banned) return 'The timeout has ended.'
  if (action === 'kick') return result.is_disconnect_pending ? 'The server could not confirm disconnection. Refresh the user list before trying again.' : 'The game session was disconnected.'
  if (result.is_disconnect_pending) return 'The ban was saved. Disconnecting the active game session is still pending.'
  return result.is_banned ? 'The account is banned.' : 'The account is unbanned.'
}
async function moderate() {
  const user = pendingUser.value
  if (!user || isBusy.value) return
  if (pendingAction.value === 'timeout' && !isTimeoutDurationValid.value) return
  const current = generation
  const profile = props.profile
  isBusy.value = true
  error.value = ''
  try {
    const action = pendingAction.value
    const result = action === 'timeout'
      ? await TimeoutRemoteControlUser(profile.serverAddress, profile.loginName, user.id, timeoutMinutes.value, reason.value)
      : await ModerateRemoteControlUser(profile.serverAddress, profile.loginName, user.id, action, reason.value)
    if (!isCurrent(current, profile)) return
    pendingUser.value = null
    reason.value = ''
    notice.value = moderationNotice(action, user, result)
    isBusy.value = false
    updateObservedTime()
    await loadUsers()
  } catch (failure) { if (isCurrent(current, profile)) failed(failure) }
  finally { if (isCurrent(current, profile)) isBusy.value = false }
}
onBeforeUnmount(() => { isDisposed = true; window.clearInterval(timeoutTimer); emit('busy', false); users.value = []; pendingUser.value = null })
if (tab.value === 'users' && operations.value.includes('admin.users.list')) void loadUsers()
</script>

<template>
  <Tabs :model-value="tab" class="admin-view" @update:model-value="switchTab">
    <TabsList class="admin-tabs" :aria-label="$t('Administrator pages')">
      <TabsTrigger v-if="operations.some(operation => ['server.identity.read', 'admin.announcements.write', 'admin.matchmaking.read', 'admin.capacity.read'].includes(operation))" value="server" :disabled="isAnyBusy">{{ $t('SERVER') }}</TabsTrigger>
      <TabsTrigger v-if="operations.some(operation => ['admin.maintenance.read', 'admin.update.read', 'admin.health.read'].includes(operation))" value="manage" :disabled="isAnyBusy">{{ $t('MANAGE') }}</TabsTrigger>
      <TabsTrigger v-if="operations.includes('admin.diagnostics.list')" value="diagnostic" :disabled="isAnyBusy">{{ $t('DIAGNOSTIC') }}</TabsTrigger>
      <TabsTrigger v-if="operations.includes('admin.users.list')" value="users" :disabled="isAnyBusy">{{ $t('USER') }}</TabsTrigger>
      <TabsTrigger v-if="operations.includes('admin.history.list')" value="history" :disabled="isAnyBusy">{{ $t('AUDIT') }}</TabsTrigger>
    </TabsList>
    <p v-if="error" role="alert">{{ $t(error) }}</p>
    <p v-if="notice" role="status">{{ $t(notice) }}</p>
    <TabsContent v-if="tab === 'server'" value="server"><RemoteControlServer :profile="profile" :panel="panel" @busy="isServerBusy = $event" @invalidated="emit('invalidated', $event)" @changed="emit('changed')" /></TabsContent>
    <TabsContent v-if="tab === 'manage'" value="manage" class="manage-view">
      <RemoteControlManage :profile="profile" :panel="panel" @busy="isManageBusy = $event" @invalidated="emit('invalidated', $event)" @maintenance="emit('maintenance', $event)" />
    </TabsContent>
    <TabsContent v-if="tab === 'diagnostic' && operations.includes('admin.diagnostics.list')" value="diagnostic"><RemoteControlDiagnostic :profile="profile" :panel="panel" @busy="isDiagnosticBusy = $event" @invalidated="emit('invalidated', $event)" /></TabsContent>
    <TabsContent v-if="tab === 'users' && operations.includes('admin.users.list')" value="users">
      <form class="admin-filter" @submit.prevent="loadUsers(false)">
        <Input v-model="search" :aria-label="$t('Search account names')" :placeholder="$t('SEARCH ACCOUNTS')" :maxlength="128" :disabled="isAnyBusy" />
        <Button type="submit" variant="outline" :disabled="isAnyBusy">{{ $t('SEARCH') }}</Button>
      </form>
      <div class="admin-table-scroll"><table>
        <thead><tr><th>{{ $t('ACCOUNT') }}</th><th>{{ $t('LEVEL') }}</th><th>{{ $t('STATUS') }}</th><th>{{ $t('GAME SESSION') }}</th><th>{{ $t('ACCESS') }}</th><th>{{ $t('ACTION') }}</th></tr></thead>
        <tbody><tr v-for="user in users" :key="user.id">
          <td>{{ user.display_name }}<small>{{ user.login_name }} · {{ user.id }}</small></td>
          <td>{{ user.level }}</td><td>{{ user.status }}<small v-if="user.ip_limit">{{ $t('ALLOW IP') }}: {{ user.ip_limit }}</small></td><td>{{ $t(user.is_online ? 'ONLINE' : 'OFFLINE') }}</td><td>{{ $t(user.is_banned ? 'BANNED' : isUserTimedOut(user) ? 'TIMED OUT' : 'ALLOWED') }}<small v-if="isUserTimedOut(user)">{{ formatControlTime(user.timeout_until_unix * 1000) }}</small></td>
          <td class="admin-user-actions"><Button v-if="operations.includes('admin.users.access')" variant="outline" :disabled="isAnyBusy || user.login_name === profile.loginName" @click="editAccess(user)">{{ $t('EDIT ACCESS') }}</Button><Button v-if="operations.includes('admin.users.loginHistory')" variant="outline" :disabled="isAnyBusy" @click="reviewLoginHistory(user)">{{ $t('REVIEW LOGIN HISTORY') }}</Button><Button v-if="operations.includes('admin.users.activity')" variant="outline" :disabled="isAnyBusy" @click="review(user)">{{ $t('REVIEW METRICS') }}</Button><Button v-if="operations.includes(user.is_banned || isUserTimedOut(user) ? 'admin.users.unban' : 'admin.users.ban')" :variant="user.is_banned || isUserTimedOut(user) ? 'outline' : 'destructive'" :disabled="isAnyBusy || user.status > 250 || user.login_name === profile.loginName" @click="confirm(user, user.is_banned || isUserTimedOut(user) ? 'unban' : 'ban')">{{ $t(user.is_banned ? 'UNBAN' : isUserTimedOut(user) ? 'END TIMEOUT' : 'BAN') }}</Button><Button v-if="operations.includes('admin.users.timeout')" variant="outline" :disabled="isAnyBusy || user.is_banned || user.status > 250 || user.login_name === profile.loginName" @click="confirm(user, 'timeout')">{{ $t('TIMEOUT') }}</Button><Button v-if="operations.includes('admin.users.kick')" variant="outline" :disabled="isAnyBusy || !user.is_online || user.status > 250 || user.login_name === profile.loginName" @click="confirm(user, 'kick')">{{ $t('KICK') }}</Button></td>
        </tr></tbody>
      </table></div>
      <p v-if="!users.length && !isBusy">{{ $t('No matching accounts.') }}</p>
      <Button v-if="nextUserID" variant="outline" :disabled="isAnyBusy" @click="loadUsers(true)">{{ $t('LOAD MORE') }}</Button>
      <section v-if="reviewUser && operations.includes('admin.users.activity')" class="admin-review">
        <header><h3>{{ reviewUser.displayName }} · {{ reviewUser.loginName }} · {{ reviewUser.id }}</h3><Button variant="outline" :disabled="isReviewBusy" @click="reviewUser = null">{{ $t('CLOSE METRICS REVIEW') }}</Button></header>
        <RemoteControlPersonalActivity :key="reviewUser.id" :profile="profile" :targetUserID="reviewUser.id" :is-review="true" @busy="isReviewBusy = $event" @invalidated="emit('invalidated', $event)" />
      </section>
      <section v-if="loginReviewUser && operations.includes('admin.users.loginHistory')" class="admin-review">
        <header><h3>{{ loginReviewUser.displayName }} · {{ loginReviewUser.loginName }} · {{ loginReviewUser.id }}</h3><Button variant="outline" :disabled="isReviewBusy" @click="loginReviewUser = null">{{ $t('CLOSE LOGIN HISTORY REVIEW') }}</Button></header>
        <RemoteControlLoginHistory :key="loginReviewUser.id" :profile="profile" :targetUserID="loginReviewUser.id" @busy="isReviewBusy = $event" @invalidated="emit('invalidated', $event)" />
      </section>
    </TabsContent>
    <TabsContent v-if="tab === 'history' && operations.includes('admin.history.list')" value="history"><RemoteControlHistory :profile="profile" @busy="isHistoryBusy = $event" @invalidated="emit('invalidated', $event)" /></TabsContent>
  </Tabs>
  <RemoteControlUserAccess v-if="accessUser" :key="accessUser.id" :profile="profile" :user="accessUser" @close="accessUser = null" @busy="isAccessBusy = $event" @saved="accessSaved" @invalidated="emit('invalidated', $event)" />
  <LauncherDialog v-if="pendingUser" :is-dismissible="!isBusy" @close="cancel">
    <form class="notice-card admin-confirmation" @submit.prevent="moderate">
      <DialogTitle>{{ $t(pendingAction === 'timeout' ? 'TIMEOUT CROGENITOR?' : pendingAction === 'kick' ? 'KICK GAME SESSION?' : pendingAction === 'unban' ? pendingUser.is_banned ? 'UNBAN ACCOUNT?' : 'END TIMEOUT?' : 'BAN ACCOUNT?') }}</DialogTitle>
      <p>{{ pendingUser.display_name }} · {{ pendingUser.login_name }}</p>
      <p v-if="pendingAction === 'ban'">{{ $t('Banning blocks sign-in and disconnects active gameplay. Administrator accounts and your own account are protected.') }}</p>
      <p v-if="pendingAction === 'kick'">{{ $t('Disconnect this active game session. This does not ban the account.') }}</p>
      <div v-if="pendingAction === 'timeout'" class="timeout-duration">
        <label for="control-timeout-duration">{{ $t('DURATION') }}</label>
        <div><Input id="control-timeout-duration" v-model.number="timeoutDuration" type="number" min="1" step="1" :disabled="isBusy" />
          <Select v-model="timeoutUnit" :disabled="isBusy"><SelectTrigger :aria-label="$t('DURATION UNIT')"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="minute">{{ $t('MINUTES') }}</SelectItem><SelectItem value="hour">{{ $t('HOURS') }}</SelectItem><SelectItem value="day">{{ $t('DAYS') }}</SelectItem></SelectContent></Select>
        </div>
        <p v-if="!isTimeoutDurationValid" role="alert">{{ $t('Choose a duration from 1 minute to 365 days.') }}</p>
      </div>
      <label for="control-moderation-reason">{{ $t('REASON') }}</label>
      <Input id="control-moderation-reason" v-model="reason" :maxlength="128" :disabled="isBusy" autocomplete="off" />
      <p v-if="error" role="alert">{{ $t(error) }}</p>
      <div class="admin-confirmation-actions">
        <Button type="button" variant="outline" :disabled="isBusy" @click="cancel">{{ $t('CANCEL') }}</Button>
        <Button type="submit" :variant="pendingUser.is_banned ? 'default' : 'destructive'" :disabled="isBusy || !reason.trim() || (pendingAction === 'timeout' && !isTimeoutDurationValid)">{{ $t(isBusy ? 'WORKING...' : 'CONFIRM') }}</Button>
      </div>
    </form>
  </LauncherDialog>
</template>

<style scoped>
.admin-view { display:flex; flex-direction:column; gap:14px; }
.manage-view { display:flex; flex-direction:column; gap:24px; }
.admin-tabs { max-width:100%; height:auto; min-height:40px; flex-shrink:0; overflow-x:auto; overflow-y:hidden; justify-content:flex-start; }
.admin-filter { display:flex; flex-wrap:wrap; gap:10px; }
.admin-filter input { max-width:360px; }
.admin-table-scroll { overflow:auto; }
table { width:100%; border-collapse:collapse; text-align:left; font-size:12px; }
th, td { padding:10px; border-bottom:1px solid var(--border); vertical-align:top; overflow-wrap:anywhere; }
td small { display:block; color:var(--muted-foreground); }
.admin-confirmation { display:flex; flex-direction:column; gap:12px; }
.timeout-duration { display:flex; flex-direction:column; gap:6px; }
.timeout-duration > div { display:flex; gap:10px; }
.timeout-duration input { width:120px; }
.admin-confirmation-actions { display:flex; justify-content:space-between; gap:12px; }
.admin-user-actions { display:flex; flex-wrap:wrap; gap:8px; }
.admin-review { padding:16px; border:1px solid var(--border); border-radius:8px; }
.admin-review header { display:flex; flex-wrap:wrap; align-items:center; justify-content:space-between; gap:12px; overflow-wrap:anywhere; }
</style>
