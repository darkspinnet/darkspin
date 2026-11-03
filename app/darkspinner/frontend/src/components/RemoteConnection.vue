<script setup>
import { computed } from 'vue'
import { translate } from '../localization'

const props = defineProps({
  connection: { type:Object, default:null },
  isChecking: { type:Boolean, default:false },
  clientVersion: { type:String, default:'' },
})
const versionLabel = computed(() => {
  const version = props.connection?.serverVersion?.trim() || ''
  const parts = /^v?(\d+\.\d+\.\d+)(?:-([^+]+))?(?:\+.*)?$/.exec(version)
  if (!parts) return version || translate('Unknown version')
  return parts[1] + (parts[2]?.split('.')[0] === 'unstable' ? ` ${translate('(Unstable)')}` : '')
})
</script>

<template>
  <div class="remote-connection" role="status" aria-live="polite"
    :class="{ online:connection?.isOnline, mismatch:connection?.isOnline && !connection?.isVersionMatched, maintenance:connection?.isOnline && connection?.isMaintenance }"
    :title="$t('Client {0} vs Server {1}', [connection?.clientVersion || clientVersion || $t('unknown'), connection?.serverVersion || $t('unknown')]) + (connection?.error ? '\n' + connection.error : '')">
    <span class="connection-state"><span class="connection-dot" aria-hidden="true"></span><template v-if="connection?.isOnline && !isChecking"><template v-if="connection.isMaintenance">{{ $t('Maintenance') }} · </template>{{ versionLabel }}<template v-if="!connection.isVersionMatched"> {{ $t('Out of Date') }}</template></template><template v-else>{{ $t(isChecking ? 'Checking…' : !connection ? 'Not checked' : 'Offline / Unreachable') }}</template></span>
    <div v-if="connection?.isOnline && connection?.isMaintenance && !isChecking" class="maintenance-reason">{{ connection.maintenanceReason || $t('The server is undergoing maintenance. Try again later.') }}</div>
  </div>
</template>

<style scoped>
.remote-connection { font-size:12px; overflow-wrap:anywhere; }
.connection-state { display:flex; align-items:center; gap:7px; color:#a5abb5; }
.connection-dot { width:8px; height:8px; flex-shrink:0; border-radius:50%; background:currentColor; }
.online .connection-state { color:#89d5b1; }
.mismatch .connection-state { color:#f87171; }
.maintenance .connection-state, .maintenance-reason { color:#fbbf24; }
.maintenance-reason { margin-top:4px; }
</style>
