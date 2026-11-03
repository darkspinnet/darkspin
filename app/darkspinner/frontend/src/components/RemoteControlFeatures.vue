<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { DialogTitle } from '@/components/ui/dialog'
import LauncherDialog from './LauncherDialog.vue'
import { RenameRemoteControlAccount, SetRemoteControlPassword } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, panel:Object, isDeletionDisabled:Boolean, isShiftHeld:Boolean, isLocal:Boolean })
const emit = defineEmits(['busy', 'renamed', 'invalidated', 'delete', 'password-changed'])
const displayName = ref(props.panel.profile.display_name)
const currentPassword = ref('')
const oldPassword = ref('')
const newPassword = ref('')
const repeatedPassword = ref('')
const isPasswordRemembered = ref(!!props.profile?.isPasswordRemembered)
const isConfirming = ref(false)
const isBusy = ref(false)
const error = ref('')
const notice = ref('')
const isRenameAvailable = computed(() => props.panel.server.supported_operations?.includes('account.rename'))
const isPasswordAvailable = computed(() => props.panel.server.supported_operations?.includes('account.password'))
const isPasswordMismatched = computed(() => !!repeatedPassword.value && newPassword.value !== repeatedPassword.value)
const isPasswordReady = computed(() => !!oldPassword.value && !!newPassword.value && newPassword.value === repeatedPassword.value)
const isNameReady = computed(() => displayName.value.trim() !== props.panel.profile.display_name)
let generation = 0
let isDisposed = false
watch(isBusy, next => emit('busy', next), { flush:'sync' })
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  displayName.value = props.panel.profile.display_name
  currentPassword.value = ''
  clearPassword()
  isPasswordRemembered.value = !!props.profile?.isPasswordRemembered
  isConfirming.value = false
  isBusy.value = false
  error.value = ''
  notice.value = ''
}, { flush:'sync' })
function confirmRename() {
  if (isBusy.value || !isNameReady.value) return
  error.value = ''
  notice.value = ''
  const name = displayName.value.trim()
  if (!name || name.length > 20 || !/^[A-Za-z0-9]+$/.test(name)) {
    const code = !name ? 'username_required' : name.length > 20 ? 'username_too_long' : 'username_pattern'
    error.value = panelError(`controlPanel:${code}`)
    return
  }
  currentPassword.value = ''
  isConfirming.value = true
}
function cancelRename() {
  if (isBusy.value) return
  isConfirming.value = false
  currentPassword.value = ''
  error.value = ''
}
function requestDelete(event) {
  if (isBusy.value || props.isDeletionDisabled) return
  emit('delete', !props.isLocal && (props.isShiftHeld || !!event?.shiftKey))
}
function clearPassword() {
  oldPassword.value = ''
  newPassword.value = ''
  repeatedPassword.value = ''
}
async function changePassword() {
  if (isBusy.value || props.isDeletionDisabled || !isPasswordAvailable.value || !isPasswordReady.value) return
  const current = generation
  const profile = { ...props.profile }
  const isRemembered = isPasswordRemembered.value
  isBusy.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await SetRemoteControlPassword(profile.serverAddress, profile.loginName,
      oldPassword.value, newPassword.value, isRemembered)
    if (isDisposed || current !== generation) return
    clearPassword()
    emit('password-changed', { ...result, isPasswordRemembered:isRemembered })
  } catch (failure) {
    if (isDisposed || current !== generation) return
    error.value = panelError(failure)
    if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
  } finally {
    if (!isDisposed && current === generation) {
      oldPassword.value = ''
      isBusy.value = false
    }
  }
}
async function rename() {
  if (isBusy.value || !isRenameAvailable.value || !isNameReady.value || !currentPassword.value) return
  const current = generation
  const profile = { ...props.profile }
  isBusy.value = true
  error.value = ''
  try {
    const result = await RenameRemoteControlAccount(profile.serverAddress, profile.loginName, currentPassword.value, displayName.value.trim())
    if (isDisposed || current !== generation) return
    isConfirming.value = false
    notice.value = result.is_cache_updated ? 'The server saved your account change.' : 'The server saved your change, but the local profile cache could not be updated.'
    emit('renamed', result)
  } catch (failure) {
    if (isDisposed || current !== generation) return
    error.value = panelError(failure)
    if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
  } finally {
    if (!isDisposed && current === generation) {
      currentPassword.value = ''
      isBusy.value = false
    }
  }
}
onBeforeUnmount(() => { isDisposed = true; generation++; currentPassword.value = ''; clearPassword(); emit('busy', false) })
</script>

<template>
  <section class="control-features" :aria-label="$t('FEATURES')">
    <form v-if="isRenameAvailable" class="feature-group rename-feature" @submit.prevent="confirmRename">
      <h3 id="feature-rename-title">{{ $t('RENAME CROGENITOR') }}</h3>
      <div class="rename-field">
        <Input id="feature-crogenitor-name" v-model="displayName" aria-labelledby="feature-rename-title" :disabled="isBusy" autocomplete="off" spellcheck="false" />
        <Button type="submit" variant="outline" :disabled="isBusy || !isNameReady">{{ $t('RENAME') }}</Button>
      </div>
    </form>
    <form v-if="isPasswordAvailable" class="feature-group password-feature" @submit.prevent="changePassword">
      <h3>{{ $t('CHANGE PASSWORD') }}</h3>
      <label for="feature-old-password">{{ $t('CURRENT PASSWORD') }}</label>
      <Input id="feature-old-password" v-model="oldPassword" type="password" autocomplete="current-password" :disabled="isBusy || isDeletionDisabled" />
      <div class="password-pair">
        <div class="password-field">
          <label for="feature-new-password">{{ $t('NEW PASSWORD') }}</label>
          <Input id="feature-new-password" v-model="newPassword" type="password" autocomplete="new-password" :disabled="isBusy || isDeletionDisabled" />
        </div>
        <div class="password-field">
          <label for="feature-repeat-password">{{ $t('REPEAT NEW PASSWORD') }}</label>
          <Input id="feature-repeat-password" v-model="repeatedPassword" type="password" autocomplete="new-password" :disabled="isBusy || isDeletionDisabled" :aria-invalid="isPasswordMismatched" :aria-describedby="isPasswordMismatched ? 'feature-password-mismatch' : undefined" />
        </div>
      </div>
      <p v-if="isPasswordMismatched" id="feature-password-mismatch" role="alert">{{ $t('New passwords do not match.') }}</p>
      <label class="password-save"><Checkbox v-model="isPasswordRemembered" :disabled="isBusy || isDeletionDisabled" /><span>{{ $t('SAVE PASSWORD') }}</span></label>
      <Button type="submit" variant="outline" :disabled="isBusy || isDeletionDisabled || !isPasswordReady">{{ $t(isBusy ? 'WORKING...' : 'CHANGE PASSWORD') }}</Button>
    </form>
    <section class="feature-group delete-feature">
      <h3>{{ $t('DELETE CROGENITOR') }}</h3>
      <Button type="button" variant="destructive" :disabled="isBusy || isDeletionDisabled" @click="requestDelete">{{ $t(!isLocal && isShiftHeld ? 'LOCAL DELETE' : 'DELETE') }}</Button>
    </section>
    <p v-if="notice" role="status">{{ $t(notice) }}</p>
    <p v-if="error && !isConfirming" role="alert">{{ $t(error) }}</p>
  </section>
  <LauncherDialog v-if="isConfirming" :is-dismissible="!isBusy" @close="cancelRename">
    <form class="notice-card rename-confirmation" @submit.prevent="rename">
      <DialogTitle>{{ $t('CHANGE DISPLAY NAME?') }}</DialogTitle>
      <p>{{ $t('NEW DISPLAY NAME') }}: {{ displayName.trim() }}</p>
      <label for="feature-rename-password">{{ $t('CURRENT PASSWORD') }}</label>
      <Input id="feature-rename-password" v-model="currentPassword" type="password" autocomplete="current-password" :disabled="isBusy" />
      <p v-if="error" role="alert">{{ $t(error) }}</p>
      <div class="rename-confirmation-actions">
        <Button type="button" variant="outline" :disabled="isBusy" @click="cancelRename">{{ $t('CANCEL') }}</Button>
        <Button type="submit" :disabled="isBusy || !currentPassword">{{ $t(isBusy ? 'WORKING...' : 'CONFIRM') }}</Button>
      </div>
    </form>
  </LauncherDialog>
</template>

<style scoped>
.control-features { display:flex; flex-direction:column; gap:16px; margin-top:18px; min-height:min(360px, 50dvh); }
.feature-group { padding:16px; border:1px solid var(--border); border-radius:8px; background:var(--background); }
.rename-feature { display:flex; flex-direction:column; gap:10px; max-width:480px; }
.delete-feature { display:flex; flex-direction:column; align-items:flex-start; gap:10px; }
.password-feature { display:flex; flex-direction:column; gap:10px; max-width:480px; }
.password-feature > button { align-self:flex-start; }
.password-pair { display:grid; grid-template-columns:repeat(auto-fit,minmax(min(100%,180px),1fr)); gap:12px; }
.password-field { display:flex; flex-direction:column; gap:6px; min-width:0; }
.password-save { display:flex; align-items:center; gap:10px; }
.rename-field { display:flex; align-items:center; gap:10px; }
.rename-field input { flex:1; min-width:0; }
.rename-field button { flex-shrink:0; }
p { font-size:12px; }
.rename-confirmation { display:flex; flex-direction:column; gap:12px; }
.rename-confirmation-actions { display:flex; justify-content:space-between; gap:12px; }
</style>
