<script setup>
import { formatControlTime } from '../control_time'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import LauncherDialog from './LauncherDialog.vue'

import RemoteControlAdmin from './RemoteControlAdmin.vue'
import RemoteControlActivity from './RemoteControlActivity.vue'
import RemoteControlPersonalActivity from './RemoteControlPersonalActivity.vue'
import RemoteControlFeatures from './RemoteControlFeatures.vue'
import { OpenLocalControlPanel, OpenRemoteControlPanel, GetRemoteControlPanel, CloseRemoteControlPanel } from '../platform'
import { panelError, maintenancePhaseLabel, maintenanceActionLabel } from '../control_panel'

const props = defineProps({ profile:Object, password:String, isLocal:{ type:Boolean, default:false } })
const emit = defineEmits(['capabilities', 'password', 'server-name', 'maintenance', 'interact', 'renamed'])
const panel = ref(null)
const maintenance = ref(null)
const page = ref('')
const isBusy = ref(false)
const isActionBusy = ref(false)
const isPromptOpen = ref(false)
const enteredPassword = ref('')
const error = ref('')
let generation = 0
const isAdmin = computed(() => !!panel.value?.capabilities?.is_admin)
const isPlayerReviewAvailable = computed(() => isAdmin.value && ['admin.users.list', 'admin.users.activity'].every(operation => panel.value?.server?.supported_operations?.includes(operation)))
const effectiveProfile = computed(() => panel.value ? { ...props.profile, serverAddress:panel.value.server_address, loginName:panel.value.profile.login_name } : props.profile)
function isSelectedPanel(result, profile, isLocal = props.isLocal) {
  return !!profile && result?.profile?.login_name === profile.loginName && (isLocal || result?.server_address === profile.serverAddress)
}
function setMaintenance(result) {
  maintenance.value = result || null
  if (panel.value) panel.value.server.maintenance = result
  emit('maintenance', result || null)
}
function acceptPanel(result) {
  panel.value = result
  setMaintenance(result.server.maintenance)
}
function displayDate(timestamp, isDaily = false) { return formatControlTime(timestamp, isDaily) }
async function serverChanged() {
  await nextTick()
  await refresh()
}
watch(isAdmin, next => emit('capabilities', next))
watch(() => panel.value?.server?.name, next => emit('server-name', next || ''))

async function signIn() {
  const current = ++generation
  const profile = props.profile
  const isLocal = props.isLocal
  if (!profile) return
  isBusy.value = true
  error.value = ''
  try {
    const result = isLocal
      ? await OpenLocalControlPanel(profile.loginName)
      : await OpenRemoteControlPanel(profile.serverAddress, profile.loginName, enteredPassword.value || props.password || '')
    if (current !== generation || isLocal !== props.isLocal || !isSelectedPanel(result, props.profile) || !isSelectedPanel(result, profile, isLocal)) return
    acceptPanel(result)
    if (enteredPassword.value) emit('password', enteredPassword.value)
    enteredPassword.value = ''
    isPromptOpen.value = false
  } catch (failure) {
    if (current !== generation) return
    panel.value = null
    error.value = panelError(failure)
    isPromptOpen.value = !isLocal
  } finally {
    if (current === generation) isBusy.value = false
  }
}

function open(nextPage = 'features') {
  page.value = nextPage
  error.value = ''
  if (panel.value) { void refresh(); return }
  if (props.isLocal || props.profile?.isPasswordRemembered || props.password) void signIn()
  else isPromptOpen.value = true
}

function close() {
  generation++
  page.value = ''
  panel.value = null
  // Emit synchronously: component watchers are stopped during unmount.
  emit('capabilities', false)
  emit('server-name', '')
  enteredPassword.value = ''
  isPromptOpen.value = false
  isBusy.value = false
  isActionBusy.value = false
  error.value = ''
  void CloseRemoteControlPanel().catch(() => {})
}

async function refresh() {
  if (!page.value || !panel.value || isBusy.value || isActionBusy.value || document.visibilityState === 'hidden') return
  const current = generation
  isBusy.value = true
  try {
    const profile = effectiveProfile.value
    const result = await GetRemoteControlPanel(profile.serverAddress, profile.loginName)
    if (current === generation && isSelectedPanel(result, props.profile) && isSelectedPanel(result, profile, false)) acceptPanel(result)
  } catch (failure) {
    if (current !== generation) return
    panel.value = null
    error.value = panelError(failure)
  } finally {
    if (current === generation) isBusy.value = false
  }
}

function invalidated(message) {
  generation++
  panel.value = null
  emit('capabilities', false)
  error.value = message
}
function accountRenamed(result) {
  if (panel.value) panel.value.profile.display_name = result.display_name
  emit('renamed')
}

watch(() => `${props.isLocal}/${props.profile?.serverAddress || ''}/${props.profile?.loginName || ''}`, () => {
  close()
  setMaintenance(null)
  // Panel authentication and projections wait until the player opens the panel.
}, { immediate:true, flush:'sync' })
onBeforeUnmount(close)
defineExpose({ open })
</script>

<template>
  <aside v-if="!page && maintenance?.is_admission_blocked" class="control-welcome" role="status"><strong>{{ $t('SERVER MAINTENANCE') }}</strong><p class="control-server-copy">{{ maintenance.reason }}</p><p>{{ $t(maintenancePhaseLabel(maintenance.phase)) }} · {{ $t(displayDate(maintenance.deadline)) }}</p><p>{{ $t('New game sessions are currently blocked on this server.') }}</p></aside>
  <aside v-if="!page && panel?.server?.welcome" class="control-welcome"><strong>{{ panel.server.name }}</strong><p class="control-server-copy">{{ panel.server.welcome }}</p></aside>
  <LauncherDialog v-if="page" :is-dismissible="true" :width="isPromptOpen && !isLocal ? null : 1120" :height="isPromptOpen && !isLocal ? null : 720" @close="close" @interact="emit('interact', $event)">
    <Tabs v-if="!isPromptOpen || isLocal" v-model="page" as="section" class="control-page" :aria-label="$t('Server control panel')">
      <header>
        <DialogTitle class="sr-only">{{ $t('Server control panel') }}</DialogTitle>
        <TabsList class="control-tabs" :aria-label="$t('Control panel pages')">
          <TabsTrigger value="features" :disabled="isBusy || isActionBusy">{{ $t('FEATURE') }}</TabsTrigger>
          <TabsTrigger value="dashboard" :disabled="isBusy || isActionBusy">{{ $t('DASHBOARD') }}</TabsTrigger>
          <TabsTrigger v-if="isAdmin" value="admin" :disabled="isBusy || isActionBusy">{{ $t('ADMIN') }}</TabsTrigger>
        </TabsList>
      </header>
      <div class="control-body">
      <p v-if="error" role="alert">{{ $t(error) }}</p>
      <p v-if="isBusy && !panel">{{ $t('SIGNING IN...') }}</p>
      <template v-if="panel">
        <TabsContent v-if="page === 'features'" value="features" class="control-content">
          <RemoteControlFeatures :profile="effectiveProfile" :panel="panel" @busy="isActionBusy = $event" @renamed="accountRenamed" @invalidated="invalidated" />
        </TabsContent>
        <TabsContent v-else-if="page === 'dashboard'" value="dashboard" class="control-content">
          <RemoteControlActivity :is-player-review-available="isPlayerReviewAvailable" v-if="panel.server.supported_operations?.includes('server.activity.read')" :profile="effectiveProfile" :is-level-available="panel.server.supported_operations.includes('server.activity.level.read')" :is-dna-available="panel.server.supported_operations.includes('server.activity.dna.read')" :is-progression-available="panel.server.supported_operations.includes('server.activity.progression.read')" :is-personal-available="panel.server.supported_operations.includes('account.activity.read')" @busy="isActionBusy = $event" @invalidated="invalidated" />
          <RemoteControlPersonalActivity v-else-if="panel.server.supported_operations?.includes('account.activity.read')" :profile="effectiveProfile" :is-range-selectable="true" @busy="isActionBusy = $event" @invalidated="invalidated" />
        </TabsContent>
        <TabsContent v-else-if="page === 'admin' && isAdmin" value="admin" class="control-content">
          <RemoteControlAdmin :profile="effectiveProfile" :panel="panel" @busy="isActionBusy = $event" @invalidated="invalidated" @changed="serverChanged" @maintenance="setMaintenance" />
        </TabsContent>
      </template>
      <Button v-else-if="!isBusy" variant="outline" @click="isLocal ? signIn() : isPromptOpen = true">{{ $t('SIGN IN') }}</Button>
      </div>
    </Tabs>
    <form v-else class="notice-card control-sign-in" @submit.prevent="signIn">
      <DialogTitle>{{ $t('SERVER CONTROL PANEL SIGN IN') }}</DialogTitle>
      <p>{{ profile?.displayName }} · {{ profile?.serverAddress }}</p>
      <label for="control-password">{{ $t('PASSWORD') }}</label>
      <Input id="control-password" v-model="enteredPassword" type="password" autocomplete="current-password" :disabled="isBusy" />
      <p v-if="error" role="alert">{{ $t(error) }}</p>
      <div class="control-sign-in-actions">
        <Button type="button" variant="outline" :disabled="isBusy" @click="close">{{ $t('CANCEL / BACK') }}</Button>
        <Button type="submit" :disabled="isBusy || (!enteredPassword && !profile?.isPasswordRemembered && !password)">{{ $t('SIGN IN') }}</Button>
      </div>
    </form>
  </LauncherDialog>
</template>

<style scoped>
.control-page { display:grid; grid-template-rows:40px minmax(0,1fr); gap:18px; min-width:0; min-height:0; height:100%; }
.control-page > header { height:40px; min-height:40px; max-height:40px; min-width:0; }
.control-tabs { max-width:100%; height:40px; min-height:40px; max-height:40px; overflow-x:auto; overflow-y:hidden; justify-content:flex-start; }
.control-tabs :deep([data-slot="tabs-trigger"]) { flex:0 0 auto; height:40px; min-height:40px; max-height:40px; }
.control-body { min-width:0; min-height:0; overflow-y:auto; scrollbar-gutter:stable; }
.control-content { min-width:0; }
.control-server-copy { white-space:pre-wrap; overflow-wrap:anywhere; margin:12px 0; }
.control-welcome { padding:14px 18px; border:1px solid var(--border); border-radius:8px; background:var(--card); font-size:12px; }
.control-content p { color:var(--muted-foreground); }
.control-sign-in { display:flex; flex-direction:column; gap:12px; }
.control-sign-in-actions { display:flex; justify-content:space-between; gap:12px; }
</style>
