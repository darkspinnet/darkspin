<script setup>
import { formatControlTime } from '../control_time'
import { computed } from 'vue'
import { translate } from '../localization'
import ConfigHelp from './ConfigHelp.vue'

const props = defineProps({ activity:{ type:Object, required:true }, isPersonal:Boolean })
const dayMilliseconds = 86400000
const title = computed(() => props.isPersonal ? 'DAILY PERSONAL CAMPAIGN COMPLETIONS' : 'DAILY CAMPAIGN COMPLETIONS')
function date(timestamp, isDaily = false) { return formatControlTime(timestamp, isDaily) }
function number(count) { return count.toLocaleString() }
const chart = computed(() => {
  const start = Math.floor(Date.parse(props.activity.range_start) / dayMilliseconds) * dayMilliseconds
  const end = Date.parse(props.activity.daily_observed_at)
  const collectionStart = Date.parse(props.activity.campaign_started_at)
  const emptyChart = { series:[], maximum:1, start:'', end:'', pointCount:0 }
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return emptyChart
  const collectionDay = Number.isFinite(collectionStart) ? Math.floor(collectionStart / dayMilliseconds) * dayMilliseconds : start
  const samples = (Array.isArray(props.activity.campaign_samples) ? props.activity.campaign_samples : [])
    .filter(sample => Number.isFinite(Date.parse(sample.at)) && Date.parse(sample.at) >= Math.max(start, collectionDay) && Date.parse(sample.at) <= end)
    .slice().sort((first, second) => Date.parse(first.at) - Date.parse(second.at))
  const observations = samples.filter(sample => Number.isSafeInteger(sample.completion_count) && sample.completion_count >= 0 && Number.isSafeInteger(sample.replay_count) && sample.replay_count >= 0 && sample.replay_count <= sample.completion_count)
  const maximum = Math.max(1, ...observations.map(sample => sample.completion_count))
  const series = [
    { field:'completion_count', label:'ALL SUCCESSFUL COMPLETIONS' },
    { field:'replay_count', label:'REPEAT COMPLETIONS' }
  ].map(series => {
    const points = []
    const paths = []
    let previous = null
    for (const sample of observations) {
      const timestamp = Date.parse(sample.at)
      const point = { at:sample.at, count:sample[series.field], timestamp, x:48 + (timestamp - start) / Math.max(1, end - start) * 672, y:170 - sample[series.field] / maximum * 144 }
      points.push(point)
      // Only consecutive observed UTC days establish a connected series.
      if (!previous || timestamp - previous.timestamp !== dayMilliseconds) paths.push(`M ${point.x} ${point.y}`)
      else paths[paths.length - 1] += ` L ${point.x} ${point.y}`
      previous = point
    }
    return { ...series, points, paths }
  })
  return { series, maximum, start:new Date(start).toISOString(), end:new Date(end).toISOString(), pointCount:observations.length }
})
function pointLabel(point, series) {
  return translate('Completion day {0}; {1}: {2}', [date(point.at, true), translate(series.label), number(point.count)])
}
</script>

<template>
  <figure class="campaign-completion-chart">
    <figcaption>{{ $t(title) }}<ConfigHelp :text="title">{{ $t('Successful campaign runs per player, including repeats. Co-op players each count once.') }}</ConfigHelp></figcaption>
    <div class="campaign-legend"><span v-for="(series, index) in chart.series" :key="series.field"><i :class="{ 'repeat-swatch':index === 1 }" aria-hidden="true" />{{ $t(series.label) }}</span></div>
    <svg viewBox="0 0 744 216" role="img" :aria-label="$t(title)">
      <title>{{ $t(title) }}</title>
      <path class="campaign-axis" d="M 48 26 V 170 H 720" />
      <text x="40" y="30" text-anchor="end">{{ number(chart.maximum) }}</text><text x="40" y="174" text-anchor="end">0</text>
      <g v-for="(series, index) in chart.series" :key="series.field" :class="['campaign-series', { 'repeat-series':index === 1 }]">
        <path v-for="(path, pathIndex) in series.paths" :key="pathIndex" class="campaign-line" :d="path" />
        <g v-for="point in series.points" :key="point.at"><title>{{ pointLabel(point, series) }}</title>
          <circle v-if="index === 0" class="campaign-point" :cx="point.x" :cy="point.y" r="3" />
          <rect v-else class="campaign-point" :x="point.x - 4" :y="point.y - 4" width="8" height="8" />
        </g>
      </g>
      <text x="48" y="202">{{ date(chart.start, true) }}</text><text x="720" y="202" text-anchor="end">{{ date(chart.end, true) }}</text>
    </svg>
    <p v-if="!chart.pointCount">{{ $t('No recorded campaign completion observations are available for this range.') }}</p>
  </figure>
</template>

<style scoped>
.campaign-completion-chart { min-width:0; margin:0; padding:14px; border:1px solid var(--border); border-radius:8px; background:var(--background); }
figcaption { font-size:12px; text-align:left; margin-bottom:10px; }
p { color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; }
svg { display:block; width:100%; height:auto; overflow:visible; }
svg text { fill:var(--muted-foreground); font-size:16px; }
.campaign-axis { stroke:var(--border); fill:none; }
.campaign-series { color:var(--primary); }
.campaign-line { stroke:currentColor; stroke-width:2; fill:none; }
.campaign-point { stroke:currentColor; fill:currentColor; stroke-width:2; }
.repeat-series { color:var(--foreground); }
.repeat-series .campaign-line { stroke-dasharray:7 5; }
.repeat-series .campaign-point { fill:var(--background); }
.campaign-legend { display:flex; flex-wrap:wrap; gap:14px; margin-bottom:8px; font-size:11px; color:var(--muted-foreground); }
.campaign-legend span { display:flex; align-items:center; gap:6px; }
.campaign-legend i { display:inline-block; width:22px; border-top:2px solid var(--primary); }
.campaign-legend .repeat-swatch { border-top:2px dashed var(--foreground); }
</style>
