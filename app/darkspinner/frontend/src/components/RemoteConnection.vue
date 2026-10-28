<script setup>
import { computed } from 'vue'

const props = defineProps({
  connection: { type:Object, default:null },
  isChecking: { type:Boolean, default:false },
  clientVersion: { type:String, default:'' },
})
const versionLabel = computed(() => {
  const version = props.connection?.serverVersion?.trim() || ''
  const parts = /^v?(\d+\.\d+\.\d+)(?:-([^+]+))?(?:\+.*)?$/.exec(version)
  if (!parts) return version || 'Unknown version'
  return parts[1] + (parts[2]?.split('.')[0] === 'unstable' ? ' (Unstable)' : '')
})
</script>

<template>
  <div class="remote-connection" role="status" aria-live="polite"
    :class="{ online:connection?.isOnline, mismatch:connection?.isOnline && !connection?.isVersionMatched }"
    :title="`Client ${connection?.clientVersion || clientVersion || 'unknown'} vs Server ${connection?.serverVersion || 'unknown'}${connection?.error ? '\n' + connection.error : ''}`">
    <span class="connection-state"><span class="connection-dot" aria-hidden="true"></span>{{ isChecking ? 'Checking…' : !connection ? 'Not checked' : connection.isOnline ? versionLabel + (connection.isVersionMatched ? '' : ' Out of Date') : 'Offline / Unreachable' }}</span>
  </div>
</template>

<style scoped>
.remote-connection { font-size:12px; overflow-wrap:anywhere; }
.connection-state { display:flex; align-items:center; gap:7px; color:#a5abb5; }
.connection-dot { width:8px; height:8px; flex-shrink:0; border-radius:50%; background:currentColor; }
.online .connection-state { color:#89d5b1; }
.mismatch .connection-state { color:#f87171; }
</style>
