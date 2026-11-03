<script setup>
import { computed, onBeforeUnmount, ref } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { DialogTitle } from '@/components/ui/dialog'
import LauncherDialog from './LauncherDialog.vue'
import { SetRemoteControlUserAccess } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, user:Object })
const emit = defineEmits(['busy', 'close', 'saved', 'invalidated'])
const status = ref(Number(props.user.status))
const ipLimit = ref(props.user.ip_limit || '')
const reason = ref('')
const isBusy = ref(false)
const error = ref('')
const isStatusValid = computed(() => Number.isSafeInteger(status.value) && status.value >= 0)
const isProtectedIPMissing = computed(() => status.value >= 250 && !ipLimit.value.trim())
const isChanged = computed(() => status.value !== Number(props.user.status) || ipLimit.value.trim() !== (props.user.ip_limit || ''))
let isDisposed = false

function close() { if (!isBusy.value) emit('close') }
async function save() {
  if (isBusy.value || !isChanged.value || !reason.value.trim()) return
  if (!isStatusValid.value) { error.value = 'Status must be a nonnegative integer.'; return }
  if (isProtectedIPMissing.value) { error.value = 'Status 250 or higher requires an allowed IP address or CIDR.'; return }
  isBusy.value = true
  emit('busy', true)
  error.value = ''
  try {
    const result = await SetRemoteControlUserAccess(props.profile.serverAddress, props.profile.loginName, props.user.id, status.value, ipLimit.value.trim(), reason.value.trim())
    if (isDisposed) return
    isBusy.value = false
    emit('busy', false)
    emit('saved', result)
  } catch (failure) {
    if (isDisposed) return
    error.value = String(failure).includes('controlPanel:invalid_request')
      ? 'Enter a nonnegative status, a valid IP address or CIDR, and a reason.' : panelError(failure)
    if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
  } finally {
    if (!isDisposed) { isBusy.value = false; emit('busy', false) }
  }
}
onBeforeUnmount(() => { isDisposed = true; emit('busy', false) })
</script>

<template>
  <LauncherDialog :is-dismissible="!isBusy" @close="close">
    <form class="notice-card access-form" @submit.prevent="save">
      <DialogTitle>{{ $t('EDIT USER ACCESS') }}</DialogTitle>
      <p>{{ user.display_name }} · {{ user.login_name }}</p>
      <label for="user-access-status">{{ $t('STATUS') }}<Input id="user-access-status" v-model.number="status" type="number" min="0" step="1" :disabled="isBusy" /></label>
      <label for="user-access-ip">{{ $t('ALLOW IP') }}<Input id="user-access-ip" v-model="ipLimit" :maxlength="128" :placeholder="$t('IP address or CIDR')" autocomplete="off" spellcheck="false" :disabled="isBusy" /></label>
      <label for="user-access-reason">{{ $t('REASON') }}<Input id="user-access-reason" v-model="reason" :maxlength="128" :disabled="isBusy" /></label>
      <p v-if="error" role="alert">{{ $t(error) }}</p>
      <div class="access-actions"><Button variant="outline" type="button" :disabled="isBusy" @click="close">{{ $t('CANCEL') }}</Button><Button type="submit" :disabled="isBusy || !isChanged || !reason.trim()">{{ $t(isBusy ? 'WORKING...' : 'SAVE') }}</Button></div>
    </form>
  </LauncherDialog>
</template>

<style scoped>
.access-form { display:flex; flex-direction:column; gap:14px; }
.access-form label { display:flex; flex-direction:column; gap:6px; }
.access-form p { color:var(--muted-foreground); font-size:12px; }
.access-actions { display:flex; align-items:center; justify-content:space-between; gap:12px; }
</style>
