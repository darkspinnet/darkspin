<script setup>
import { formatControlTime } from '../control_time'
import { computed } from 'vue'
import { translate } from '../localization'
import ConfigHelp from './ConfigHelp.vue'

const props = defineProps({ activity:{ type:Object, required:true }, metric:{ type:String, required:true } })
const kinds = [
  { kind:'campaign', label:'PvE', color:'#4ade80', dash:'' },
  { kind:'arena1_ranked', label:'Ranked 1v1', color:'#fb923c', dash:'8 4' },
  { kind:'arena1_unranked', label:'Unranked 1v1', color:'#60a5fa', dash:'3 4' },
  { kind:'arena2_ranked', label:'Ranked 2v2', color:'#e879f9', dash:'10 3 2 3' },
  { kind:'arena2_unranked', label:'Unranked 2v2', color:'#facc15', dash:'2 3 2 7' }
]
const isDaily = computed(() => props.activity.bucket_seconds >= 86400)
const isCount = computed(() => props.metric === 'queued_count')
const isAvailable = computed(() => Array.isArray(props.activity.queue_samples))
const title = computed(() => {
  if (isCount.value) return isDaily.value ? 'DAILY AVERAGE QUEUED PLAYERS' : 'HOURLY QUEUED PLAYERS'
  if (props.metric === 'maximum_wait_seconds') return isDaily.value ? 'DAILY LONGEST OBSERVED QUEUE AGE' : 'HOURLY LONGEST OBSERVED QUEUE AGE'
  return isDaily.value ? 'DAILY WEIGHTED AVERAGE QUEUE AGE' : 'HOURLY AVERAGE QUEUE AGE'
})
function date(timestamp, isDaily = false) { return formatControlTime(timestamp, isDaily) }
function number(count) { return count.toLocaleString(undefined, { maximumFractionDigits:2 }) }
function amount(count) { return isCount.value ? number(count) : translate('{0} seconds', [number(count)]) }
const chart = computed(() => {
  const start = Date.parse(props.activity.range_start)
  const end = Date.parse(props.activity.observed_at)
  const cadence = Number(props.activity.bucket_seconds) * 1000
  const emptyChart = { series:[], maximum:1, start:'', end:'', pointCount:0 }
  if (![3600000, 86400000].includes(cadence) || !Number.isFinite(start) || !Number.isFinite(end) || end < start) return emptyChart
  const samples = (isAvailable.value ? props.activity.queue_samples : [])
    .filter(sample => sample && Number.isFinite(Date.parse(sample.at)) && Date.parse(sample.at) >= start && Date.parse(sample.at) <= end && Number.isSafeInteger(sample.observation_count) && sample.observation_count > 0 && Number.isFinite(sample.queued_count) && sample.queued_count >= 0)
    .slice().sort((first, second) => Date.parse(first.at) - Date.parse(second.at))
  const series = kinds.map(kind => ({ ...kind, samples:samples.filter(sample => sample.kind === kind.kind && Number.isFinite(sample[props.metric]) && sample[props.metric] >= 0 && (isCount.value || sample.queued_count > 0)) }))
  const maximum = Math.max(1, ...series.flatMap(series => series.samples.map(sample => Math.ceil(sample[props.metric]))))
  let pointCount = 0
  for (const item of series) {
    item.points = []
    item.paths = []
    let previous = null
    for (const sample of item.samples) {
      const timestamp = Date.parse(sample.at)
      const point = { ...sample, timestamp, amount:sample[props.metric], x:64 + (timestamp - start) / Math.max(1, end - start) * 656, y:190 - sample[props.metric] / maximum * 164 }
      item.points.push(point)
      // Missing observations and empty-queue waiting ages remain visible gaps.
      if (!previous || timestamp - previous.timestamp !== cadence) item.paths.push(`M ${point.x} ${point.y}`)
      else item.paths[item.paths.length - 1] += ` L ${point.x} ${point.y}`
      previous = point
      pointCount++
    }
  }
  return { series, maximum, start:new Date(start).toISOString(), end:new Date(end).toISOString(), pointCount }
})
function pointLabel(point, series) {
  return translate(isDaily.value ? 'Day {0}; {1}; {2}: {3}; {4} hourly observations.' : 'Hour {0}; {1}; {2}: {3}; {4} observation.', [date(point.at, isDaily.value), translate(series.label), translate(title.value), amount(point.amount), number(point.observation_count)])
}
</script>

<template>
  <figure class="queue-chart">
    <figcaption>{{ $t(title) }}<ConfigHelp :text="title"><template v-if="isCount">{{ $t('Players waiting for a match at each hourly check, grouped by mode. Longer ranges show daily averages.') }}</template><template v-else>{{ $t('How long players were already waiting when checked, not the time needed to find a match. Empty queues have no wait time.') }}</template></ConfigHelp></figcaption>
    <div class="queue-legend"><span v-for="kind in kinds" :key="kind.kind"><svg viewBox="0 0 24 8" aria-hidden="true"><path d="M 0 4 H 24" :stroke="kind.color" :stroke-dasharray="kind.dash" /></svg>{{ $t(kind.label) }}</span></div>
    <p v-if="!isAvailable">{{ $t('Queue history is unavailable from this server. Missing queue fields are unknown.') }}</p>
    <template v-else>
      <svg viewBox="0 0 744 242" role="img" :aria-label="$t(title)">
        <title>{{ $t(title) }}</title>
        <path class="queue-axis" d="M 64 26 V 190 H 720" />
        <path class="queue-grid" d="M 64 26 H 720 M 64 108 H 720" />
        <text x="56" y="30" text-anchor="end">{{ number(chart.maximum) }}</text><text x="56" y="194" text-anchor="end">0</text>
        <text v-if="!isCount" x="64" y="16">{{ $t('SECONDS') }}</text>
        <g v-for="series in chart.series" :key="series.kind" :style="{ color:series.color }">
          <path v-for="(path, index) in series.paths" :key="index" class="queue-line" :d="path" :stroke-dasharray="series.dash" />
          <circle v-for="point in series.points" :key="point.at" class="queue-point" :cx="point.x" :cy="point.y" r="3"><title>{{ pointLabel(point, series) }}</title></circle>
        </g>
        <text x="64" y="216">{{ date(chart.start, true) }}</text><text v-if="!isDaily" x="64" y="234">{{ date(chart.start).slice(11) }}</text>
        <text x="720" y="216" text-anchor="end">{{ date(chart.end, true) }}</text><text v-if="!isDaily" x="720" y="234" text-anchor="end">{{ date(chart.end).slice(11) }}</text>
      </svg>
      <p v-if="!chart.pointCount">{{ $t(isCount ? 'No recorded queue count observations are available for this range.' : 'No recorded waiting ages are available for this range. Empty queues have no waiting age.') }}</p>
    </template>
  </figure>
</template>

<style scoped>
.queue-chart { min-width:0; margin:0; padding:16px; border:1px solid var(--border); border-radius:8px; background:var(--background); }
figcaption { font-size:12px; margin-bottom:12px; }
p { color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; }
.queue-legend { display:flex; flex-wrap:wrap; gap:8px 14px; margin-bottom:8px; font-size:11px; color:var(--muted-foreground); }
.queue-legend span { display:flex; align-items:center; gap:6px; }
.queue-legend svg { width:24px; height:8px; }
.queue-legend path { stroke-width:2; }
svg { display:block; width:100%; height:auto; overflow:visible; }
svg text { fill:var(--muted-foreground); font-size:16px; }
.queue-axis, .queue-grid { stroke:var(--border); fill:none; }
.queue-grid { stroke-dasharray:3 5; }
.queue-line { stroke:currentColor; stroke-width:2; fill:none; }
.queue-point { fill:currentColor; }
.queue-point:hover { r:5; }
</style>
