<script setup>
import { computed } from 'vue'
import { TooltipArrow, TooltipContent, TooltipPortal, TooltipProvider, TooltipRoot, TooltipTrigger } from 'reka-ui'
import { translate } from '../localization'
import { maintenancePhaseLabel } from '../control_panel'
import { formatControlTime } from '../control_time'

const props = defineProps({
  connection: { type:Object, default:null },
  maintenance: { type:Object, default:null },
  isChecking: { type:Boolean, default:false },
  clientVersion: { type:String, default:'' },
})
const versionLabel = computed(() => {
  const version = props.connection?.serverVersion?.trim() || ''
  const parts = /^v?(\d+\.\d+\.\d+)(?:-([^+]+))?(?:\+.*)?$/.exec(version)
  if (!parts) return version || translate('Unknown version')
  return parts[1] + (parts[2]?.split('.')[0] === 'unstable' ? ` ${translate('(Unstable)')}` : '')
})
const maintenanceTiming = computed(() => {
  const phase = props.maintenance?.phase
  const deadline = props.maintenance?.deadline
  return [
    phase ? translate(maintenancePhaseLabel(phase)) : '',
    deadline && !deadline.startsWith('0001-') ? formatControlTime(deadline) : '',
  ].filter(Boolean).join(' · ')
})
</script>

<template>
  <span class="remote-connection" role="status"
    :class="{ online:connection?.isOnline, mismatch:connection?.isOnline && !connection?.isVersionMatched }">
    <TooltipProvider v-if="maintenance" :delay-duration="250">
      <TooltipRoot>
        <TooltipTrigger as-child>
          <span class="connection-state maintenance" tabindex="0"><span class="connection-dot" aria-hidden="true"></span>{{ $t('Maintenance') }}</span>
        </TooltipTrigger>
        <TooltipPortal>
          <TooltipContent class="server-maintenance-tooltip" side="top" :side-offset="8" :collision-padding="12">
            <p v-if="maintenance.reason">{{ maintenance.reason }}</p>
            <p v-if="maintenanceTiming">{{ maintenanceTiming }}</p>
            <p>{{ $t('New game sessions are currently blocked on this server.') }}</p>
            <TooltipArrow class="server-maintenance-tooltip-arrow" :width="10" :height="5" />
          </TooltipContent>
        </TooltipPortal>
      </TooltipRoot>
    </TooltipProvider>
    <span v-else class="connection-state"
      :title="$t('Client {0} vs Server {1}', [connection?.clientVersion || clientVersion || $t('unknown'), connection?.serverVersion || $t('unknown')]) + (connection?.error ? '\n' + connection.error : '')">
      <span class="connection-dot" aria-hidden="true"></span>
      <template v-if="connection?.isOnline && !isChecking">{{ versionLabel }}<template v-if="!connection.isVersionMatched"> {{ $t('Out of Date') }}</template></template>
      <template v-else>{{ $t(isChecking ? 'Checking…' : !connection ? 'Not checked' : 'Offline / Unreachable') }}</template>
    </span>
  </span>
</template>

<style scoped>
.remote-connection { font-size:12px; overflow-wrap:anywhere; }
.connection-state { display:flex; align-items:center; gap:7px; color:#a5abb5; }
.connection-dot { width:8px; height:8px; flex-shrink:0; border-radius:50%; background:currentColor; }
.online .connection-state { color:#89d5b1; }
.mismatch .connection-state { color:#f87171; }
.connection-state.maintenance { display:inline-flex; color:#fbbf24; cursor:help; }
:global(.server-maintenance-tooltip) { z-index:150; width:max-content; max-width:min(300px, calc(100vw - 24px), var(--reka-tooltip-content-available-width)); padding:10px 12px; border:1px solid var(--border); border-radius:8px; background:var(--popover, #18181b); color:var(--popover-foreground, #fafafa); box-shadow:0 6px 20px #0006; font-size:12px; line-height:1.5; overflow-wrap:anywhere; }
:global(.server-maintenance-tooltip p + p) { margin-top:8px; }
:global(.server-maintenance-tooltip-arrow) { fill:var(--popover, #18181b); }
</style>
