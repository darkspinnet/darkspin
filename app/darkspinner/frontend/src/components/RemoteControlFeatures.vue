<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { DialogTitle } from '@/components/ui/dialog'
import LauncherDialog from './LauncherDialog.vue'
import { RenameRemoteControlAccount } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, panel:Object })
const emit = defineEmits(['busy', 'renamed', 'invalidated'])
const displayName = ref(props.panel.profile.display_name)
const currentPassword = ref('')
const isConfirming = ref(false)
const isBusy = ref(false)
const error = ref('')
const notice = ref('')
const isRenameAvailable = computed(() => props.panel.server.supported_operations?.includes('account.rename'))
const isNameReady = computed(() => displayName.value.trim() !== props.panel.profile.display_name)
let generation = 0
let isDisposed = false
watch(isBusy, next => emit('busy', next), { flush:'sync' })
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  displayName.value = props.panel.profile.display_name
  currentPassword.value = ''
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
onBeforeUnmount(() => { isDisposed = true; generation++; currentPassword.value = ''; emit('busy', false) })
</script>

<template>
  <section class="control-features" :aria-label="$t('FEATURES')">
    <form v-if="isRenameAvailable" class="rename-feature" @submit.prevent="confirmRename">
      <h3 id="feature-rename-title">{{ $t('RENAME CROGENITOR') }}</h3>
      <div class="rename-field">
        <Input id="feature-crogenitor-name" v-model="displayName" aria-labelledby="feature-rename-title" :disabled="isBusy" autocomplete="off" spellcheck="false" />
        <Button type="submit" variant="outline" :disabled="isBusy || !isNameReady">{{ $t('RENAME') }}</Button>
      </div>
    </form>
    <p v-else>{{ $t('No player operations are available on this server.') }}</p>
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
.rename-feature { display:flex; flex-direction:column; gap:10px; max-width:480px; }
.rename-field { display:flex; align-items:center; gap:10px; }
.rename-field input { flex:1; min-width:0; }
.rename-field button { flex-shrink:0; }
p { font-size:12px; }
.rename-confirmation { display:flex; flex-direction:column; gap:12px; }
.rename-confirmation-actions { display:flex; justify-content:space-between; gap:12px; }
</style>
