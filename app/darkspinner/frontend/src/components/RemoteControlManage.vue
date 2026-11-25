<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RefreshCw } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import ConfigHelp from './ConfigHelp.vue'
import RemoteControlUpdate from './RemoteControlUpdate.vue'
import RemoteControlMaintenance from './RemoteControlMaintenance.vue'
import RemoteControlHealth from './RemoteControlHealth.vue'
import { maintenanceActionLabel, maintenancePhaseLabel } from '../control_panel'
import { formatControlTime } from '../control_time'

const props = defineProps({ profile:Object, panel:Object })
const emit = defineEmits(['busy', 'invalidated', 'maintenance'])
const updateView = ref(null)
const maintenanceView = ref(null)
const healthView = ref(null)
const update = ref(null)
const maintenance = ref(null)
const health = ref(null)
const isUpdateBusy = ref(false)
const isMaintenanceBusy = ref(false)
const isHealthBusy = ref(false)
const operations = computed(() => props.panel.server.supported_operations || [])
const isBusy = computed(() => isUpdateBusy.value || isMaintenanceBusy.value || isHealthBusy.value)
const updateStatus = computed(() => {
  if (!operations.value.includes('admin.update.check')) return 'UPDATE CHECK UNSUPPORTED'
  if (!update.value) return 'UNKNOWN'
  if (update.value.is_check_supported !== true) return 'UPDATE CHECK UNSUPPORTED'
  if (update.value?.is_check_in_progress) return 'CHECKING FOR UPDATES'
  return { checking:'CHECKING FOR UPDATES', available:'UPDATE AVAILABLE', current:'UP TO DATE', failed:'UPDATE CHECK FAILED' }[update.value?.check_status] || 'NOT CHECKED'
})
const gameplayStatus = computed(() => !maintenance.value ? 'UNKNOWN' : maintenance.value.is_admission_blocked ? 'BLOCKED' : 'ALLOWED')
const healthStatus = computed(() => ({ healthy:'HEALTHY', degraded:'DEGRADED' }[health.value?.status] || 'UNKNOWN'))
watch(isBusy, next => emit('busy', next), { flush:'sync' })
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  update.value = null
  maintenance.value = null
  health.value = null
}, { flush:'sync' })
function refresh() {
  if (isBusy.value) return
  updateView.value?.reload()
  maintenanceView.value?.reload()
  healthView.value?.reload()
}
function acceptMaintenance(observation) {
  maintenance.value = observation
  emit('maintenance', observation)
}
onBeforeUnmount(() => emit('busy', false))
</script>

<template>
  <section class="manage-overview">
    <header class="manage-heading">
      <h3>{{ $t('SERVER STATUS') }}</h3>
      <Button variant="outline" size="icon" :disabled="isBusy" :aria-label="$t('REFRESH')" :title="$t('REFRESH')" @click="refresh"><RefreshCw :size="16" aria-hidden="true" /></Button>
    </header>
    <div class="manage-statuses">
      <article v-if="operations.includes('admin.update.read')" class="manage-status manage-update" :data-status="update?.check_status">
        <h4>{{ $t('UPDATE') }}</h4>
        <div class="update-check">
          <Button variant="outline" :disabled="isUpdateBusy || !operations.includes('admin.update.check') || update?.is_check_supported !== true || update?.is_check_in_progress || maintenance?.is_active" @click="updateView?.check()">{{ $t('CHECK FOR UPDATES') }}</Button>
          <strong role="status">{{ $t(updateStatus) }}</strong>
        </div>
        <p v-if="!operations.includes('admin.update.check') || (update && update.is_check_supported !== true)">{{ $t('This deployment does not support update checks.') }}</p>
        <p v-if="update?.candidate_version">{{ $t('CANDIDATE VERSION') }}: {{ update.candidate_version }}</p>
        <p v-if="update?.checked_at">{{ formatControlTime(update.checked_at) }}</p>
      </article>
      <article v-if="operations.includes('admin.maintenance.read')" class="manage-status" :data-status="maintenance?.is_admission_blocked ? 'blocked' : ''">
        <h4>{{ $t('NEW GAME ADMISSION') }}</h4>
        <strong role="status">{{ $t(gameplayStatus) }}</strong>
        <template v-if="maintenance?.is_active">
          <p>{{ $t(maintenanceActionLabel(maintenance.action)) }} · {{ $t(maintenancePhaseLabel(maintenance.phase)) }}</p>
          <p>{{ $t('LATEST DEADLINE') }}: {{ formatControlTime(maintenance.deadline) }}</p>
        </template>
      </article>
      <article v-if="operations.includes('admin.health.read')" class="manage-status" :data-status="health?.status">
        <h4>{{ $t('SERVER HEALTH') }}<ConfigHelp text="SERVER HEALTH">{{ $t('Recent save, storage, and backup status. Unknown means there is not enough recent information.') }}</ConfigHelp></h4>
        <strong role="status">{{ $t(healthStatus) }}</strong>
        <p v-if="health?.observed_at">{{ formatControlTime(health.observed_at) }}</p>
      </article>
    </div>
    <div v-if="operations.includes('admin.maintenance.read')" class="manage-operations">
    <RemoteControlMaintenance ref="maintenanceView" :profile="profile" :panel="panel" @busy="isMaintenanceBusy = $event" @invalidated="emit('invalidated', $event)" @observation="maintenance = $event" @maintenance="acceptMaintenance" />
    </div>
    <div class="manage-details">
      <RemoteControlUpdate v-if="operations.includes('admin.update.read')" ref="updateView" :profile="profile" :panel="panel" @busy="isUpdateBusy = $event" @observation="update = $event" @invalidated="emit('invalidated', $event)" />
      <RemoteControlHealth v-if="operations.includes('admin.health.read')" ref="healthView" :profile="profile" :panel="panel" @busy="isHealthBusy = $event" @observation="health = $event" @invalidated="emit('invalidated', $event)" />
    </div>
  </section>
</template>

<style scoped>
.manage-overview { display:flex; flex-direction:column; gap:18px; container-type:inline-size; }
.manage-heading { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.manage-statuses { display:grid; grid-template-columns:minmax(0,2fr) repeat(2,minmax(0,1fr)); gap:12px; }
.manage-status { display:flex; flex-direction:column; align-items:flex-start; gap:10px; min-width:0; padding:16px; border:1px solid var(--border); border-radius:8px; background:var(--background); }
.manage-status h4 { color:var(--muted-foreground); font-size:11px; }
.manage-status strong { font-size:14px; }
.update-check { display:flex; align-items:center; flex-wrap:wrap; gap:12px; width:100%; }
.update-check > button { flex-shrink:0; max-width:100%; min-height:40px; height:auto; white-space:normal; }
.update-check > strong { flex:1 0 100px; max-width:100%; font-size:12px; overflow-wrap:normal; word-break:normal; }
.manage-status p { font-size:12px; color:var(--muted-foreground); overflow-wrap:anywhere; }
.manage-status[data-status="available"] strong, .manage-status[data-status="healthy"] strong { color:var(--primary); }
.manage-status[data-status="failed"] strong, .manage-status[data-status="degraded"] strong { color:var(--destructive); }
.manage-operations, .manage-details { padding-top:18px; border-top:1px solid var(--border); }
.manage-details { display:flex; flex-direction:column; gap:16px; }
@container (max-width:740px) {
  .manage-statuses { grid-template-columns:repeat(2,minmax(0,1fr)); }
  .manage-update { grid-column:1 / -1; }
}
@container (max-width:440px) {
  .manage-statuses { grid-template-columns:minmax(0,1fr); }
}
</style>
