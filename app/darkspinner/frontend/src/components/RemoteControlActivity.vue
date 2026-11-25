<script setup>
import { RefreshCw } from '@lucide/vue'
import { formatControlTime } from '../control_time'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { GetRemoteControlActivity, GetMyControlActivity } from '../platform'
import { panelError } from '../control_panel'
import { translate } from '../localization'
import ControlMetric from './ControlMetric.vue'
import ConfigHelp from './ConfigHelp.vue'
import CampaignCompletionChart from './CampaignCompletionChart.vue'
import CampaignLevelChart from './CampaignLevelChart.vue'
import MissionParticipationChart from './MissionParticipationChart.vue'
import PlayerStateDistributionChart from './PlayerStateDistributionChart.vue'
import QueueActivityChart from './QueueActivityChart.vue'
import DashboardPlayerReview from './DashboardPlayerReview.vue'

const props = defineProps({ profile:Object, isLevelAvailable:Boolean, isDNAAvailable:Boolean, isProgressionAvailable:{ type:Boolean, default:false }, isPersonalAvailable:{ type:Boolean, default:false }, isPlayerReviewAvailable:Boolean })
const emit = defineEmits(['busy', 'invalidated'])
const range = ref('day')
const activity = ref(null)
const isBusy = ref(false)
const isPersonalBusy = ref(false)
const personalActivity = ref(null)
const personalError = ref('')
const graphScope = ref('server')
const playerActivity = ref(null)
const playerLabel = ref('')
const isPlayerBusy = ref(false)
const playerRefreshKey = ref(0)
function clearPlayerReview() {
  playerActivity.value = null
  playerLabel.value = ''
  if (graphScope.value === 'player') graphScope.value = 'server'
}
function refreshDashboard() {
  playerRefreshKey.value++
  void refresh()
}
function acceptPlayerReview(result) {
  if (!props.isPlayerReviewAvailable) return
  playerActivity.value = result.activity
  playerLabel.value = result.label
  graphScope.value = 'player'
}
watch(() => props.isPlayerReviewAvailable, next => { if (!next) clearPlayerReview() }, { flush:'sync' })
async function loadPersonalActivity(isForced = false) {
  if (!props.isPersonalAvailable || isPersonalBusy.value || (!isForced && personalActivity.value)) return
  if (!props.profile?.serverAddress || !props.profile?.loginName) return
  const current = generation
  const profile = { ...props.profile }
  const requestedRange = range.value
  isPersonalBusy.value = true
  personalError.value = ''
  try {
    const result = await GetMyControlActivity(profile.serverAddress, profile.loginName, requestedRange)
    if (isCurrent(current, profile, requestedRange)) personalActivity.value = result
  } catch (failure) {
    if (!isCurrent(current, profile, requestedRange)) return
    personalError.value = panelError(failure)
    if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) {
      personalActivity.value = null
      emit('invalidated', personalError.value)
    }
  } finally {
    if (isCurrent(current, profile, requestedRange)) isPersonalBusy.value = false
  }
}
const isAnyBusy = computed(() => isBusy.value || isPersonalBusy.value || isPlayerBusy.value)
const error = ref('')
const ranges = [['hour', 'LAST HOUR'], ['day', 'LAST 24 HOURS'], ['week', 'LAST 7 DAYS'], ['month', 'LAST 30 DAYS'], ['year', 'LAST YEAR']]
let generation = 0
let isDisposed = false

watch(isAnyBusy, next => emit('busy', next), { flush:'sync' })
function isCurrent(current, profile, requestedRange) {
  return !isDisposed && current === generation && requestedRange === range.value && profile.serverAddress === props.profile?.serverAddress && profile.loginName === props.profile?.loginName
}
async function refresh() {
  if (isBusy.value || !props.profile?.serverAddress || !props.profile?.loginName) return
  const current = generation
  const profile = { serverAddress:props.profile.serverAddress, loginName:props.profile.loginName }
  const requestedRange = range.value
  isBusy.value = true
  error.value = ''
  try {
    const result = await GetRemoteControlActivity(profile.serverAddress, profile.loginName, requestedRange)
    if (!isCurrent(current, profile, requestedRange)) return
    activity.value = result
    if (props.isPersonalAvailable) void loadPersonalActivity(true)
  } catch (failure) {
    if (!isCurrent(current, profile, requestedRange)) return
    error.value = panelError(failure)
    if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) {
      activity.value = null
      emit('invalidated', error.value)
    }
  } finally { if (isCurrent(current, profile, requestedRange)) isBusy.value = false }
}
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}/${range.value}`, () => {
  generation++
  isBusy.value = false
  activity.value = null
  personalActivity.value = null
  personalError.value = ''
  isPersonalBusy.value = false
  clearPlayerReview()
  error.value = ''
  void refresh()
}, { immediate:true, flush:'sync' })
onBeforeUnmount(() => { isDisposed = true; generation++; emit('busy', false) })

function date(timestamp, isDaily = false) { return formatControlTime(timestamp, isDaily) }
function number(count) { return Number(count).toLocaleString(undefined, { maximumFractionDigits:2 }) }
const chart = computed(() => {
  const result = activity.value
  if (!result) return { points:[], paths:[], maximum:1 }
  const start = Date.parse(result.range_start)
  const end = Date.parse(result.observed_at)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return { points:[], paths:[], maximum:1 }
  const samples = (result.samples || []).filter(sample => Number.isFinite(Date.parse(sample.at)) && Date.parse(sample.at) >= start && Date.parse(sample.at) <= end && Number.isFinite(sample.online_count) && sample.online_count >= 0).sort((first, second) => Date.parse(first.at) - Date.parse(second.at))
  const maximum = Math.max(1, Math.ceil(Math.max(0, ...samples.map(sample => sample.online_count))))
  const points = samples.map(sample => ({ ...sample, timestamp:Date.parse(sample.at), x:48 + (Date.parse(sample.at) - start) / (end - start) * 672, y:190 - sample.online_count / maximum * 164 }))
  const paths = []
  let previous = null
  const cadence = Number(result.bucket_seconds) * 1000
  for (const point of points) {
    // Missing buckets are unknown coverage, rather than zero online players.
    if (!previous || cadence <= 0 || point.timestamp - previous.timestamp > cadence + 1) paths.push(`M ${point.x} ${point.y}`)
    else paths[paths.length - 1] += ` L ${point.x} ${point.y}`
    previous = point
  }
  return { points, paths, maximum }
})
function pointLabel(point) {
  return translate('{0}: {1} players online', [date(point.at, activity.value?.bucket_seconds >= 86400), number(point.online_count)])
}
const chartSummary = computed(() => translate('{0} observed buckets; peak observed online: {1}. Gaps indicate unavailable observations.', [chart.value.points.length, activity.value?.peak_online_count ?? 0]))
const dnaChart = computed(() => {
  const result = activity.value
  if (!result) return { points:[], paths:[], maximum:1 }
  const start = Date.parse(result.range_start)
  const end = Date.parse(result.observed_at)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return { points:[], paths:[], maximum:1 }
  const samples = (result.dna_samples || []).filter(sample => Number.isFinite(Date.parse(sample.at)) && Date.parse(sample.at) >= start && Date.parse(sample.at) <= end && Number.isFinite(sample.earned) && sample.earned >= 0).sort((first, second) => Date.parse(first.at) - Date.parse(second.at))
  const maximum = Math.max(1, Math.ceil(Math.max(0, ...samples.map(sample => sample.earned))))
  const points = samples.map(sample => ({ ...sample, timestamp:Date.parse(sample.at), x:48 + (Date.parse(sample.at) - start) / (end - start) * 672, y:190 - sample.earned / maximum * 164 }))
  const paths = []
  const cadence = Number(result.bucket_seconds) * 1000
  let previous = null
  for (const point of points) {
    // Unobserved periods have no implied DNA reward total.
    if (!previous || !Number.isFinite(cadence) || cadence <= 0 || point.timestamp - previous.timestamp > cadence + 1) paths.push(`M ${point.x} ${point.y}`)
    else paths[paths.length - 1] += ` L ${point.x} ${point.y}`
    previous = point
  }
  return { points, paths, maximum }
})
function dnaPointLabel(point) {
  return translate('{0}: {1} DNA earned in this recorded bucket', [date(point.at, activity.value?.bucket_seconds >= 86400), number(point.earned)])
}
const dnaChartSummary = computed(() => translate('{0} recorded buckets; {1} DNA earned in range. Buckets show DNA earnings recorded at profile-save time. Gaps indicate unavailable observations.', [dnaChart.value.points.length, number(activity.value?.dna_earned ?? 0)]))
const dayMilliseconds = 86400000
function day(timestamp) { return date(timestamp, true) }
const levelChart = computed(() => {
  const result = activity.value
  if (!result) return { points:[], paths:[], maximum:1, start:'' }
  const rangeStart = Date.parse(result.range_start)
  const end = Date.parse(result.observed_at)
  const start = Math.floor(rangeStart / dayMilliseconds) * dayMilliseconds
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return { points:[], paths:[], maximum:1, start:'' }
  const samples = (result.level_samples || []).filter(sample => Number.isFinite(Date.parse(sample.at)) && Date.parse(sample.at) >= start && Date.parse(sample.at) <= end).sort((first, second) => Date.parse(first.at) - Date.parse(second.at))
  const maximum = Math.max(1, Math.ceil(Math.max(0, ...samples.map(sample => Number.isFinite(sample.average_crogenitor_level) ? sample.average_crogenitor_level : 0))))
  const points = []
  const paths = []
  let previous = null
  for (const sample of samples) {
    if (!Number.isFinite(sample.average_crogenitor_level) || sample.average_crogenitor_level < 0) {
      previous = null
      continue
    }
    const timestamp = Date.parse(sample.at)
    const point = { ...sample, timestamp, x:48 + (timestamp - start) / Math.max(1, end - start) * 672, y:190 - sample.average_crogenitor_level / maximum * 164 }
    points.push(point)
    // A daily snapshot never implies coverage for an unobserved UTC day.
    if (!previous || timestamp - previous.timestamp !== dayMilliseconds) paths.push(`M ${point.x} ${point.y}`)
    else paths[paths.length - 1] += ` L ${point.x} ${point.y}`
    previous = point
  }
  return { points, paths, maximum, start:new Date(start).toISOString() }
})
function levelPointLabel(point) {
  return translate('{0}: average Crogenitor level {1}', [day(point.at), number(point.average_crogenitor_level)])
}
const levelChartSummary = computed(() => translate('{0} daily snapshots with known average Crogenitor levels. Missing days and snapshots without accounts have no line.', [levelChart.value.points.length]))
const progressionChart = computed(() => {
  const result = activity.value
  if (!result) return { points:[], paths:[], maximum:1, start:'' }
  const rangeStart = Date.parse(result.range_start)
  const end = Date.parse(result.observed_at)
  const start = Math.floor(rangeStart / dayMilliseconds) * dayMilliseconds
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return { points:[], paths:[], maximum:1, start:'' }
  const samples = (result.progression_samples || []).filter(sample => Number.isFinite(Date.parse(sample.at)) && Date.parse(sample.at) >= start && Date.parse(sample.at) <= end).sort((first, second) => Date.parse(first.at) - Date.parse(second.at))
  const maximum = Math.max(1, Math.ceil(Math.max(0, ...samples.map(sample => Number.isFinite(sample.average_campaign_progression) ? sample.average_campaign_progression : 0))))
  const points = []
  const paths = []
  let previous = null
  for (const sample of samples) {
    if (!Number.isFinite(sample.average_campaign_progression) || sample.average_campaign_progression < 0) {
      previous = null
      continue
    }
    const timestamp = Date.parse(sample.at)
    const point = { ...sample, timestamp, x:48 + (timestamp - start) / Math.max(1, end - start) * 672, y:190 - sample.average_campaign_progression / maximum * 164 }
    points.push(point)
    // Keep unknown account populations and unobserved UTC days as gaps.
    if (!previous || timestamp - previous.timestamp !== dayMilliseconds) paths.push(`M ${point.x} ${point.y}`)
    else paths[paths.length - 1] += ` L ${point.x} ${point.y}`
    previous = point
  }
  return { points, paths, maximum, start:new Date(start).toISOString() }
})
function progressionPointLabel(point) {
  return translate('{0}: average campaign progression {1}; highest completed campaign {2}', [day(point.at), number(point.average_campaign_progression), point.highest_completed_campaign == null ? translate('UNKNOWN') : number(point.highest_completed_campaign)])
}
const progressionChartSummary = computed(() => translate('{0} daily snapshots with known average campaign progression. Missing days and snapshots without accounts have no line.', [progressionChart.value.points.length]))

function historyChart(samples, field, isDaily, observedAt = '') {
  const result = activity.value
  if (!result) return { points:[], paths:[], maximum:1, start:'' }
  const rangeStart = Date.parse(result.range_start)
  const start = isDaily ? Math.floor(rangeStart / dayMilliseconds) * dayMilliseconds : rangeStart
  const end = Date.parse(observedAt || (isDaily ? result.daily_observed_at : result.observed_at))
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return { points:[], paths:[], maximum:1, start:'' }
  const observations = (samples || []).filter(sample => Number.isFinite(Date.parse(sample.at)) && Date.parse(sample.at) >= start && Date.parse(sample.at) <= end && Number.isFinite(sample[field]) && sample[field] >= 0).sort((first, second) => Date.parse(first.at) - Date.parse(second.at))
  const maximum = Math.max(1, ...observations.map(sample => Math.ceil(sample[field])))
  const points = observations.map(sample => ({ at:sample.at, count:sample[field], timestamp:Date.parse(sample.at), x:48 + (Date.parse(sample.at) - start) / (end - start) * 672, y:190 - sample[field] / maximum * 164 }))
  const paths = []
  let previous = null
  const cadence = isDaily ? dayMilliseconds : Number(result.bucket_seconds) * 1000
  for (const point of points) {
    if (!previous || point.timestamp - previous.timestamp !== cadence) paths.push(`M ${point.x} ${point.y}`)
    else paths[paths.length - 1] += ` L ${point.x} ${point.y}`
    previous = point
  }
  return { points, paths, maximum, isDaily:isDaily || result.bucket_seconds >= 86400, start:new Date(start).toISOString(), end:new Date(end).toISOString() }
}
const historyCharts = computed(() => [
  { title:'DAILY NEW ACCOUNT SIGNUPS', help:'New accounts registered each day. Deleting an account does not reduce this count.', ...historyChart(activity.value?.signup_samples, 'account_count', true, activity.value?.observed_at) },
  { title:'DAILY ACCOUNT POPULATION', help:'Saved accounts, excluding deleted accounts, counted once per day.', ...historyChart(activity.value?.population_samples, 'account_count', true) },
  { title:'DAILY FIRST RECORDED GAMEPLAY ADMISSIONS', help:'Accounts first seen entering gameplay since tracking began. Existing accounts may be included.', ...historyChart(activity.value?.first_admission_samples, 'account_count', true, activity.value?.observed_at) },
  { title:'DAILY REMAINING ACCOUNT CREATIONS', help:'Accounts still saved, grouped by creation date. Deleted accounts are excluded.', ...historyChart(activity.value?.creation_samples, 'account_count', true) },
  { title:activity.value?.bucket_seconds >= 86400 ? 'DAILY DISTINCT OBSERVED PLAYERS' : 'HOURLY DISTINCT OBSERVED PLAYERS', help:'Different players seen in each hour or day. A player may appear in several periods.', ...historyChart(activity.value?.samples, 'unique_player_count', false) },
  { title:activity.value?.bucket_seconds >= 86400 ? 'DAILY PEAK OBSERVED ONLINE' : 'HOURLY PEAK OBSERVED ONLINE', help:'The highest number of players seen online together in each hour or day.', ...historyChart(activity.value?.samples, 'peak_online_count', false) }
])
function historyPointLabel(point, history) { return translate('{0}: {1} accounts', [date(point.at, history.isDaily), number(point.count)]) }

</script>

<template>
  <section class="activity-view" :aria-label="$t('PLAYER ACTIVITY')" :aria-busy="isBusy">
    <header class="dashboard-toolbar">
      <div class="activity-actions">
        <label class="activity-range"><span>{{ $t('DATE RANGE') }}</span>
          <Select v-model="range" :disabled="isAnyBusy"><SelectTrigger :aria-label="$t('DATE RANGE')"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="option in ranges" :key="option[0]" :value="option[0]">{{ $t(option[1]) }}</SelectItem></SelectContent></Select>
        </label>
        <label v-if="isPersonalAvailable || isPlayerReviewAvailable" class="activity-scope"><span>{{ $t('Graph data') }}</span>
          <Select v-model="graphScope">
            <SelectTrigger :aria-label="$t('Graph data')"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="server">{{ $t('SERVER') }}</SelectItem>
              <SelectItem v-if="isPersonalAvailable" value="personal">{{ $t('MY PROGRESS') }}</SelectItem>
              <SelectItem v-if="isPlayerReviewAvailable && playerActivity" value="player">{{ $t('SELECTED PLAYER') }}</SelectItem>
            </SelectContent>
          </Select>
        </label>
        <Button variant="outline" :disabled="isAnyBusy" @click="refreshDashboard" size="icon" :aria-label="$t('REFRESH')" :title="$t('REFRESH')"><RefreshCw :size="16" aria-hidden="true" /></Button>
      </div>
      <DashboardPlayerReview v-if="isPlayerReviewAvailable" :key="`${profile.serverAddress}/${profile.loginName}`" :profile="profile" :range="range" :refresh-key="playerRefreshKey" @busy="isPlayerBusy = $event" @ready="acceptPlayerReview" @clear="clearPlayerReview" @invalidated="emit('invalidated', $event)" />
    </header>
    <p v-if="error" role="alert">{{ $t(error) }}</p>
    <p v-if="isBusy" role="status">{{ $t('LOADING...') }}</p>
    <template v-if="activity">
      <div class="activity-charts">
        <figure>
          <figcaption>{{ $t(activity.bucket_seconds >= 86400 ? 'DAILY AVERAGE PLAYERS ONLINE' : 'HOURLY PLAYERS ONLINE') }}<ConfigHelp text="HOURLY PLAYERS ONLINE">{{ $t('Players online at each hourly check. Ranges longer than a week show daily averages.') }}</ConfigHelp></figcaption>
          <svg viewBox="0 0 744 230" role="img" :aria-label="chartSummary">
            <title>{{ chartSummary }}</title>
            <path class="activity-axis" d="M 48 26 V 190 H 720" />
            <path class="activity-grid" d="M 48 26 H 720 M 48 108 H 720" />
            <text x="40" y="30" text-anchor="end">{{ chart.maximum }}</text><text x="40" y="194" text-anchor="end">0</text>
            <path v-for="(path, index) in chart.paths" :key="index" class="activity-line" :d="path" />
            <circle v-for="point in chart.points" :key="point.at" class="activity-point" :cx="point.x" :cy="point.y" r="3"><title>{{ pointLabel(point) }}</title></circle>
            <text x="48" y="216">{{ date(activity.range_start, activity.bucket_seconds >= 86400) }}</text><text x="720" y="216" text-anchor="end">{{ date(activity.observed_at, activity.bucket_seconds >= 86400) }}</text>
          </svg>
          <p v-if="!chart.points.length">{{ $t('No online-player observations are available for this range.') }}</p>

        </figure>

        <figure v-for="history in historyCharts.slice(0, 1)" :key="history.title">
          <figcaption>{{ $t(history.title) }}<ConfigHelp :text="history.title">{{ $t(history.help) }}</ConfigHelp></figcaption>
          <svg viewBox="0 0 744 230" role="img" :aria-label="$t(history.title)">
            <title>{{ $t(history.title) }}</title>
            <path class="activity-axis" d="M 48 26 V 190 H 720" />
            <path class="activity-grid" d="M 48 26 H 720 M 48 108 H 720" />
            <text x="40" y="30" text-anchor="end">{{ number(history.maximum) }}</text><text x="40" y="194" text-anchor="end">0</text>
            <path v-for="(path, index) in history.paths" :key="index" class="activity-line" :d="path" />
            <circle v-for="point in history.points" :key="point.at" class="activity-point" :cx="point.x" :cy="point.y" r="3"><title>{{ historyPointLabel(point, history) }}</title></circle>
            <text x="48" y="216">{{ date(history.start, history.isDaily) }}</text><text x="720" y="216" text-anchor="end">{{ date(history.end, history.isDaily) }}</text>
          </svg>
          <p v-if="!history.points.length">{{ $t('No recorded observations are available for this range.') }}</p>
        </figure>
        <ControlMetric :model-value="graphScope" v-if="isLevelAvailable" personal-metric="level" :is-personal-available="isPersonalAvailable" :personal-activity="personalActivity" :personal-error="personalError" :is-personal-busy="isPersonalBusy" :range="range" @personal="loadPersonalActivity()" :is-player-review-available="isPlayerReviewAvailable" :player-activity="playerActivity" :player-label="playerLabel" :is-player-busy="isPlayerBusy">
        <figure>
          <figcaption>{{ $t('DAILY AVERAGE CROGENITOR LEVEL') }}<ConfigHelp text="DAILY AVERAGE CROGENITOR LEVEL">{{ $t('Average level of saved Crogenitors, recorded once per day.') }}</ConfigHelp></figcaption>
          <svg viewBox="0 0 744 230" role="img" :aria-label="levelChartSummary">
            <title>{{ levelChartSummary }}</title>
            <path class="activity-axis" d="M 48 26 V 190 H 720" />
            <path class="activity-grid" d="M 48 26 H 720 M 48 108 H 720" />
            <text x="40" y="30" text-anchor="end">{{ levelChart.maximum }}</text><text x="40" y="194" text-anchor="end">0</text>
            <path v-for="(path, index) in levelChart.paths" :key="index" class="activity-line" :d="path" />
            <circle v-for="point in levelChart.points" :key="point.at" class="activity-point" :cx="point.x" :cy="point.y" r="3"><title>{{ levelPointLabel(point) }}</title></circle>
            <text x="48" y="216">{{ day(levelChart.start) }}</text><text x="720" y="216" text-anchor="end">{{ day(activity.observed_at) }}</text>
          </svg>
          <p v-if="!levelChart.points.length">{{ $t('No daily Crogenitor level snapshots with a known average are available for this range.') }}</p>

        </figure>
        </ControlMetric>
        <figure v-for="history in historyCharts.slice(1)" :key="history.title">
          <figcaption>{{ $t(history.title) }}<ConfigHelp :text="history.title">{{ $t(history.help) }}</ConfigHelp></figcaption>
          <svg viewBox="0 0 744 230" role="img" :aria-label="$t(history.title)">
            <title>{{ $t(history.title) }}</title>
            <path class="activity-axis" d="M 48 26 V 190 H 720" />
            <path class="activity-grid" d="M 48 26 H 720 M 48 108 H 720" />
            <text x="40" y="30" text-anchor="end">{{ number(history.maximum) }}</text><text x="40" y="194" text-anchor="end">0</text>
            <path v-for="(path, index) in history.paths" :key="index" class="activity-line" :d="path" />
            <circle v-for="point in history.points" :key="point.at" class="activity-point" :cx="point.x" :cy="point.y" r="3"><title>{{ historyPointLabel(point, history) }}</title></circle>
            <text x="48" y="216">{{ date(history.start, history.isDaily) }}</text><text x="720" y="216" text-anchor="end">{{ date(history.end, history.isDaily) }}</text>
          </svg>
          <p v-if="!history.points.length">{{ $t('No recorded observations are available for this range.') }}</p>
        </figure>
        <PlayerStateDistributionChart :activity="activity" />
        <ControlMetric :model-value="graphScope" personal-metric="campaign" :is-personal-available="isPersonalAvailable" :personal-activity="personalActivity" :personal-error="personalError" :is-personal-busy="isPersonalBusy" :range="range" @personal="loadPersonalActivity()" :is-player-review-available="isPlayerReviewAvailable" :player-activity="playerActivity" :player-label="playerLabel" :is-player-busy="isPlayerBusy">
        <CampaignCompletionChart :activity="activity" />
        </ControlMetric>
        <ControlMetric :model-value="graphScope" personal-metric="campaign-level" :is-personal-available="isPersonalAvailable" :personal-activity="personalActivity" :personal-error="personalError" :is-personal-busy="isPersonalBusy" :range="range" @personal="loadPersonalActivity()" :is-player-review-available="isPlayerReviewAvailable" :player-activity="playerActivity" :player-label="playerLabel" :is-player-busy="isPlayerBusy">
        <CampaignLevelChart :activity="activity" />
        </ControlMetric>
        <ControlMetric :model-value="graphScope" personal-metric="mission" :is-personal-available="isPersonalAvailable" :personal-activity="personalActivity" :personal-error="personalError" :is-personal-busy="isPersonalBusy" :range="range" @personal="loadPersonalActivity()" :is-player-review-available="isPlayerReviewAvailable" :player-activity="playerActivity" :player-label="playerLabel" :is-player-busy="isPlayerBusy">
        <MissionParticipationChart :activity="activity" />
        </ControlMetric>
        <ControlMetric :model-value="graphScope" personal-metric="unresolved" :is-personal-available="isPersonalAvailable" :personal-activity="personalActivity" :personal-error="personalError" :is-personal-busy="isPersonalBusy" :range="range" @personal="loadPersonalActivity()" :is-player-review-available="isPlayerReviewAvailable" :player-activity="playerActivity" :player-label="playerLabel" :is-player-busy="isPlayerBusy">
        <MissionParticipationChart :activity="activity" is-unresolved />
        </ControlMetric>

        <ControlMetric :model-value="graphScope" v-if="isDNAAvailable" personal-metric="dna" :is-personal-available="isPersonalAvailable" :personal-activity="personalActivity" :personal-error="personalError" :is-personal-busy="isPersonalBusy" :range="range" @personal="loadPersonalActivity()" :is-player-review-available="isPlayerReviewAvailable" :player-activity="playerActivity" :player-label="playerLabel" :is-player-busy="isPlayerBusy">
        <figure>
          <figcaption>{{ $t(activity.bucket_seconds >= 86400 ? 'DAILY DNA EARNED' : 'HOURLY DNA EARNED') }}<ConfigHelp text="DNA EARNED IN RANGE">{{ $t('DNA earned from gameplay rewards and vendor sales. Shown hourly for up to a week, then as daily totals.') }}</ConfigHelp></figcaption>
          <svg viewBox="0 0 744 230" role="img" :aria-label="dnaChartSummary">
            <title>{{ dnaChartSummary }}</title>
            <path class="activity-axis" d="M 48 26 V 190 H 720" />
            <path class="activity-grid" d="M 48 26 H 720 M 48 108 H 720" />
            <text x="40" y="30" text-anchor="end">{{ number(dnaChart.maximum) }}</text><text x="40" y="194" text-anchor="end">0</text>
            <path v-for="(path, index) in dnaChart.paths" :key="index" class="activity-line" :d="path" />
            <circle v-for="point in dnaChart.points" :key="point.at" class="activity-point" :cx="point.x" :cy="point.y" r="3"><title>{{ dnaPointLabel(point) }}</title></circle>
            <text x="48" y="216">{{ date(activity.range_start, activity.bucket_seconds >= 86400) }}</text><text x="720" y="216" text-anchor="end">{{ date(activity.observed_at, activity.bucket_seconds >= 86400) }}</text>
          </svg>
          <p v-if="!dnaChart.points.length">{{ $t('No DNA earnings observations are available for this range.') }}</p>

        </figure>
        </ControlMetric>

        <ControlMetric :model-value="graphScope" v-if="isProgressionAvailable" personal-metric="progression" :is-personal-available="isPersonalAvailable" :personal-activity="personalActivity" :personal-error="personalError" :is-personal-busy="isPersonalBusy" :range="range" @personal="loadPersonalActivity()" :is-player-review-available="isPlayerReviewAvailable" :player-activity="playerActivity" :player-label="playerLabel" :is-player-busy="isPlayerBusy">
        <figure>
          <figcaption>{{ $t('DAILY AVERAGE CAMPAIGN PROGRESSION') }}<ConfigHelp text="DAILY AVERAGE CAMPAIGN PROGRESSION">{{ $t('Average highest campaign level completed across saved Crogenitors, recorded daily.') }}</ConfigHelp></figcaption>
          <svg viewBox="0 0 744 230" role="img" :aria-label="progressionChartSummary">
            <title>{{ progressionChartSummary }}</title>
            <path class="activity-axis" d="M 48 26 V 190 H 720" />
            <path class="activity-grid" d="M 48 26 H 720 M 48 108 H 720" />
            <text x="40" y="30" text-anchor="end">{{ number(progressionChart.maximum) }}</text><text x="40" y="194" text-anchor="end">0</text>
            <path v-for="(path, index) in progressionChart.paths" :key="index" class="activity-line" :d="path" />
            <circle v-for="point in progressionChart.points" :key="point.at" class="activity-point" :cx="point.x" :cy="point.y" r="3"><title>{{ progressionPointLabel(point) }}</title></circle>
            <text x="48" y="216">{{ day(progressionChart.start) }}</text><text x="720" y="216" text-anchor="end">{{ day(activity.observed_at) }}</text>
          </svg>
          <p v-if="!progressionChart.points.length">{{ $t('No daily campaign progression snapshots with a known average are available for this range.') }}</p>

        </figure>
        </ControlMetric>
        <QueueActivityChart :activity="activity" metric="queued_count" />
        <QueueActivityChart :activity="activity" metric="average_wait_seconds" />
        <QueueActivityChart :activity="activity" metric="maximum_wait_seconds" />
      </div>

    </template>
  </section>
</template>

<style scoped>
.activity-view { display:flex; flex-direction:column; gap:14px; margin:0; }
.dashboard-toolbar { position:sticky; top:0; z-index:20; display:flex; align-items:end; flex-wrap:wrap; gap:12px; padding:12px 0; border-bottom:1px solid var(--border); background:var(--background); }
/* Cover the scrollable dialog's 24px padding above and beside the header. */
.dashboard-toolbar::before { content:''; position:absolute; inset:-24px -24px 0; z-index:-1; background:var(--background); pointer-events:none; }
.activity-actions { display:flex; align-items:end; flex-wrap:wrap; gap:12px; }
.activity-range { display:flex; flex-direction:column; gap:6px; min-width:190px; font-size:11px; }
.activity-scope { display:flex; flex-direction:column; gap:6px; min-width:160px; font-size:11px; }
.activity-charts { position:relative; z-index:0; display:grid; grid-template-columns:repeat(auto-fit,minmax(min(100%,420px),1fr)); gap:16px; }
.activity-charts > figure { min-width:0; margin:0; background:var(--background); }
figure { padding:16px; border:1px solid var(--border); border-radius:8px; }
figcaption { font-size:12px; margin-bottom:12px; }
figure > svg { display:block; width:100%; height:auto; overflow:visible; }
figure > svg text { fill:var(--muted-foreground); font-size:16px; }
.activity-axis, .activity-grid { stroke:var(--border); fill:none; }
.activity-grid { stroke-dasharray:3 5; }
.activity-line { stroke:var(--primary); stroke-width:2; fill:none; }
.activity-point { fill:var(--primary); }
.activity-point:hover { r:5; }
.activity-view p { color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; }
.activity-view .activity-coverage { color:var(--foreground); border-left:3px solid var(--primary); padding-left:12px; }
</style>
