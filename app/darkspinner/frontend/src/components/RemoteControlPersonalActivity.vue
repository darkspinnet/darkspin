<script setup>
import { RefreshCw } from '@lucide/vue'
import { formatControlTime } from '../control_time'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { GetMyControlActivity, GetUserControlActivity } from '../platform'
import { panelError } from '../control_panel'
import { translate } from '../localization'
import ConfigHelp from './ConfigHelp.vue'
import CampaignCompletionChart from './CampaignCompletionChart.vue'
import CampaignLevelChart from './CampaignLevelChart.vue'
import MissionParticipationChart from './MissionParticipationChart.vue'

const props = defineProps({ profile:Object, range:{ type:String, default:'day' }, targetUserID:{ type:Number, default:0 }, isReview:{ type:Boolean, default:false }, isRangeSelectable:{ type:Boolean, default:false }, serverActivity:{ type:Object, default:null }, isEmbedded:Boolean, metric:String, suppliedActivity:{ type:Object, default:null } })
const emit = defineEmits(['busy', 'invalidated'])
const activity = ref(null)
watch(() => props.suppliedActivity, next => { if (props.isEmbedded) activity.value = next }, { immediate:true })
const reviewRange = ref('day')
const reason = ref('')
const isBusy = ref(false)
const error = ref('')
const requestedRange = computed(() => props.isReview || props.isRangeSelectable ? reviewRange.value : props.range)
const ranges = [['hour', 'LAST HOUR'], ['day', 'LAST 24 HOURS'], ['week', 'LAST 7 DAYS'], ['month', 'LAST 30 DAYS'], ['year', 'LAST YEAR']]
let generation = 0
let isDisposed = false
watch(isBusy, next => emit('busy', next), { flush:'sync' })

function isCurrent(current) { return !isDisposed && generation === current }
async function refresh() {
  if (isBusy.value || !props.profile?.serverAddress || !props.profile?.loginName) return
  if (props.isReview && (!props.targetUserID || !reason.value.trim())) return
  const current = generation
  const profile = { ...props.profile }
  isBusy.value = true
  error.value = ''
  // Clear a prior review before each audited read, including failed reads.
  if (props.isReview) activity.value = null
  try {
    const result = props.isReview
      ? await GetUserControlActivity(profile.serverAddress, profile.loginName, props.targetUserID, requestedRange.value, reason.value.trim())
      : await GetMyControlActivity(profile.serverAddress, profile.loginName, requestedRange.value)
    if (!isCurrent(current)) return
    activity.value = result
  } catch (failure) {
    if (!isCurrent(current)) return
    error.value = panelError(failure)
    if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) {
      activity.value = null
      emit('invalidated', error.value)
    }
  } finally { if (isCurrent(current)) isBusy.value = false }
}
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}/${props.targetUserID}/${props.isReview}/${requestedRange.value}`, () => {
  generation++
  isBusy.value = false
  activity.value = props.isEmbedded ? props.suppliedActivity : null
  error.value = ''
  if (!props.isReview && !props.isEmbedded) void refresh()
}, { immediate:true, flush:'sync' })
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}/${props.targetUserID}`, () => { reason.value = '' }, { flush:'sync' })
onBeforeUnmount(() => { isDisposed = true; generation++; activity.value = null; emit('busy', false) })

function date(timestamp, isDaily = false) { return formatControlTime(timestamp, isDaily) }
function number(count) { return count == null || !Number.isFinite(Number(count)) ? translate('UNKNOWN') : Number(count).toLocaleString(undefined, { maximumFractionDigits:4 }) }
const days = computed(() => (activity.value?.days || []).filter(snapshot => Number.isFinite(Date.parse(snapshot.at))).slice().sort((first, second) => Date.parse(first.at) - Date.parse(second.at)))
const growthCharts = computed(() => [
  { field:'level', label:'CROGENITOR LEVEL', samples:'level_samples', average:'average_crogenitor_level' },
  { field:'progression', label:'CAMPAIGN PROGRESSION', samples:'progression_samples', average:'average_campaign_progression' },
].filter(metric => !props.isEmbedded || props.metric === metric.field).map(growthChart))
function growthChart(metric) {
  const personalChart = { ...metric, ...makeChart(days.value, metric.field, 86400000, true), serverPoints:[], serverPaths:[], isComparison:false }
  const server = props.serverActivity
  // Only the public response already loaded for this view is compared. Audited
  // account reviews remain separate and never trigger another metrics request.
  if (props.isReview || !server || server.range !== activity.value?.range || server.range !== requestedRange.value || !Array.isArray(server[metric.samples])) return personalChart
  const start = Math.floor(Math.max(Date.parse(activity.value.range_start), Date.parse(server.range_start)) / 86400000) * 86400000
  const end = Math.min(Date.parse(activity.value.observed_at), Date.parse(server.observed_at))
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return personalChart
  const serverSamplesByDay = new Map()
  for (const sample of server[metric.samples]) {
    const timestamp = Date.parse(sample.at)
    const observed = Date.parse(sample.observed_at)
    if (!Number.isFinite(timestamp) || timestamp < start || timestamp > end ||
      !Number.isFinite(observed) || observed < timestamp || observed >= timestamp + 86400000 || observed > end ||
      !Number.isFinite(sample[metric.average]) || sample[metric.average] < 0) continue
    serverSamplesByDay.set(timestamp, sample)
  }
  const personalSamples = []
  const serverSamples = []
  for (const snapshot of days.value) {
    const timestamp = Date.parse(snapshot.at)
    const observed = Date.parse(snapshot.observed_at)
    const sample = serverSamplesByDay.get(timestamp)
    if (!sample || !Number.isFinite(snapshot[metric.field]) || snapshot[metric.field] < 0 ||
      !Number.isFinite(observed) || observed < timestamp || observed >= timestamp + 86400000 || observed > end) continue
    personalSamples.push(snapshot)
    serverSamples.push(sample)
  }
  if (!personalSamples.length) return personalChart
  const maximum = Math.max(1, ...personalSamples.map(snapshot => snapshot[metric.field]), ...serverSamples.map(sample => sample[metric.average]))
  const scale = { start, end, maximum }
  const personal = makeChart(personalSamples, metric.field, 86400000, true, scale)
  const average = makeChart(serverSamples, metric.average, 86400000, true, scale)
  return { ...metric, ...personal, serverPoints:average.points, serverPaths:average.paths, isComparison:true, end:new Date(end).toISOString() }
}
const dnaChart = computed(() => makeChart(activity.value?.dna_samples || [], 'earned', ['month', 'year'].includes(requestedRange.value) ? 86400000 : 3600000, false))
const presenceChart = computed(() => makeChart(activity.value?.presence_samples || [], 'observed_hour_count', ['month', 'year'].includes(requestedRange.value) ? 86400000 : 3600000, false))
const pvpCharts = computed(() => [
  { label:'COMPLETED MATCH PVP ROUND TIME', isAdminOnly:true, series:[{ field:'playtime_seconds', label:'ACTIVE ROUND HOURS', divisor:3600 }] },
].filter(metric => (!metric.isAdminOnly || props.isReview) && (!props.isEmbedded || props.metric === metric.label)).map(pvpChart))
function pvpChart(metric) {
  const samples = (activity.value?.pvp_samples || []).filter(sample => {
    const timestamp = Date.parse(sample.at)
    return Number.isFinite(timestamp) && timestamp % 86400000 === 0 &&
      Number.isSafeInteger(sample.match_count) && sample.match_count > 0
  })
  const seriesSamples = metric.series.map(series => samples.map(sample => {
    const amount = sample[series.field]
    const isKnown = Number.isSafeInteger(amount) && amount >= 0
    return { at:sample.at, amount:isKnown ? amount / (series.divisor || 1) : null }
  }))
  const maximum = Math.max(metric.maximum || 1, ...seriesSamples.flat().map(sample => sample.amount ?? 0))
  const dailyEnd = Date.parse(activity.value?.daily_observed_at)
  const hourlyEnd = Date.parse(activity.value?.observed_at)
  const end = Number.isFinite(dailyEnd) && dailyEnd > 0 ? Math.min(dailyEnd, hourlyEnd) : hourlyEnd
  const series = metric.series.map((series, index) => ({ ...series, ...makeChart(seriesSamples[index], 'amount', 86400000, true, { maximum, end }) }))
  return { ...metric, series, maximum, start:series[0].start, end:Number.isFinite(end) ? new Date(end).toISOString() : '', arePointsAvailable:series.some(series => series.points.length > 0) }
}
function makeChart(samples, field, cadence, isDaily, scale = {}) {
  const rangeStart = Date.parse(activity.value?.range_start)
  const start = scale.start ?? (isDaily ? Math.floor(rangeStart / 86400000) * 86400000 : rangeStart)
  const end = scale.end ?? Date.parse(activity.value?.observed_at)
  const emptyChart = { points:[], paths:[], maximum:1, start:'' }
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return emptyChart
  const snapshots = samples.filter(snapshot => Number.isFinite(Date.parse(snapshot.at)) && Date.parse(snapshot.at) >= start && Date.parse(snapshot.at) <= end).slice().sort((first, second) => Date.parse(first.at) - Date.parse(second.at))
  const maximum = Math.max(1, scale.maximum || 0, ...snapshots.map(snapshot => Number.isFinite(snapshot[field]) ? snapshot[field] : 0))
  const points = []
  const paths = []
  let previous = null
  for (const snapshot of snapshots) {
    const amount = snapshot[field]
    if (!Number.isFinite(amount) || amount < 0) { previous = null; continue }
    const timestamp = Date.parse(snapshot.at)
    const point = { at:snapshot.at, observedAt:snapshot.observed_at, from:snapshot.from, until:snapshot.until, amount, timestamp, x:48 + (timestamp - start) / Math.max(1, end - start) * 672, y:170 - amount / maximum * 144 }
    points.push(point)
    if (!previous || timestamp - previous.timestamp !== cadence) paths.push(`M ${point.x} ${point.y}`)
    else paths[paths.length - 1] += ` L ${point.x} ${point.y}`
    previous = point
  }
  return { points, paths, maximum, start:new Date(start).toISOString() }
}
function pointLabel(point) {
  if (point.from) return translate('{0} to {1}: {2}', [date(point.from), date(point.until), number(point.amount)])
  return translate('{0}: {1}', [date(point.at, ['month', 'year'].includes(requestedRange.value)), number(point.amount)])
}
function growthPointLabel(point, isServer) {
  return translate('{0}; Day {1}; observed {2}; {3}', [translate(isServer ? 'SERVER AVERAGE' : 'PERSONAL SNAPSHOT'), date(point.at, true), date(point.observedAt, true), number(point.amount)])
}
function pvpPointLabel(point, series) {
  return translate('Completion day {0}; {1}: {2}', [date(point.at, true), translate(series.label), number(point.amount)])
}
</script>

<template>
  <section class="personal-activity" :class="{ embedded:isEmbedded }" :aria-label="$t(isReview ? 'PLAYER METRICS REVIEW' : 'MY PROGRESS')" :aria-busy="isBusy">
    <header v-if="!isEmbedded"><h3>{{ $t(isReview ? 'PLAYER METRICS REVIEW' : 'MY PROGRESS') }}<ConfigHelp v-if="isReview" text="PLAYER METRICS REVIEW">{{ $t('Review this player for the selected date range. Each review and its reason are logged.') }}</ConfigHelp></h3><Button v-if="!isReview" variant="outline" :disabled="isBusy" @click="refresh" size="icon" :aria-label="$t('REFRESH MY PROGRESS')" :title="$t('REFRESH MY PROGRESS')"><RefreshCw :size="16" aria-hidden="true" /></Button></header>
    <label v-if="!isReview && isRangeSelectable" class="personal-range"><span>{{ $t('DATE RANGE') }}</span><Select v-model="reviewRange" :disabled="isBusy"><SelectTrigger :aria-label="$t('DATE RANGE')"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="option in ranges" :key="option[0]" :value="option[0]">{{ $t(option[1]) }}</SelectItem></SelectContent></Select></label>
    <form v-if="isReview" class="review-form" @submit.prevent="refresh">
      <label><span>{{ $t('DATE RANGE') }}</span><Select v-model="reviewRange" :disabled="isBusy"><SelectTrigger :aria-label="$t('DATE RANGE')"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="option in ranges" :key="option[0]" :value="option[0]">{{ $t(option[1]) }}</SelectItem></SelectContent></Select></label>
      <label class="review-reason"><span>{{ $t('REVIEW REASON') }}</span><Input v-model="reason" :maxlength="128" :disabled="isBusy" autocomplete="off" /></label>
      <Button type="submit" variant="outline" :disabled="isBusy || !reason.trim()">{{ $t('READ PLAYER METRICS') }}</Button>
    </form>

    <p v-if="error" role="alert">{{ $t(error) }}</p>
    <p v-if="isBusy" role="status">{{ $t('LOADING...') }}</p>
    <template v-if="activity">
      <div class="personal-charts">
        <CampaignCompletionChart v-if="!isEmbedded || metric === 'campaign'" :activity="activity" is-personal />
        <CampaignLevelChart v-if="!isEmbedded || metric === 'campaign-level'" :activity="activity" is-personal />
        <MissionParticipationChart v-if="!isEmbedded || metric === 'mission'" :activity="activity" is-personal />
        <MissionParticipationChart v-if="!isEmbedded || metric === 'unresolved'" :activity="activity" is-personal is-unresolved />
        <figure v-for="chart in growthCharts.filter(chart => !isEmbedded || metric === chart.field)" :key="chart.field">
          <figcaption>{{ $t(chart.label) }}<template v-if="!isEmbedded"> · {{ $t(chart.isComparison ? 'PERSONAL AND SERVER DAILY SNAPSHOTS' : 'DAILY PERSONAL SNAPSHOTS') }}</template><ConfigHelp :text="chart.isComparison ? 'PERSONAL AND SERVER DAILY SNAPSHOTS' : 'DAILY PERSONAL SNAPSHOTS'"><template v-if="chart.isComparison">{{ $t('Your daily progress compared with the server average on days with data for both.') }}</template><template v-else>{{ $t('Saved progress for this Crogenitor, recorded daily. Gaps mean no data was recorded.') }}</template></ConfigHelp></figcaption>
          <div v-if="chart.isComparison" class="chart-legend"><span><i class="personal-swatch" aria-hidden="true" />{{ $t('PERSONAL SNAPSHOT') }}</span><span><i class="server-swatch" aria-hidden="true" />{{ $t('SERVER AVERAGE') }}</span></div>
          <svg viewBox="0 0 744 216" role="img" :aria-label="$t(chart.label)">
            <title>{{ $t(chart.label) }} · {{ $t(chart.isComparison ? 'PERSONAL AND SERVER DAILY SNAPSHOTS' : 'DAILY PERSONAL SNAPSHOTS') }}</title>
            <path class="chart-axis" d="M 48 26 V 170 H 720" /><text x="40" y="30" text-anchor="end">{{ number(chart.maximum) }}</text><text x="40" y="174" text-anchor="end">0</text>
            <path v-for="(path, index) in chart.paths" :key="index" class="chart-line" :d="path" />
            <path v-for="(path, index) in chart.serverPaths" :key="`server-${index}`" class="chart-server-line" :d="path" />
            <circle v-for="point in chart.points" :key="point.at" class="chart-point" :cx="point.x" :cy="point.y" r="3"><title>{{ growthPointLabel(point, false) }}</title></circle>
            <rect v-for="point in chart.serverPoints" :key="`server-${point.at}`" class="chart-server-point" :x="point.x - 4" :y="point.y - 4" width="8" height="8"><title>{{ growthPointLabel(point, true) }}</title></rect>
            <text x="48" y="202">{{ date(chart.start, true) }}</text><text x="720" y="202" text-anchor="end">{{ date(chart.end || activity.observed_at, true) }}</text>
          </svg><p v-if="!chart.points.length">{{ $t(chart.isComparison ? 'No shared daily observations are available for comparison.' : 'No personal daily snapshots are available for this range.') }}</p>

        </figure>
        <figure v-if="!isEmbedded || metric === 'dna'"><figcaption>{{ $t(['month', 'year'].includes(requestedRange) ? 'DAILY PERSONAL DNA EARNED' : 'HOURLY PERSONAL DNA EARNED') }}<ConfigHelp text="PERSONAL DNA EARNINGS">{{ $t('Recorded DNA earnings for this Crogenitor from gameplay rewards and vendor sales.') }}</ConfigHelp></figcaption>
          <svg viewBox="0 0 744 216" role="img" :aria-label="$t('PERSONAL DNA EARNINGS')"><title>{{ $t('PERSONAL DNA EARNINGS') }}</title>
            <path class="chart-axis" d="M 48 26 V 170 H 720" /><text x="40" y="30" text-anchor="end">{{ number(dnaChart.maximum) }}</text><text x="40" y="174" text-anchor="end">0</text>
            <path v-for="(path, index) in dnaChart.paths" :key="index" class="chart-line" :d="path" />
            <circle v-for="point in dnaChart.points" :key="point.at" class="chart-point" :cx="point.x" :cy="point.y" r="3"><title>{{ pointLabel(point) }}</title></circle>
            <text x="48" y="202">{{ date(activity.range_start, ['month', 'year'].includes(requestedRange)) }}</text><text x="720" y="202" text-anchor="end">{{ date(activity.observed_at, ['month', 'year'].includes(requestedRange)) }}</text>
          </svg><p v-if="!dnaChart.points.length">{{ $t('No DNA earnings observations are available for this range.') }}</p>

        </figure>
        <figure v-if="isReview && (!isEmbedded || metric === 'presence')"><figcaption>{{ $t('OBSERVED PERSONAL PRESENCE') }}<ConfigHelp text="OBSERVED PERSONAL PRESENCE">{{ $t('Hours in which you were seen online. This is not total playtime.') }}</ConfigHelp></figcaption>
          <svg viewBox="0 0 744 216" role="img" :aria-label="$t('OBSERVED PERSONAL PRESENCE')"><title>{{ $t('OBSERVED PERSONAL PRESENCE') }}</title>
            <path class="chart-axis" d="M 48 26 V 170 H 720" /><text x="40" y="30" text-anchor="end">{{ number(presenceChart.maximum) }}</text><text x="40" y="174" text-anchor="end">0</text>
            <g v-for="point in presenceChart.points" :key="point.at"><path class="chart-line" :d="`M ${point.x} 170 V ${point.y}`" /><circle class="chart-point" :cx="point.x" :cy="point.y" r="3"><title>{{ pointLabel(point) }}</title></circle></g>
            <text x="48" y="202">{{ date(presenceChart.start, ['month', 'year'].includes(requestedRange)) }}</text><text x="720" y="202" text-anchor="end">{{ date(activity.observed_at, ['month', 'year'].includes(requestedRange)) }}</text>
          </svg><p v-if="!presenceChart.points.length">{{ $t('No personal presence observations are available for this range.') }}</p>
        </figure>
        <figure v-for="chart in pvpCharts.filter(chart => !isEmbedded || metric === chart.label)" :key="chart.label">
          <figcaption>{{ $t(chart.label) }}<ConfigHelp :text="chart.label"><template v-if="chart.help">{{ $t(chart.help) }}</template><template v-else>{{ $t('Daily results from completed best-of-three PvP matches. Unfinished matches are excluded.') }}</template></ConfigHelp></figcaption>
          <div class="chart-legend"><span v-for="(series, index) in chart.series" :key="series.field"><i :class="`receipt-swatch-${index}`" aria-hidden="true" />{{ $t(series.label) }}</span></div>
          <svg viewBox="0 0 744 216" role="img" :aria-label="$t(chart.label)"><title>{{ $t(chart.label) }}</title>
            <path class="chart-axis" d="M 48 26 V 170 H 720" /><text x="40" y="30" text-anchor="end">{{ number(chart.maximum) }}</text><text x="40" y="174" text-anchor="end">0</text>
            <g v-for="(series, index) in chart.series" :key="series.field" :class="`receipt-series-${index}`">
              <path v-for="(path, pathIndex) in series.paths" :key="pathIndex" class="receipt-line" :d="path" />
              <g v-for="point in series.points" :key="point.at"><title>{{ pvpPointLabel(point, series) }}</title>
                <circle v-if="index === 0" class="receipt-point" :cx="point.x" :cy="point.y" r="3" />
                <rect v-else-if="index === 1" class="receipt-point" :x="point.x - 4" :y="point.y - 4" width="8" height="8" />
                <path v-else class="receipt-point" :d="`M ${point.x} ${point.y - 5} L ${point.x - 5} ${point.y + 4} H ${point.x + 5} Z`" />
              </g>
            </g>
            <text x="48" y="202">{{ date(chart.start, true) }}</text><text x="720" y="202" text-anchor="end">{{ date(chart.end, true) }}</text>
          </svg><p v-if="!chart.arePointsAvailable">{{ $t('No saved completed-match observations are available for this range.') }}</p>
        </figure>
      </div>
    </template>
  </section>
</template>

<style scoped>
.personal-activity { display:flex; flex-direction:column; gap:14px; margin:20px 0; }
header, .review-form { display:flex; flex-wrap:wrap; align-items:end; justify-content:space-between; gap:12px; }
.review-form label, .personal-range { display:flex; flex-direction:column; gap:6px; min-width:180px; font-size:11px; }
.review-reason { flex:1; }
.personal-activity.embedded, .embedded .personal-charts { display:contents; }
.personal-charts { display:grid; grid-template-columns:repeat(auto-fit,minmax(min(100%,420px),1fr)); gap:16px; }
.personal-charts > figure { min-width:0; margin:0; }
figure { padding:14px; border:1px solid var(--border); border-radius:8px; background:var(--background); }
p { color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; }
figcaption { font-size:12px; text-align:left; margin-bottom:10px; }
svg { display:block; width:100%; height:auto; overflow:visible; }
svg text { fill:var(--muted-foreground); font-size:16px; }
.chart-axis { stroke:var(--border); fill:none; }
.chart-line { stroke:var(--primary); stroke-width:2; fill:none; }
.chart-point { fill:var(--primary); }
.chart-server-line { stroke:var(--foreground); stroke-width:2; stroke-dasharray:7 5; fill:none; }
.chart-server-point { fill:transparent; stroke:var(--foreground); stroke-width:2; }
.chart-legend { display:flex; flex-wrap:wrap; gap:14px; margin-bottom:8px; font-size:11px; color:var(--muted-foreground); }
.chart-legend span { display:flex; align-items:center; gap:6px; }
.chart-legend i { display:inline-block; width:22px; }
.personal-swatch { border-top:2px solid var(--primary); }
.server-swatch { border-top:2px dashed var(--foreground); }
.receipt-series-0 { color:var(--primary); }
.receipt-series-1 { color:var(--foreground); }
.receipt-series-2 { color:var(--muted-foreground); }
.receipt-line { stroke:currentColor; stroke-width:2; fill:none; }
.receipt-point { stroke:currentColor; fill:transparent; stroke-width:2; }
.receipt-series-0 .receipt-point { fill:currentColor; }
.receipt-series-1 .receipt-line { stroke-dasharray:7 5; }
.receipt-series-2 .receipt-line { stroke-dasharray:2 5; }
.receipt-swatch-0 { border-top:2px solid var(--primary); }
.receipt-swatch-1 { border-top:2px dashed var(--foreground); }
.receipt-swatch-2 { border-top:2px dotted var(--muted-foreground); }
</style>
