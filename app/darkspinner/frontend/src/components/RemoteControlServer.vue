<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import ServerIdentityConfiguration from './ServerIdentityConfiguration.vue'
import RemoteControlMatchmaking from './RemoteControlMatchmaking.vue'
import RemoteControlCapacity from './RemoteControlCapacity.vue'
import ConfigHelp from './ConfigHelp.vue'
import { SendRemoteControlAnnouncement } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, panel:Object })
const emit = defineEmits(['busy', 'changed', 'invalidated'])
const message = ref('')
const delivery = ref(null)
const error = ref('')
const isBusy = ref(false)
const isIdentityBusy = ref(false)
const isMatchmakingBusy = ref(false)
const isCapacityBusy = ref(false)
const operations = computed(() => props.panel.server.supported_operations || [])
const isMessageReady = computed(() => !!message.value.trim() && new TextEncoder().encode(message.value).length <= 512)
function updateAnnouncement(event) {
  if (event.isComposing) return
  const input = event.target
  const encoder = new TextEncoder()
  let acceptedMessage = ''
  let byteCount = 0
  for (const character of input.value) {
    const byteLength = encoder.encode(character).length
    if (byteCount + byteLength > 512) break
    acceptedMessage += character
    byteCount += byteLength
  }
  message.value = acceptedMessage
  if (input.value !== acceptedMessage) input.value = acceptedMessage
}
let generation = 0
let isDisposed = false
watch([isBusy, isIdentityBusy, isMatchmakingBusy, isCapacityBusy], () => emit('busy', isBusy.value || isIdentityBusy.value || isMatchmakingBusy.value || isCapacityBusy.value), { flush:'sync' })
function isCurrent(current, profile) {
  return !isDisposed && current === generation && props.profile?.serverAddress === profile?.serverAddress && props.profile?.loginName === profile?.loginName
}
function failed(failure) {
  error.value = panelError(failure)
  if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
}
async function announce() {
  if (!isMessageReady.value || isBusy.value) return
  const current = generation
  const profile = props.profile
  isBusy.value = true
  error.value = ''
  delivery.value = null
  try {
    const result = await SendRemoteControlAnnouncement(profile.serverAddress, profile.loginName, message.value)
    if (!isCurrent(current, profile)) return
    delivery.value = result
    message.value = ''
    emit('changed')
  } catch (failure) { if (isCurrent(current, profile)) failed(failure) }
  finally { if (isCurrent(current, profile)) isBusy.value = false }
}
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  delivery.value = null
  message.value = ''
  error.value = ''
  isBusy.value = false
}, { immediate:true, flush:'sync' })
onBeforeUnmount(() => { isDisposed = true; emit('busy', false) })
</script>

<template>
  <div class="server-management">
    <ServerIdentityConfiguration v-if="operations.includes('admin.identity.write')" :profile="profile" @busy="isIdentityBusy = $event" @changed="emit('changed')" @invalidated="emit('invalidated', $event)" />
    <RemoteControlCapacity v-if="operations.includes('admin.capacity.write')" :profile="profile" :is-disabled="isBusy || isIdentityBusy || isMatchmakingBusy" @busy="isCapacityBusy = $event" @invalidated="emit('invalidated', $event)" />
    <RemoteControlMatchmaking v-if="operations.includes('admin.matchmaking.write')" :profile="profile" :is-disabled="isBusy || isIdentityBusy || isCapacityBusy" @busy="isMatchmakingBusy = $event" @invalidated="emit('invalidated', $event)" />
    <section v-if="operations.includes('admin.announcements.write')">
      <h3>{{ $t('ANNOUNCEMENTS') }}<ConfigHelp text="Send a message to connected players" /></h3>
      <form @submit.prevent="announce">
        <label for="control-announcement">{{ $t('MESSAGE TO CONNECTED PLAYERS') }}</label>
        <textarea id="control-announcement" :value="message" rows="3" :disabled="isBusy || isIdentityBusy || isMatchmakingBusy || isCapacityBusy" @input="updateAnnouncement" @compositionend="updateAnnouncement"></textarea>
        <Button type="submit" :disabled="!isMessageReady || isBusy || isIdentityBusy || isMatchmakingBusy || isCapacityBusy">{{ $t('SEND ANNOUNCEMENT') }}</Button>
      </form>
      <p v-if="delivery" role="status">{{ $t('Delivered to {0} connected players.', [delivery.delivered]) }} {{ $t(delivery.is_delivery_partial ? 'Delivery was partial; some players may not have received the message.' : 'The server completed this delivery attempt.') }}</p>
      <p v-if="delivery && delivery.outcome === 'failed'" role="alert">{{ $t('The announcement delivery failed.') }}</p>
      <p v-if="delivery && !delivery.is_audit_recorded" role="alert">{{ $t('The announcement outcome could not be recorded in admin history.') }}</p>
    </section>
    <p v-if="error" role="alert">{{ $t(error) }}</p>
  </div>
</template>

<style scoped>
.server-management, .server-management section, .server-management form { display:flex; flex-direction:column; gap:14px; }
.server-management > section { border-top:1px solid var(--border); padding-top:20px; }
.server-management textarea { width:100%; resize:vertical; padding:10px; border:1px solid var(--border); border-radius:6px; background:var(--background); font:inherit; }
.server-management textarea:focus-visible { outline:2px solid var(--ring); }
.server-management label { font-size:11px; font-weight:600; }
.server-management p { color:var(--muted-foreground); font-size:12px; }
.server-management button { align-self:flex-start; }
</style>
