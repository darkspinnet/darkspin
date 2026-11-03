<script setup>
import { formatControlTime } from '../control_time'
import { computed } from 'vue'
import { translate } from '../localization'
import ConfigHelp from './ConfigHelp.vue'

const props = defineProps({ activity:{ type:Object, required:true } })
const states = [
  { field:'pve_count', label:'PvE', color:'#4ade80' },
  { field:'pvp_count', label:'PvP', color:'#fb923c' },
  { field:'lobby_count', label:'Lobby', color:'#60a5fa' },
  { field:'matchmaking_count', label:'Matchmaking', color:'#c084fc' },
  { field:'other_count', label:'Other', color:'#a1a1aa' }
]
const isDaily = computed(() => props.activity.bucket_seconds >= 86400)
const title = computed(() => isDaily.value ? 'DAILY AVERAGE PLAYER STATE DISTRIBUTION' : 'HOURLY PLAYER STATE DISTRIBUTION')
function date(timestamp, isDaily = false) { return formatControlTime(timestamp, isDaily) }
function number(count) { return count.toLocaleString(undefined, { maximumFractionDigits:2 }) }
const chart = computed(() => {
  const cadence = Number(props.activity.bucket_seconds) * 1000
  const rangeStart = Date.parse(props.activity.range_start)
  const start = Math.floor(rangeStart / cadence) * cadence
  const end = Date.parse(props.activity.observed_at)
  const emptyChart = { points:[], maximum:1, start:'', end:'' }
  if (![3600000, 86400000].includes(cadence) || !Number.isFinite(start) || !Number.isFinite(end) || end < start) return emptyChart
  const samples = (Array.isArray(props.activity.distribution_samples) ? props.activity.distribution_samples : [])
    .filter(sample => sample && Number.isFinite(Date.parse(sample.at)) && Date.parse(sample.at) >= start && Date.parse(sample.at) <= end && Number.isSafeInteger(sample.observation_count) && sample.observation_count > 0 && states.every(state => Number.isFinite(sample[state.field]) && sample[state.field] >= 0))
    .slice().sort((first, second) => Date.parse(first.at) - Date.parse(second.at))
  const maximum = Math.max(1, ...samples.map(sample => Math.ceil(states.reduce((total, state) => total + sample[state.field], 0))))
  const width = Math.min(24, Math.max(1, cadence / Math.max(cadence, end - start) * 672 * 0.75))
  const points = samples.map(sample => {
    const x = 48 + (Date.parse(sample.at) - start) / Math.max(1, end - start) * 672
    let total = 0
    const segments = states.map(state => {
      const count = sample[state.field]
      total += count
      return { ...state, count, y:190 - total / maximum * 164, height:count / maximum * 164 }
    })
    return { ...sample, x, width, total, segments }
  })
  return { points, maximum, start:new Date(start).toISOString(), end:new Date(end).toISOString() }
})
function pointLabel(point) {
  const counts = point.segments.map(segment => translate('{0}: {1}', [translate(segment.label), number(segment.count)])).join('; ')
  return translate(isDaily.value ? 'Day {0}; mean observed players by state: {1}; total: {2}; {3} hourly observations.' : 'Hour {0}; observed players by state: {1}; total: {2}; {3} observation.', [date(point.at, isDaily.value), counts, number(point.total), number(point.observation_count)])
}
</script>

<template>
  <figure class="distribution-chart">
    <figcaption>{{ $t(title) }}<ConfigHelp :text="title">{{ $t('Players in PvE, PvP, matchmaking, or the lobby at each hourly check. Longer ranges show daily averages.') }}</ConfigHelp></figcaption>
    <div class="distribution-legend"><span v-for="state in states" :key="state.field"><i :style="{ background:state.color }" aria-hidden="true" />{{ $t(state.label) }}</span></div>
    <svg viewBox="0 0 744 242" role="img" :aria-label="$t(title)">
      <title>{{ $t(title) }}</title>
      <path class="distribution-axis" d="M 48 26 V 190 H 720" />
      <path class="distribution-grid" d="M 48 26 H 720 M 48 108 H 720" />
      <text x="40" y="30" text-anchor="end">{{ number(chart.maximum) }}</text><text x="40" y="194" text-anchor="end">0</text>
      <g v-for="point in chart.points" :key="point.at">
        <rect v-for="segment in point.segments" :key="segment.field" :x="point.x - point.width / 2" :y="segment.y" :width="point.width" :height="segment.height" :fill="segment.color" />
        <rect class="distribution-target" :x="point.x - Math.max(4, point.width) / 2" y="26" :width="Math.max(4, point.width)" height="164"><title>{{ pointLabel(point) }}</title></rect>
      </g>
      <text x="48" y="216">{{ date(chart.start, true) }}</text><text v-if="!isDaily" x="48" y="234">{{ date(chart.start).slice(11) }}</text>
      <text x="720" y="216" text-anchor="end">{{ date(chart.end, true) }}</text><text v-if="!isDaily" x="720" y="234" text-anchor="end">{{ date(chart.end).slice(11) }}</text>
    </svg>
    <p v-if="!chart.points.length">{{ $t('No recorded player state observations are available for this range.') }}</p>
  </figure>
</template>

<style scoped>
.distribution-chart { min-width:0; margin:0; padding:16px; border:1px solid var(--border); border-radius:8px; background:var(--background); }
figcaption { font-size:12px; margin-bottom:12px; }
p { color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; }
.distribution-legend { display:flex; flex-wrap:wrap; gap:8px 14px; margin-bottom:8px; font-size:11px; color:var(--muted-foreground); }
.distribution-legend span { display:flex; align-items:center; gap:6px; }
.distribution-legend i { width:10px; height:10px; border-radius:2px; }
svg { display:block; width:100%; height:auto; overflow:visible; }
svg text { fill:var(--muted-foreground); font-size:16px; }
.distribution-axis, .distribution-grid { stroke:var(--border); fill:none; }
.distribution-grid { stroke-dasharray:3 5; }
.distribution-target { fill:transparent; }
.distribution-target:hover { fill:var(--foreground); fill-opacity:0.08; }
</style>
