<script setup>
import { computed, onMounted, ref } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { DialogTitle } from '@/components/ui/dialog'
import LauncherDialog from '@/components/LauncherDialog.vue'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { BrowserOpenURL, GetServerRulesConfiguration, SetServerRulesConfiguration } from '../platform'

const props = defineProps({ buildVersion: { type: String, default: '' } })
const emit = defineEmits(['close', 'interact'])

const configuration = ref(null)
const isBusy = ref(false)
const message = ref('')
const isError = ref(false)
const isValid = computed(() => {
  const rules = configuration.value?.allyAlert
  const spawn = configuration.value?.bossSpawning
  return rules && Number.isInteger(rules.rangePercent) && rules.rangePercent >= 1 && rules.rangePercent <= 1000 &&
    Number.isInteger(rules.maxHops) && rules.maxHops >= 1 && rules.maxHops <= 16 &&
    ['source', 'recipient'].includes(rules.rangeOwner) && spawn &&
    Number.isInteger(spawn.openingAddsPerAnchor) && spawn.openingAddsPerAnchor >= 1 && spawn.openingAddsPerAnchor <= 4 &&
    Number.isInteger(spawn.initialBossAddCount) && spawn.initialBossAddCount >= 1 && spawn.initialBossAddCount <= 12 &&
    Number.isInteger(spawn.captainDelayMillisecond) && spawn.captainDelayMillisecond >= 0 && spawn.captainDelayMillisecond <= 30000
})

async function reload() {
  isBusy.value = true
  isError.value = false
  message.value = ''
  try { configuration.value = await GetServerRulesConfiguration() }
  catch (error) { isError.value = true; message.value = String(error?.message || error) }
  finally { isBusy.value = false }
}

async function save() {
  if (isBusy.value || !isValid.value) return
  isBusy.value = true
  isError.value = false
  message.value = 'Applying server rules…'
  try {
    // Include the backend section version so stale views cannot restore old overrides.
    configuration.value = await SetServerRulesConfiguration({ allyAlert: { ...configuration.value.allyAlert }, bossSpawning: { ...configuration.value.bossSpawning } })
    message.value = 'Saved. Alert changes apply immediately; boss spawning changes apply to new missions.'
  }
  catch (error) { isError.value = true; message.value = String(error?.message || error) }
  finally { isBusy.value = false }
}

function shareToGitHub() {
  if (isBusy.value || !isValid.value) return
  const rules = configuration.value.allyAlert
  const spawn = configuration.value.bossSpawning
  // Explicitly export every editable rule and its section version. Never
  // include unrelated launcher configuration or authentication settings.
  const toml = [
    '[ally_alert]',
    `version = ${rules.version}`,
    `is_enabled = ${rules.isEnabled}`,
    `range_percent = ${rules.rangePercent}`,
    `range_owner = ${JSON.stringify(rules.rangeOwner)}`,
    `max_hops = ${rules.maxHops}`,
    `is_diagnostic_logging_enabled = ${rules.isDiagnosticLoggingEnabled}`,
    '',
    '[boss_spawning]',
    `version = ${spawn.version}`,
    `opening_adds_per_anchor = ${spawn.openingAddsPerAnchor}`,
    `initial_boss_add_count = ${spawn.initialBossAddCount}`,
    `captain_delay_millisecond = ${spawn.captainDelayMillisecond}`,
  ].join('\n')
  const body = [
    '### Server rules',
    '',
    'Settings currently shown in the Server Rules dialog; unapplied edits may be included.',
    '',
    '```toml',
    toml,
    '```',
    '',
    `Darkspinner build: ${props.buildVersion || 'Unknown'}`,
    '',
    '### Observations',
    '',
    '<!-- Describe the behavior and optionally link a comparison video. -->',
  ].join('\n')
  const url = new URL('https://github.com/darkspinnet/darkspin/issues/new')
  url.searchParams.set('title', 'Server rules feedback')
  url.searchParams.set('body', body)
  isError.value = false
  try {
    BrowserOpenURL(url.href)
    message.value = 'GitHub opened with these settings. Review and submit the new issue there.'
  }
  catch (error) { isError.value = true; message.value = String(error?.message || error) }
}

onMounted(reload)
</script>

<template>
  <LauncherDialog :is-dismissible="!isBusy" @close="emit('close')" @interact="emit('interact', $event)">
    <section class="server-rules-dialog">
    <header class="rules-heading">
      <DialogTitle as="h2">SERVER RULES</DialogTitle>
      <Button variant="outline" type="button" :disabled="isBusy || !isValid" @click="shareToGitHub">Share to GitHub</Button>
    </header>
    <p class="rules-description">Tune the local server. Alerts update immediately; boss spawning settings apply to new missions.</p>
    <div v-if="configuration" class="rules-sections">
      <details class="rules-group">
        <summary class="rules-group-heading">
          <span>BOSS SPAWNING</span><span class="rules-group-status">Experimental</span><span class="rules-chevron" aria-hidden="true"></span>
        </summary>
        <div class="rules-group-content">
          <p class="rules-description">Reconstructed wave sizes and pacing. These defaults are server estimates; exact original wave counts and timing are not recovered. Authored enemy eligibility and spawn anchors still apply.</p>
          <fieldset :disabled="isBusy" class="rules-fields">
            <div class="rules-numbers">
              <label for="boss-opening-count"><strong>OPENING ADDS PER ANCHOR</strong><Input id="boss-opening-count" v-model.number="configuration.bossSpawning.openingAddsPerAnchor" type="number" min="1" max="4" step="1" inputmode="numeric" /><small>1–4 enemies per authored anchor in captain arenas. Default: 1; Illust has four anchors.</small></label>
              <label for="boss-followup-count"><strong>ADDS ALONGSIDE ILLUST</strong><Input id="boss-followup-count" v-model.number="configuration.bossSpawning.initialBossAddCount" type="number" min="1" max="12" step="1" inputmode="numeric" /><small>1–12 enemies in the follow-up wave of mission 1-1. Default: 2.</small></label>
              <label for="boss-captain-delay"><strong>CAPTAIN DELAY (MS)</strong><Input id="boss-captain-delay" v-model.number="configuration.bossSpawning.captainDelayMillisecond" type="number" min="0" max="30000" step="100" inputmode="numeric" /><small>Delay after clearing the opening wave before the captain enters. Default: 2000; allowed: 0–30000.</small></label>
            </div>
          </fieldset>
        </div>
      </details>
      <details class="rules-group">
        <summary class="rules-group-heading">
          <span>ALLY ALERT</span>
          <span class="rules-group-status" :class="{ 'rules-error':!isValid }">{{ !isValid ? 'Check values' : configuration.allyAlert.isEnabled ? 'Enabled' : 'Disabled' }}</span>
          <span class="rules-chevron" aria-hidden="true"></span>
        </summary>
        <div class="rules-group-content">
      <p class="rules-description">Experimental reconstruction of how nearby enemies share a target.</p>
      <fieldset :disabled="isBusy" class="rules-fields">
        <label class="management-toggle"><Checkbox v-model="configuration.allyAlert.isEnabled" :disabled="isBusy" /><span><strong>SHARE ENEMY ATTENTION</strong><small>Idle allies can join a nearby enemy's fight. Existing targets stay unchanged.</small></span></label>
        <div class="rules-numbers">
          <label for="ally-alert-range"><strong>ALERT RANGE (%)</strong><Input id="ally-alert-range" v-model.number="configuration.allyAlert.rangePercent" type="number" min="1" max="1000" step="1" inputmode="numeric" /><small>100 uses the authored range. Allowed: 1–1000.</small></label>
          <label for="ally-alert-hops"><strong>ALERT HOPS</strong><Input id="ally-alert-hops" v-model.number="configuration.allyAlert.maxHops" type="number" min="1" max="16" step="1" inputmode="numeric" /><small>1 alerts neighbors; 2 lets them alert another wave. Maximum: 16.</small></label>
        </div>
        <div>
          <label for="ally-alert-owner"><strong>WHOSE RANGE TO USE</strong></label>
          <Select v-model="configuration.allyAlert.rangeOwner" :disabled="isBusy">
            <SelectTrigger id="ally-alert-owner" class="w-full"><SelectValue /></SelectTrigger>
            <SelectContent><SelectItem value="recipient">Ally receiving the alert</SelectItem><SelectItem value="source">Enemy sending the alert</SelectItem></SelectContent>
          </Select>
        </div>
        <label class="management-toggle"><Checkbox v-model="configuration.allyAlert.isDiagnosticLoggingEnabled" :disabled="isBusy" /><span><strong>LOG ALERT DETAILS</strong><small>Record participants, distance and hops for comparisons with gameplay recordings.</small></span></label>
      </fieldset>
      <p v-if="!isValid" class="rules-error" role="alert">Enter whole numbers within the ranges shown.</p>
        </div>
      </details>
    </div>
    <p v-if="message" class="management-message" :class="{ 'rules-error':isError }" :role="isError ? 'alert' : 'status'" aria-live="polite">{{ message }}</p>
    <div class="rules-actions">
      <Button type="button" :disabled="isBusy || !isValid" @click="save">{{ isBusy ? 'PLEASE WAIT…' : 'APPLY SERVER RULES' }}</Button>
      <Button variant="outline" type="button" :disabled="isBusy" @click="reload">RELOAD</Button>
      <Button variant="outline" class="rules-close" type="button" :disabled="isBusy" @click="emit('close')">CLOSE</Button>
    </div>
    <p class="rules-description rules-footnote">Validated updates may reset changed sections to new defaults.</p>
    </section>
  </LauncherDialog>
</template>

<style scoped>
.rules-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; margin-bottom: 12px; }
.rules-heading h2 { font-size: 16px; font-weight: 700; margin: 0; }
.rules-heading > button { margin-left: auto; }
.rules-sections { display: grid; gap: 12px; margin-top: 18px; max-height: min(52dvh, 440px); overflow-y: auto; overscroll-behavior: contain; scrollbar-gutter: stable; }
.rules-group { min-width: 0; border: 1px solid var(--border); border-radius: 8px; }
.rules-group-heading { display: flex; align-items: center; gap: 12px; padding: 14px; cursor: pointer; list-style: none; font-size: 12px; font-weight: 700; border-radius: 8px; }
.rules-group-heading::-webkit-details-marker { display: none; }
.rules-group-heading:hover { background: var(--muted); }
.rules-group-heading:focus-visible { outline: 2px solid var(--ring); outline-offset: -2px; }
.rules-group-status { margin-left: auto; color: var(--muted-foreground); font-size: 11px; font-weight: 400; }
.rules-chevron { width: 7px; height: 7px; flex-shrink: 0; border-right: 2px solid currentColor; border-bottom: 2px solid currentColor; transform: rotate(-45deg); transition: transform 150ms ease; }
.rules-group[open] .rules-chevron { transform: rotate(45deg); }
.rules-group-content { padding: 4px 14px 14px; }
.rules-description, small { color: var(--muted-foreground); font-size: 12px; line-height: 1.5; }
.rules-fields { border: 0; padding: 0; margin: 18px 0 0; display: grid; gap: 18px; }
.rules-numbers { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.rules-numbers label { display: grid; gap: 6px; }
strong { font-size: 11px; display: block; margin-bottom: 6px; }
.rules-actions { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 18px; }
.rules-close { margin-left: auto; }
.rules-error { color: var(--destructive); }
.rules-footnote { margin-top: 12px; }
@media (max-width: 560px) { .rules-numbers { grid-template-columns: 1fr; } }
@media (prefers-reduced-motion: reduce) { .rules-chevron { transition: none; } }
</style>
