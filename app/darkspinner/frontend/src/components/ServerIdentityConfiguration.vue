<script setup>
import { RefreshCw } from '@lucide/vue'
import { onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import ConfigHelp from './ConfigHelp.vue'
import { GetRemoteControlIdentity, SetRemoteControlIdentity } from '../platform'

const props = defineProps({ profile:Object, isActive:{ type:Boolean, default:true }, isEditable:{ type:Boolean, default:true } })
const emit = defineEmits(['busy', 'changed', 'invalidated'])
const draft = ref(null)
const error = ref('')
const isBusy = ref(false)
const isPending = ref(false)
const isConflict = ref(false)
let generation = 0
let editVersion = 0
let timer = null
let isDisposed = false
const fields = [
  { key:'name', label:'SERVER NAME', help:'The name identifying this server.', limit:80 },
  { key:'description', label:'DESCRIPTION', help:'A short description of this server.', limit:1024 },
  { key:'welcome', label:'WELCOME MESSAGE', help:'Sent to players when they sign in.', limit:2048, isMultiline:true },
  { key:'rules', label:'SERVER RULES', help:'Rules players should follow on this server.', limit:8192, isMultiline:true },
  { key:'contact_url', label:'CONTACT LINK', help:'Server website or support link. Use an HTTP or HTTPS address.', limit:2048 },
]
watch([isBusy, isPending], () => emit('busy', isBusy.value || isPending.value), { flush:'sync' })
function isCurrent(current, profile) {
  return !isDisposed && current === generation && props.profile?.serverAddress === profile?.serverAddress && props.profile?.loginName === profile?.loginName
}
function failed(failure) {
  const message = String(failure)
  if (message.includes('controlPanel:outcome_unknown')) {
    isConflict.value = true
    error.value = 'The request was accepted, but its outcome is unknown. Reload server identity and review administration history before trying again.'
    return
  }
  isConflict.value = /conflict|changed/i.test(message)
  error.value = isConflict.value ? 'Server configuration changed elsewhere. Reload before editing again.' : 'Unable to save server identity. Check field lengths, the contact link, and your connection.'
  if (/controlPanel:(unauthorized|forbidden)/.test(message)) emit('invalidated', error.value)
}
async function reload() {
  if (isBusy.value) return
  const current = generation
  const profile = props.profile
  window.clearTimeout(timer)
  isPending.value = false
  isBusy.value = true
  error.value = ''
  try {
    const result = await GetRemoteControlIdentity(profile.serverAddress, profile.loginName)
    if (!isCurrent(current, profile)) return
    draft.value = result
    isConflict.value = false
    editVersion = 0
  } catch (failure) { if (isCurrent(current, profile)) failed(failure) }
  finally { if (isCurrent(current, profile)) isBusy.value = false }
}
function changed() {
  editVersion++
  if (isConflict.value || !props.isEditable) return
  isPending.value = true
  window.clearTimeout(timer)
  timer = window.setTimeout(save, 650)
}
async function save() {
  if (isBusy.value || !isPending.value || !draft.value || isConflict.value || !props.isEditable) return
  window.clearTimeout(timer)
  const current = generation
  const profile = props.profile
  const version = editVersion
  const req = { ...draft.value }
  isBusy.value = true
  isPending.value = false
  error.value = ''
  try {
    const result = await SetRemoteControlIdentity(profile.serverAddress, profile.loginName, req)
    if (!isCurrent(current, profile)) return
    if (version === editVersion) draft.value = result
    else draft.value.revision = result.revision
    emit('changed', result)
  } catch (failure) { if (isCurrent(current, profile)) failed(failure) }
  finally {
    if (isCurrent(current, profile)) {
      isBusy.value = false
      if (error.value) { isPending.value = false; window.clearTimeout(timer) }
      if (!error.value && version !== editVersion) changed()
    }
  }
}
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  draft.value = null
  error.value = ''
  isConflict.value = false
  isBusy.value = false
  isPending.value = false
  window.clearTimeout(timer)
  if (props.isActive) void reload()
}, { immediate:true, flush:'sync' })
watch(() => props.isActive, next => { if (next && !draft.value) void reload() })
onBeforeUnmount(() => { isDisposed = true; window.clearTimeout(timer); emit('busy', false) })
</script>

<template>
  <section class="server-identity-editor">
    <h3>{{ $t('SERVER IDENTITY') }}</h3>
    <p v-if="!isEditable">{{ $t('Server identity is read-only on this server.') }}</p>
    <template v-if="draft">
      <div v-for="field in fields" :key="field.key" class="identity-field">
        <label :for="`remote-identity-${field.key}`">{{ $t(field.label) }}<ConfigHelp :text="field.help" /></label>
        <textarea v-if="field.isMultiline" :id="`remote-identity-${field.key}`" v-model="draft[field.key]" :maxlength="field.limit" :disabled="!isEditable || isConflict" rows="4" @input="changed" @change="save"></textarea>
        <Input v-else :id="`remote-identity-${field.key}`" v-model="draft[field.key]" :maxlength="field.limit" :disabled="!isEditable || isConflict" :type="field.key === 'contact_url' ? 'url' : 'text'" @input="changed" @change="save" />
      </div>
    </template>
    <p v-if="error" class="configuration-error" role="alert">{{ $t(error) }}</p>
    <Button v-if="error || !draft" variant="outline" :disabled="isBusy" @click="reload" size="icon" :aria-label="$t('RELOAD')" :title="$t('RELOAD')"><RefreshCw :size="16" aria-hidden="true" /></Button>
  </section>
</template>

<style scoped>
.server-identity-editor { display:flex; flex-direction:column; gap:14px; }
.server-identity-editor h3 { font-size:16px; }
.server-identity-editor p { color:var(--muted-foreground); font-size:12px; }
.identity-field { display:flex; flex-direction:column; gap:6px; }
.identity-field label { font-size:11px; font-weight:600; }
.identity-field textarea { width:100%; resize:vertical; border:1px solid var(--border); background:var(--background); padding:10px; border-radius:6px; font:inherit; }
.identity-field textarea:focus-visible { outline:2px solid var(--ring); }
.server-identity-editor .configuration-error { color:var(--destructive); }
.server-identity-editor button { align-self:flex-start; }
</style>
