<script setup>
import { RefreshCw } from '@lucide/vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import ConfigHelp from './ConfigHelp.vue'
import { GetRemoteControlHealth } from '../platform'
import { formatControlTime } from '../control_time'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, panel:Object })
const emit = defineEmits(['busy', 'invalidated', 'observation'])
const health = ref(null)
const error = ref('')
const isBusy = ref(false)
const operations = computed(() => props.panel.server.supported_operations || [])
let generation = 0
let isDisposed = false
watch(isBusy, next => emit('busy', next), { flush:'sync' })
watch(health, next => emit('observation', next), { flush:'sync' })
function isCurrent(current, profile) {
  return !isDisposed && current === generation && props.profile?.serverAddress === profile?.serverAddress && props.profile?.loginName === profile?.loginName
}

function failed(failure) {
  error.value = panelError(failure)
  if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
}

async function refreshHealth() {
  if (!operations.value.includes('admin.health.read') || isBusy.value || document.visibilityState === 'hidden') return
  const current = generation
  const profile = props.profile
  isBusy.value = true
  try {
    const result = await GetRemoteControlHealth(profile.serverAddress, profile.loginName)
    if (isCurrent(current, profile)) { health.value = result; error.value = '' }
  } catch (failure) { if (isCurrent(current, profile)) { health.value = null; failed(failure) } }
  finally { if (isCurrent(current, profile)) isBusy.value = false }
}

function statusLabel(status) {
  if (status === 'completed') return 'COMPLETED'
  if (status === 'failed') return 'FAILED'
  if (status === 'available') return 'AVAILABLE'
  if (status === 'missing') return 'MISSING'
  if (status === 'not_verified') return 'NOT VERIFIED'
  if (status === 'recovered_healthy') return 'RECOVERED AND HEALTHY'
  if (status === 'recorded_at_creation') return 'CHECKSUM RECORDED AT CREATION'
  if (status === 'mismatch') return 'FILE METADATA MISMATCH'
  if (status === 'healthy') return 'HEALTHY'
  if (status === 'restored') return 'RESTORED'
  if (status === 'degraded') return 'DEGRADED'
  return 'UNKNOWN'
}

function backupAgeParts(seconds) {
  return [Math.floor(seconds / 86400), Math.floor(seconds % 86400 / 3600), Math.floor(seconds % 3600 / 60)]
}

function displayDate(timestamp, isDaily = false) { return formatControlTime(timestamp, isDaily) }

watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  generation++
  health.value = null
  error.value = ''
  isBusy.value = false
  void refreshHealth()
}, { immediate:true, flush:'sync' })
const timer = window.setInterval(refreshHealth, 30000)
onBeforeUnmount(() => { isDisposed = true; window.clearInterval(timer); emit('busy', false); health.value = null })
defineExpose({ reload:refreshHealth })
</script>

<template>
    <section class="health-view" v-if="operations.includes('admin.health.read')">
      <details class="manage-detail">
        <summary>{{ $t('HEALTH DETAILS') }}</summary>
      <dl v-if="health">
        <dt>{{ $t('STATUS') }}</dt><dd>{{ $t(statusLabel(health.status)) }}</dd>
        <dt>{{ $t('OBSERVED AT') }}</dt><dd>{{ $t(displayDate(health.observed_at)) }}</dd>
        <dt>{{ $t('DATABASE') }}</dt><dd>{{ $t(statusLabel(health.database.status)) }}</dd>
        <dt>{{ $t('DATABASE OBSERVED AT') }}</dt><dd>{{ $t(displayDate(health.database.observed_at)) }}</dd>
        <dt>{{ $t('LAST PROFILE SAVE') }}</dt><dd>{{ $t(displayDate(health.profile_saves.last_success_at)) }}</dd>
        <dt>{{ $t('LAST SAVE FAILURE') }}</dt><dd>{{ $t(displayDate(health.profile_saves.last_failure_at)) }}</dd>
        <dt>{{ $t('PENDING ACCOUNT SAVES') }}</dt><dd>{{ health.profile_saves.pending_accounts }}</dd>
        <dt>{{ $t('ACCOUNTS WITH SAVE FAILURES') }}</dt><dd>{{ health.profile_saves.failed_accounts }}</dd>
        <dt>{{ $t('UNRESOLVED SAVE FAILURE') }}</dt><dd>{{ $t(health.profile_saves.is_pending_failure ? 'YES' : 'NO') }}</dd>
        <dt>{{ $t('RUNTIME STORAGE') }}</dt><dd>{{ $t(statusLabel(health.storage.status)) }}</dd>
        <dt>{{ $t('STORAGE OBSERVED AT') }}</dt><dd>{{ $t(displayDate(health.storage.observed_at)) }}</dd>
        <template v-if="health.storage.total_bytes != null && health.storage.available_bytes != null">
          <dt>{{ $t('TOTAL STORAGE (BYTES)') }}</dt><dd>{{ health.storage.total_bytes }}</dd>
          <dt>{{ $t('AVAILABLE STORAGE (BYTES)') }}</dt><dd>{{ health.storage.available_bytes }}</dd>
        </template>
        <dt>{{ $t('LAST RECORDED BACKUP STATUS') }}</dt><dd>{{ $t(statusLabel(health.backup.status)) }}</dd>
        <dt>{{ $t('BACKUP STORAGE') }}</dt><dd>{{ $t(statusLabel(health.backup.storage?.status)) }}</dd>
        <dt>{{ $t('BACKUP STORAGE OBSERVED AT') }}</dt><dd>{{ $t(displayDate(health.backup.storage?.observed_at)) }}</dd>
        <template v-if="health.backup.storage?.total_bytes != null && health.backup.storage?.available_bytes != null">
          <dt>{{ $t('BACKUP TOTAL STORAGE (BYTES)') }}</dt><dd>{{ health.backup.storage.total_bytes }}</dd>
          <dt>{{ $t('BACKUP AVAILABLE STORAGE (BYTES)') }}</dt><dd>{{ health.backup.storage.available_bytes }}</dd>
        </template>
        <dt>{{ $t('BACKUP OBSERVED AT') }}</dt><dd>{{ $t(displayDate(health.backup.observed_at)) }}</dd>
        <dt>{{ $t('LAST RECORDED BACKUP ATTEMPT') }}</dt><dd>{{ $t(displayDate(health.backup.last_attempt_at)) }}</dd>
        <dt>{{ $t('LAST RECORDED BACKUP FAILURE') }}</dt><dd>{{ $t(displayDate(health.backup.last_failure_at)) }}</dd>
        <template v-if="health.backup.last_success">
          <dt>{{ $t('BACKUP ID') }}</dt><dd>{{ health.backup.last_success.id }}</dd>
          <dt>{{ $t('BACKUP STARTED AT') }}</dt><dd>{{ $t(displayDate(health.backup.last_success.started_at)) }}</dd>
          <dt>{{ $t('BACKUP COMPLETED AT') }}</dt><dd>{{ $t(displayDate(health.backup.last_success.completed_at)) }}</dd>
          <dt>{{ $t('BACKUP BUILD') }}</dt><dd>{{ health.backup.last_success.build_id }}</dd>
          <dt>{{ $t('BACKUP AGE') }}</dt><dd>{{ $t('{0} days, {1} hours, {2} minutes', backupAgeParts(health.backup.last_success.age_seconds)) }}</dd>
          <dt>{{ $t('BACKUP ARCHIVE') }}</dt><dd>{{ $t(statusLabel(health.backup.archive_status)) }}</dd>
          <dt>{{ $t('BACKUP ARCHIVE OBSERVED AT') }}</dt><dd>{{ $t(displayDate(health.backup.archive_observed_at)) }}</dd>
          <dt>{{ $t('BACKUP INTEGRITY') }}</dt><dd>{{ $t(statusLabel(health.backup.integrity_status)) }}</dd>
          <dt>{{ $t('RESTORE VERIFICATION') }}<ConfigHelp text="RESTORE VERIFICATION">{{ $t('Whether this backup was restored and the server started successfully afterward.') }}</ConfigHelp></dt><dd>{{ $t(statusLabel(health.backup.restore_status)) }}</dd>
        </template>
      </dl>
      <p v-else>{{ $t('Health observations are unavailable.') }}</p>
      <div v-if="health" class="last-restore">
        <h4>{{ $t('LAST RESTORE') }}<ConfigHelp text="LAST RESTORE">{{ $t('The most recent restore attempt. Healthy means the restored server passed its startup checks.') }}</ConfigHelp></h4>
        <dl>
          <dt>{{ $t('STATUS') }}</dt><dd>{{ $t(statusLabel(health.backup.recovery?.status)) }}</dd>
          <template v-if="health.backup.recovery?.backup_id">
            <dt>{{ $t('BACKUP ID') }}</dt><dd>{{ health.backup.recovery.backup_id }}</dd>
          </template>
          <template v-if="displayDate(health.backup.recovery?.started_at) !== 'UNKNOWN'">
            <dt>{{ $t('RESTORE STARTED AT') }}</dt><dd>{{ displayDate(health.backup.recovery.started_at) }}</dd>
          </template>
          <template v-if="displayDate(health.backup.recovery?.updated_at) !== 'UNKNOWN'">
            <dt>{{ $t('RESTORE UPDATED AT') }}</dt><dd>{{ displayDate(health.backup.recovery.updated_at) }}</dd>
          </template>
          <template v-if="displayDate(health.backup.recovery?.restored_at) !== 'UNKNOWN'">
            <dt>{{ $t('RESTORED AT') }}</dt><dd>{{ displayDate(health.backup.recovery.restored_at) }}</dd>
          </template>
          <template v-if="displayDate(health.backup.recovery?.healthy_at) !== 'UNKNOWN'">
            <dt>{{ $t('RESTORE HEALTH CONFIRMED AT') }}</dt><dd>{{ displayDate(health.backup.recovery.healthy_at) }}</dd>
          </template>
          <template v-if="health.backup.recovery?.failure_code === 'restore_failed'">
            <dt>{{ $t('RESTORE FAILURE') }}</dt><dd>{{ $t('RESTORE FAILED') }}</dd>
          </template>
        </dl>
      </div>
      </details>
    <p v-if="error" role="alert">{{ $t(error) }}</p>
  </section>
</template>

<style scoped>
.health-view { display:flex; flex-direction:column; gap:14px; }
.manage-detail > summary { cursor:pointer; font-size:12px; font-weight:600; }
.manage-detail[open] > summary { margin-bottom:14px; }
.last-restore { display:flex; flex-direction:column; gap:10px; }
.health-view dl { display:grid; grid-template-columns:minmax(140px,1fr) 2fr; gap:10px; }
.health-view dd { overflow-wrap:anywhere; }
.health-view p { color:var(--muted-foreground); font-size:12px; }
</style>
