<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import ConfigHelp from './ConfigHelp.vue'
import { GetRemoteControlCapacity, SetRemoteControlCapacity } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, isDisabled:Boolean })
const emit = defineEmits(['busy', 'invalidated'])
const worldPlayerLimit = ref('')
const savedWorldPlayerLimit = ref(null)
const isBusy = ref(false)
const isReloadRequired = ref(false)
const error = ref('')
const isLimitValid = computed(() => Number.isInteger(Number(worldPlayerLimit.value)) && Number(worldPlayerLimit.value) > 0 && Number(worldPlayerLimit.value) <= 4294967295)
const isChanged = computed(() => savedWorldPlayerLimit.value !== null && Number(worldPlayerLimit.value) !== savedWorldPlayerLimit.value)
let generation = 0
let isDisposed = false
function isCurrent(current, profile) {
  return !isDisposed && current === generation && profile?.serverAddress === props.profile?.serverAddress && profile?.loginName === props.profile?.loginName
}
function failed(failure) {
  error.value = panelError(failure)
  if (String(failure).includes('controlPanel:outcome_unknown')) isReloadRequired.value = true
  if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
}
async function reload() {
  if (isBusy.value) return
  const current = generation
  const profile = props.profile
  isBusy.value = true
  error.value = ''
  try {
    const result = await GetRemoteControlCapacity(profile.serverAddress, profile.loginName)
    if (!isCurrent(current, profile)) return
    savedWorldPlayerLimit.value = result.world_player_limit
    worldPlayerLimit.value = String(result.world_player_limit)
    isReloadRequired.value = false
  } catch (failure) { if (isCurrent(current, profile)) failed(failure) }
  finally { if (isCurrent(current, profile)) isBusy.value = false }
}
async function save() {
  if (isBusy.value || props.isDisabled || isReloadRequired.value || !isLimitValid.value || !isChanged.value) return
  const current = generation
  const profile = props.profile
  isBusy.value = true
  error.value = ''
  try {
    const result = await SetRemoteControlCapacity(profile.serverAddress, profile.loginName, { world_player_limit:Number(worldPlayerLimit.value) })
    if (!isCurrent(current, profile)) return
    savedWorldPlayerLimit.value = result.world_player_limit
    worldPlayerLimit.value = String(result.world_player_limit)
  } catch (failure) { if (isCurrent(current, profile)) failed(failure) }
  finally { if (isCurrent(current, profile)) isBusy.value = false }
}
watch(isBusy, next => emit('busy', next), { flush:'sync' })
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  savedWorldPlayerLimit.value = null
  worldPlayerLimit.value = ''
  error.value = ''
  isBusy.value = false
  isReloadRequired.value = false
  void reload()
}, { immediate:true, flush:'sync' })
onBeforeUnmount(() => { isDisposed = true; emit('busy', false) })
</script>

<template>
  <section class="capacity-editor">
    <h3>{{ $t('SERVER CAPACITY') }}</h3>
    <form v-if="savedWorldPlayerLimit !== null" @submit.prevent="save">
      <label for="remote-world-player-limit">{{ $t('WORLD PLAYER LIMIT') }}<ConfigHelp text="Maximum number of players admitted to this server at once." /></label>
      <Input id="remote-world-player-limit" v-model="worldPlayerLimit" type="number" min="1" max="4294967295" step="1" inputmode="numeric" :disabled="isBusy || isDisabled || isReloadRequired" />
      <Button type="submit" :disabled="isBusy || isDisabled || isReloadRequired || !isLimitValid || !isChanged">{{ $t('SAVE') }}</Button>
    </form>
    <p v-if="error" role="alert">{{ $t(error) }}</p>
    <Button v-if="error || savedWorldPlayerLimit === null" variant="outline" :disabled="isBusy || isDisabled" @click="reload">{{ $t('RELOAD') }}</Button>
  </section>
</template>

<style scoped>
.capacity-editor, .capacity-editor form { display:flex; flex-direction:column; gap:14px; }
.capacity-editor h3 { font-size:16px; }
.capacity-editor label { font-size:11px; font-weight:600; }
.capacity-editor [role="alert"] { color:var(--destructive); font-size:12px; }
.capacity-editor button { align-self:flex-start; }
</style>
