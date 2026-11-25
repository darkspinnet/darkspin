<script setup>
import { formatControlTime } from '../control_time'
import { computed } from 'vue'
import { translate } from '../localization'
import ConfigHelp from './ConfigHelp.vue'

const props = defineProps({ activity:{ type:Object, required:true }, isPersonal:Boolean, isUnresolved:Boolean })
const dayMilliseconds = 86400000
const title = computed(() => props.isUnresolved
  ? (props.isPersonal ? 'PERSONAL UNRESOLVED MISSION STARTS' : 'UNRESOLVED MISSION STARTS')
  : (props.isPersonal ? 'DAILY PERSONAL MISSION PARTICIPATION' : 'DAILY MISSION PARTICIPATION'))
function timestamp(at) { return typeof at === 'string' && !at.startsWith('0001-') ? Date.parse(at) : NaN }
function date(at, isDaily = false) { return formatControlTime(at, isDaily) }
function number(count) { return count.toLocaleString() }
const isKnown = computed(() => Array.isArray(props.activity.mission_samples) && Number.isFinite(timestamp(props.activity.mission_started_at)))
const chart = computed(() => {
  const start = Math.floor(timestamp(props.activity.range_start) / dayMilliseconds) * dayMilliseconds
  const end = timestamp(props.activity.daily_observed_at)
  const collectionStart = timestamp(props.activity.mission_started_at)
  const emptyChart = { series:[], maximum:1, start:'', end:'', pointCount:0 }
  if (!isKnown.value || !Number.isFinite(start) || !Number.isFinite(end) || end < start) return emptyChart
  const collectionDay = Math.floor(collectionStart / dayMilliseconds) * dayMilliseconds
  const samples = props.activity.mission_samples.filter(sample => {
    const at = timestamp(sample.at)
    return Number.isFinite(at) && at % dayMilliseconds === 0 && at >= Math.max(start, collectionDay) && at <= end
  }).slice().sort((first, second) => timestamp(first.at) - timestamp(second.at))
  const metrics = props.isUnresolved
    ? [{ field:'unresolved_count', label:'STARTS WITHOUT LINKED SUCCESS', dayLabel:'Start day {0}; {1}: {2}; observed {3}' }]
    : [{ field:'started_count', label:'STARTED BY START DAY', dayLabel:'Start day {0}; {1}: {2}' },
      { field:'completed_count', label:'LINKED SUCCESSES BY COMPLETION DAY', dayLabel:'Completion day {0}; {1}: {2}' }]
  const observations = metrics.flatMap(metric => samples.filter(sample => Number.isSafeInteger(sample[metric.field]) && sample[metric.field] >= 0).map(sample => sample[metric.field]))
  const maximum = Math.max(1, ...observations)
  const series = metrics.map(metric => {
    const points = []
    const paths = []
    let previous = null
    for (const sample of samples) {
      if (!Number.isSafeInteger(sample[metric.field]) || sample[metric.field] < 0) continue
      const at = timestamp(sample.at)
      const point = { at:sample.at, count:sample[metric.field], timestamp:at, x:48 + (at - start) / Math.max(1, end - start) * 672, y:170 - sample[metric.field] / maximum * 144 }
      points.push(point)
      if (!previous || at - previous.timestamp !== dayMilliseconds) paths.push(`M ${point.x} ${point.y}`)
      else paths[paths.length - 1] += ` L ${point.x} ${point.y}`
      previous = point
    }
    return { ...metric, points, paths }
  })
  return { series, maximum, start:new Date(start).toISOString(), end:new Date(end).toISOString(), pointCount:observations.length }
})
function pointLabel(point, series) {
  return translate(series.dayLabel, [date(point.at, true), translate(series.label), number(point.count), date(props.activity.daily_observed_at, true)])
}
</script>

<template>
  <figure class="mission-chart">
    <figcaption>{{ $t(title) }}<ConfigHelp :text="title"><template v-if="isUnresolved">{{ $t('Started missions without a recorded success yet. These may still be resumed; they are not counted as failures.') }}</template><template v-else>{{ $t('Campaign starts and successful finishes, counted per player on the day each occurs.') }}</template></ConfigHelp></figcaption>
    <div class="mission-legend"><span v-for="(series, index) in chart.series" :key="series.field"><i :class="{ 'success-swatch':index === 1 }" aria-hidden="true" />{{ $t(series.label) }}</span></div>
    <svg viewBox="0 0 744 216" role="img" :aria-label="$t(title)">
      <title>{{ $t(title) }}</title>
      <path class="mission-axis" d="M 48 26 V 170 H 720" />
      <text x="40" y="30" text-anchor="end">{{ number(chart.maximum) }}</text><text x="40" y="174" text-anchor="end">0</text>
      <g v-for="(series, index) in chart.series" :key="series.field" :class="['mission-series', { 'success-series':index === 1 }]">
        <path v-for="(path, pathIndex) in series.paths" :key="pathIndex" class="mission-line" :d="path" />
        <g v-for="point in series.points" :key="point.at"><title>{{ pointLabel(point, series) }}</title>
          <circle v-if="index === 0" class="mission-point" :cx="point.x" :cy="point.y" r="3" />
          <rect v-else class="mission-point" :x="point.x - 4" :y="point.y - 4" width="8" height="8" />
        </g>
      </g>
      <text x="48" y="202">{{ date(chart.start, true) }}</text><text x="720" y="202" text-anchor="end">{{ date(chart.end, true) }}</text>
    </svg>
    <p v-if="!chart.pointCount">{{ $t(isKnown ? 'No recorded mission participation observations are available for this range.' : 'Mission participation statistics are unavailable from this server.') }}</p>
  </figure>
</template>

<style scoped>
.mission-chart { min-width:0; margin:0; padding:14px; border:1px solid var(--border); border-radius:8px; background:var(--background); }
figcaption { font-size:12px; text-align:left; margin-bottom:10px; }
p { color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; }
svg { display:block; width:100%; height:auto; overflow:visible; }
svg text { fill:var(--muted-foreground); font-size:16px; }
.mission-axis { stroke:var(--border); fill:none; }
.mission-series { color:var(--primary); }
.mission-line { stroke:currentColor; stroke-width:2; fill:none; }
.mission-point { stroke:currentColor; fill:currentColor; stroke-width:2; }
.success-series { color:var(--foreground); }
.success-series .mission-line { stroke-dasharray:7 5; }
.success-series .mission-point { fill:var(--background); }
.mission-legend { display:flex; flex-wrap:wrap; gap:14px; margin-bottom:8px; font-size:11px; color:var(--muted-foreground); }
.mission-legend span { display:flex; align-items:center; gap:6px; }
.mission-legend i { display:inline-block; width:22px; border-top:2px solid var(--primary); }
.mission-legend .success-swatch { border-top:2px dashed var(--foreground); }
</style>
