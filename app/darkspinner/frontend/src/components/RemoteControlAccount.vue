<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { DialogTitle } from '@/components/ui/dialog'
import LauncherDialog from './LauncherDialog.vue'
import RemoteControlSessions from './RemoteControlSessions.vue'
import { RenameRemoteControlAccount, ChangeRemoteControlPassword, DeleteRemoteControlAccount } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, panel:Object })
const emit = defineEmits(['renamed', 'password-changing', 'password-changed', 'deleted', 'busy', 'invalidated'])
const displayName = ref(props.panel.profile.display_name)
const newPassword = ref('')
const repeatedPassword = ref('')
const currentPassword = ref('')
const confirmation = ref('')
const isBusy = ref(false)
const isSessionBusy = ref(false)
const isActionBusy = computed(() => isBusy.value || isSessionBusy.value)
const error = ref('')
const notice = ref('')
let isDisposed = false
const operations = computed(() => props.panel.server.supported_operations || [])
const isPasswordReady = computed(() => !!newPassword.value && newPassword.value === repeatedPassword.value)
const titles = { rename:'CHANGE DISPLAY NAME?', password:'CHANGE PASSWORD?' }
let generation = 0
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  confirmation.value = ''
  currentPassword.value = ''
  newPassword.value = ''
  repeatedPassword.value = ''
  error.value = ''
  notice.value = ''
  isBusy.value = false
}, { flush:'sync' })
watch(isActionBusy, next => emit('busy', next), { flush:'sync' })
function confirm(action) {
  if (isActionBusy.value) return
  currentPassword.value = ''
  error.value = ''
  notice.value = ''
  confirmation.value = action
}
function cancel() {
  if (isBusy.value) return
  confirmation.value = ''
  currentPassword.value = ''
  error.value = ''
}
async function submit() {
  if (isActionBusy.value || !currentPassword.value) return
  const action = confirmation.value
  const profile = props.profile
  const current = generation
  isBusy.value = true
  if (action === 'password') emit('password-changing')
  error.value = ''
  try {
    let result
    if (action === 'rename') result = await RenameRemoteControlAccount(profile.serverAddress, profile.loginName, currentPassword.value, displayName.value)
    else if (action === 'password') result = await ChangeRemoteControlPassword(profile.serverAddress, profile.loginName, currentPassword.value, newPassword.value)
    else if (action === 'delete') result = await DeleteRemoteControlAccount(profile.serverAddress, profile.loginName, currentPassword.value)
    else return
    if (isDisposed || current !== generation || props.profile?.serverAddress !== profile.serverAddress || props.profile?.loginName !== profile.loginName) return
    confirmation.value = ''
    currentPassword.value = ''
    newPassword.value = ''
    repeatedPassword.value = ''
    notice.value = result.is_cache_updated ? 'The server saved your account change.' : 'The server saved your change, but the local profile cache could not be updated.'
    if (action === 'rename') emit('renamed', result)
    if (action === 'password') emit('password-changed', result)
    if (action === 'delete') emit('deleted', result)
  } catch (failure) {
    if (!isDisposed && current === generation) error.value = panelError(failure)
    if (!isDisposed && current === generation && /controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
  } finally {
    if (!isDisposed && current === generation) { currentPassword.value = ''; isBusy.value = false }
  }
}
onBeforeUnmount(() => { isDisposed = true; emit('busy', false); currentPassword.value = ''; newPassword.value = ''; repeatedPassword.value = '' })
</script>

<template>
  <div class="account-actions">
    <p>{{ $t('Close the game before changing your display name, changing your password, or deleting your Crogenitor.') }}</p>
    <p v-if="notice" role="status">{{ $t(notice) }}</p>
    <form v-if="operations.includes('account.rename')" @submit.prevent="confirm('rename')">
      <label for="control-display-name">{{ $t('DISPLAY NAME') }}</label>
      <Input id="control-display-name" v-model="displayName" :disabled="isActionBusy" autocomplete="off" />
      <p>{{ $t('Rename changes your public display name. Your login name stays the same. The account must be offline.') }}</p>
      <Button type="submit" variant="outline" :disabled="isActionBusy || !displayName.trim() || displayName === panel.profile.display_name">{{ $t('CHANGE DISPLAY NAME') }}</Button>
    </form>
    <form v-if="operations.includes('account.password')" @submit.prevent="confirm('password')">
      <label for="control-new-password">{{ $t('NEW PASSWORD') }}</label>
      <Input id="control-new-password" v-model="newPassword" type="password" autocomplete="new-password" :disabled="isActionBusy" />
      <label for="control-repeat-password">{{ $t('REPEAT NEW PASSWORD') }}</label>
      <Input id="control-repeat-password" v-model="repeatedPassword" type="password" autocomplete="new-password" :disabled="isActionBusy" />
      <p>{{ $t('Changing your password ends panel sessions and unused launch credentials. You will need to sign in again.') }}</p>
      <Button type="submit" variant="outline" :disabled="isActionBusy || !isPasswordReady">{{ $t('CHANGE PASSWORD') }}</Button>
    </form>
    <section v-if="operations.includes('account.delete')">
      <Button variant="destructive" :disabled="isActionBusy" @click="confirm('delete')">{{ $t('DELETE CROGENITOR') }}</Button>
    </section>
    <RemoteControlSessions v-if="operations.includes('account.sessions.read')" :profile="profile" :panel="panel" :is-blocked="isBusy || !!confirmation" @busy="isSessionBusy = $event" @invalidated="emit('invalidated', $event)" />
  </div>
  <LauncherDialog v-if="confirmation" :is-dismissible="!isBusy" @close="cancel">
    <form class="notice-card account-confirmation" @submit.prevent="submit">
      <DialogTitle>{{ $t(confirmation === 'delete' ? 'DELETE CROGENITOR' : titles[confirmation]) }}</DialogTitle>
      <p>{{ panel.profile.display_name }} · {{ profile.serverAddress }}</p>
      <p v-if="confirmation === 'delete'">{{ $t('Are you sure you want to delete the Crogenitor {0}?', [panel.profile.display_name]) }}</p>
      <p v-else-if="confirmation === 'rename'">{{ $t('NEW DISPLAY NAME') }}: {{ displayName }}</p>
      <p>{{ $t('Enter your current password to confirm this account change.') }}</p>
      <Input v-model="currentPassword" type="password" autocomplete="current-password" :aria-label="$t('CURRENT PASSWORD')" :disabled="isActionBusy" />
      <p v-if="error" role="alert">{{ $t(error) }}</p>
      <div class="account-confirmation-actions">
        <Button type="button" variant="outline" :disabled="isActionBusy" @click="cancel">{{ $t('CANCEL') }}</Button>
        <Button type="submit" :variant="confirmation === 'delete' ? 'destructive' : 'default'" :disabled="isActionBusy || !currentPassword">{{ $t(isBusy ? 'WORKING...' : 'CONFIRM') }}</Button>
      </div>
    </form>
  </LauncherDialog>
</template>

<style scoped>
.account-actions { display:grid; gap:24px; }
.account-actions form, .account-actions section, .account-confirmation { display:flex; flex-direction:column; gap:10px; }
.account-actions p { color:var(--muted-foreground); }
.account-actions button { align-self:flex-start; }
.account-confirmation-actions { display:flex; justify-content:space-between; gap:12px; }
</style>
