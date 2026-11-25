<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import ConfigHelp from './ConfigHelp.vue'
import { GetRemoteControlMatchmaking, SetRemoteControlMatchmaking } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, isDisabled:Boolean })
const emit = defineEmits(['busy', 'invalidated'])
const draft = ref(null)
const botSize = ref('1')
const isBusy = ref(false)
const error = ref('')
let generation = 0
let isDisposed = false
function isCurrent(current, profile) {
  return !isDisposed && current === generation && profile?.serverAddress === props.profile?.serverAddress && profile?.loginName === props.profile?.loginName
}
function failed(failure) {
  error.value = panelError(failure)
  if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
}
async function reload() {
  if (isBusy.value) return
  const current = generation
  const profile = props.profile
  isBusy.value = true
  error.value = ''
  try {
    const result = await GetRemoteControlMatchmaking(profile.serverAddress, profile.loginName)
    if (!isCurrent(current, profile)) return
    draft.value = result
    botSize.value = String(result.campaign_matchmaking_bot_size)
  } catch (failure) { if (isCurrent(current, profile)) failed(failure) }
  finally { if (isCurrent(current, profile)) isBusy.value = false }
}
async function save() {
  if (isBusy.value || props.isDisabled || !draft.value) return
  const current = generation
  const profile = props.profile
  isBusy.value = true
  error.value = ''
  try {
    const result = await SetRemoteControlMatchmaking(profile.serverAddress, profile.loginName, {
      campaign_matchmaking_bot_size:Number(botSize.value),
      is_campaign_bot_squad_mirroring_enabled:draft.value.is_campaign_bot_squad_mirroring_enabled,
    })
    if (!isCurrent(current, profile)) return
    draft.value = result
    botSize.value = String(result.campaign_matchmaking_bot_size)
  } catch (failure) { if (isCurrent(current, profile)) failed(failure) }
  finally { if (isCurrent(current, profile)) isBusy.value = false }
}
watch(isBusy, next => emit('busy', next), { flush:'sync' })
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  draft.value = null
  error.value = ''
  isBusy.value = false
  void reload()
}, { immediate:true, flush:'sync' })
onBeforeUnmount(() => { isDisposed = true; emit('busy', false) })
</script>

<template>
  <section class="matchmaking-editor">
    <h3>{{ $t('MATCHMAKING') }}</h3>
    <template v-if="draft">
      <label for="remote-matchmaking-size">{{ $t('MATCHMAKING') }}<ConfigHelp text="Choose how many bots join your campaign party." /></label>
      <Select v-model="botSize" :disabled="isBusy || isDisabled" @update:model-value="save">
        <SelectTrigger id="remote-matchmaking-size"><SelectValue /></SelectTrigger>
        <SelectContent><SelectItem v-for="count in [1, 2, 3]" :key="count" :value="String(count)">{{ count }}</SelectItem></SelectContent>
      </Select>
      <label class="matchmaking-toggle"><Checkbox v-model="draft.is_campaign_bot_squad_mirroring_enabled" :disabled="isBusy || isDisabled" @update:model-value="save" /><span>{{ $t('USE SQUADS FOR BOTS') }}<ConfigHelp text="Use your other unlocked PvE squads as matchmaking bots." /></span></label>
      <p>{{ $t('Copy unused unlocked PvE squads into matchmaking bots.') }}</p>
    </template>
    <p v-if="error" role="alert">{{ $t(error) }}</p>
    <Button v-if="error || !draft" variant="outline" :disabled="isBusy || isDisabled" @click="reload">{{ $t('RELOAD') }}</Button>
  </section>
</template>

<style scoped>
.matchmaking-editor { display:flex; flex-direction:column; gap:14px; }
.matchmaking-editor h3 { font-size:16px; }
.matchmaking-editor label { font-size:11px; font-weight:600; }
.matchmaking-toggle { display:flex; align-items:center; gap:10px; }
.matchmaking-editor p { color:var(--muted-foreground); font-size:12px; }
.matchmaking-editor [role="alert"] { color:var(--destructive); }
.matchmaking-editor button { align-self:flex-start; }
</style>
