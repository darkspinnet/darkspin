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

const populationSelections = [
  { key: 'agentSelection', label: 'ORDINARY AGENTS', help: 'Current co-op replacements, or stage-eligible external agents in solo and co-op. Agents replace ordinary slots; Mutation Agent hordes stay separate.', options: [['current_coop', 'Current co-op policy (default)'], ['authored_external', 'Authored external roster']] },
  { key: 'rosterSelection', label: 'ROSTER SOURCES', help: 'Keep current composition, or combine eligible local and selected external records. Empty source sides fall back to available candidates.', options: [['current_composition', 'Current composition (default)'], ['local_and_external', 'Local and external']] },
  { key: 'ownedSummonHealthScaling', label: 'OWNED SUMMON HEALTH', help: 'Resolve each child’s own profile once. Includes destructible ability objects; supplied current HP is capped to the child maximum.', options: [['run_and_party', 'Run and party (default)'], ['run_only', 'Run only'], ['base', 'Authored base']] },
  { key: 'fixtureHealthScaling', label: 'MAP DESTRUCTIBLE HEALTH', help: 'Placed map objects keep authored base health by default. Health policy does not change their class rank.', options: [['base', 'Authored base (default)'], ['run_only', 'Run only'], ['run_and_party', 'Run and party']] },
]
const populationNumbers = [
  { key: 'agentsPerSection', label: 'EXTERNAL AGENT CAP PER SECTION', min: 0, max: 3, help: '0–3; default 1. Used by authored external agent admission. Zero disables ordinary external agents.' },
  { key: 'externalRosterWeightPercent', label: 'EXTERNAL SOURCE SHARE (%)', min: 0, max: 100, help: '0–100; default 50. Source probability for local-and-external selection, followed by an eligible noun draw; not an enemy count.' },
  { key: 'ambientEliteChancePercent', label: 'AMBIENT ELITE CHANCE MULTIPLIER (%)', min: 0, max: 100, help: '0–100; default 100 uses authored stage/class chances. Uses eligible compatible affixes in name order; excludes explicit leaders, summons and fixtures.' },
]
const bossSelections = [
  { key: 'leaderSelection', label: 'FIRST-CLEAR LEADER', help: 'Authored planet bosses substitute only on their matching boss map, with one eligible candidate. Missing or ambiguous candidates retain campaign selection and log the fallback.', options: [['current_campaign', 'Current campaign catalog (default)'], ['authored_planet_boss', 'Authored planet boss']] },
  { key: 'replayLeaderSelection', label: 'REPLAY LEADER', help: 'Keep the eligible authored captain, or use the selected map’s first-clear policy for its threat tier. Encounter prerequisites still apply.', options: [['eligible_captain', 'Eligible captain (default)'], ['same_as_first_clear', 'Same as first clear']] },
]
const configuration = ref(null)
const isBusy = ref(false)
const message = ref('')
const isError = ref(false)
const isAllyAlertValid = computed(() => {
  const rules = configuration.value?.allyAlert
  return rules && Number.isInteger(rules.rangePercent) && rules.rangePercent >= 1 && rules.rangePercent <= 1000 &&
    Number.isInteger(rules.maxHops) && rules.maxHops >= 1 && rules.maxHops <= 16 &&
    ['source', 'recipient'].includes(rules.rangeOwner)
})
const isBossSpawningValid = computed(() => {
  const spawn = configuration.value?.bossSpawning
  return spawn &&
    Number.isInteger(spawn.openingAddsPerAnchor) && spawn.openingAddsPerAnchor >= 1 && spawn.openingAddsPerAnchor <= 4 &&
    Number.isInteger(spawn.initialBossAddCount) && spawn.initialBossAddCount >= 1 && spawn.initialBossAddCount <= 12 &&
    Number.isInteger(spawn.captainDelayMillisecond) && spawn.captainDelayMillisecond >= 0 && spawn.captainDelayMillisecond <= 30000 && bossSelections.every(field => field.options.some(option => option[0] === spawn[field.key]))
})
const isMutationAgentValid = computed(() => {
  const mutation = configuration.value?.mutationAgent
  return mutation && mutation.version === 1 &&
    typeof mutation.isEnabled === 'boolean' && typeof mutation.isAgentDefeatCompleting === 'boolean' &&
    Number.isInteger(mutation.minimumChainLevel) && mutation.minimumChainLevel >= 1 && mutation.minimumChainLevel <= 72 &&
    Number.isInteger(mutation.waveCount) && mutation.waveCount >= 1 && mutation.waveCount <= 10 &&
    Number.isInteger(mutation.spawnWave) && mutation.spawnWave >= 1 && mutation.spawnWave <= mutation.waveCount &&
    Number.isInteger(mutation.interWaveDelayMillisecond) && mutation.interWaveDelayMillisecond >= 0 && mutation.interWaveDelayMillisecond <= 30000
})
const isEnemyPopulationValid = computed(() => {
  const population = configuration.value?.enemyPopulation
  return population && population.version === 1 && Number.isInteger(population.enemiesPerSection) &&
    population.enemiesPerSection >= 10 && population.enemiesPerSection <= 60 &&
    typeof population.isAmbientEliteEnabled === 'boolean' && populationSelections.every(field => field.options.some(option => option[0] === population[field.key])) &&
    populationNumbers.every(field => Number.isInteger(population[field.key]) && population[field.key] >= field.min && population[field.key] <= field.max)
})
const isValid = computed(() => isAllyAlertValid.value && isBossSpawningValid.value && isMutationAgentValid.value && isEnemyPopulationValid.value)

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
    configuration.value = await SetServerRulesConfiguration({
      allyAlert: { ...configuration.value.allyAlert },
      bossSpawning: { ...configuration.value.bossSpawning },
      mutationAgent: { ...configuration.value.mutationAgent },
      enemyPopulation: { ...configuration.value.enemyPopulation },
    })
    message.value = 'Saved. Alert changes apply immediately; enemy population, boss spawning and Mutation Agent horde changes apply to new missions.'
  }
  catch (error) { isError.value = true; message.value = String(error?.message || error) }
  finally { isBusy.value = false }
}

function shareToGitHub() {
  if (isBusy.value || !isValid.value) return
  const rules = configuration.value.allyAlert
  const spawn = configuration.value.bossSpawning
  const mutation = configuration.value.mutationAgent
  const population = configuration.value.enemyPopulation
  // Explicitly export every editable rule and its section version. Never
  // include unrelated launcher configuration or authentication settings.
  const toml = [
    '[enemy_population]',
    `version = ${population.version}`,
    `enemies_per_section = ${population.enemiesPerSection}`,
    ...populationSelections.map(field => `${field.key.replace(/[A-Z]/g, letter => "_" + letter.toLowerCase())} = ${JSON.stringify(population[field.key])}`),
    ...populationNumbers.map(field => `${field.key.replace(/[A-Z]/g, letter => "_" + letter.toLowerCase())} = ${population[field.key]}`),
    `is_ambient_elite_enabled = ${population.isAmbientEliteEnabled}`,
    '',
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
    ...bossSelections.map(field => `${field.key.replace(/[A-Z]/g, letter => "_" + letter.toLowerCase())} = ${JSON.stringify(spawn[field.key])}`),
    '',
    '[mutation_agent]',
    `version = ${mutation.version}`,
    `is_enabled = ${mutation.isEnabled}`,
    `minimum_chain_level = ${mutation.minimumChainLevel}`,
    `wave_count = ${mutation.waveCount}`,
    `spawn_wave = ${mutation.spawnWave}`,
    `inter_wave_delay_millisecond = ${mutation.interWaveDelayMillisecond}`,
    `is_agent_defeat_completing = ${mutation.isAgentDefeatCompleting}`,
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
    '<!-- Describe the behavior, mission and difficulty, first clear or replay, party size, and whether you started a new mission after applying these settings. Attach a /bug report and optionally a comparison video with timestamps. -->',
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
    <p class="rules-description">Tune the local server. Alerts update immediately; population, boss and horde settings apply to new missions.</p>
    <div v-if="configuration" class="rules-sections">
      <details class="rules-group">
        <summary class="rules-group-heading">
          <span>ENEMY POPULATION</span><span class="rules-group-status" :class="{ 'rules-error': !isEnemyPopulationValid }">{{ isEnemyPopulationValid ? 'Experimental' : 'Check values' }}</span><span class="rules-chevron" aria-hidden="true"></span>
        </summary>
        <div class="rules-group-content">
          <p class="rules-description">Standing enemies along campaign routes. The current default is a server estimate, not a recovered original count. Try 18 for a modest increase from 15, then enter a new mission.</p>
          <fieldset :disabled="isBusy" class="rules-fields">
            <div class="rules-numbers">
              <label for="population-section-count"><strong>ENEMY TARGET PER ROUTE SECTION</strong><Input id="population-section-count" v-model.number="configuration.enemyPopulation.enemiesPerSection" type="number" min="10" max="60" step="1" inputmode="numeric" /><small>Default: 15; allowed: 10–60. Each map normally has three route sections. Available spawn points can limit the result. Fixed opening encounters, companions, scripted spawns, hordes and boss waves have separate counts.</small></label>
            </div>

            <div v-for="field in populationSelections" :key="field.key">
              <label :for="'population-' + field.key"><strong>{{ field.label }}</strong></label>
              <Select v-model="configuration.enemyPopulation[field.key]" :disabled="isBusy"><SelectTrigger :id="'population-' + field.key" class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="option in field.options" :key="option[0]" :value="option[0]">{{ option[1] }}</SelectItem></SelectContent></Select>
              <small>{{ field.help }}</small>
            </div>
            <label class="management-toggle"><Checkbox v-model="configuration.enemyPopulation.isAmbientEliteEnabled" :disabled="isBusy" /><span><strong>ENABLE AMBIENT ELITE PROMOTION</strong><small>Disabled by default. Dark Spin policy uses imported stage/class chances and affix bounds for ordinary minions and specials.</small></span></label>
            <div class="rules-numbers"><label v-for="field in populationNumbers" :key="field.key" :for="'population-' + field.key"><strong>{{ field.label }}</strong><Input :id="'population-' + field.key" v-model.number="configuration.enemyPopulation[field.key]" type="number" :min="field.min" :max="field.max" step="1" inputmode="numeric" /><small>{{ field.help }}</small></label></div>
          </fieldset>
          <p class="rules-description">Share these settings with the mission, difficulty, first clear or replay, party size and a bug report. Higher targets mostly add ordinary enemies; they do not multiply every encounter.</p>
        </div>
      </details>
      <details class="rules-group">
        <summary class="rules-group-heading">
          <span>BOSS SPAWNING</span><span class="rules-group-status" :class="{ 'rules-error': !isBossSpawningValid }">{{ isBossSpawningValid ? 'Experimental' : 'Check values' }}</span><span class="rules-chevron" aria-hidden="true"></span>
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

            <div v-for="field in bossSelections" :key="field.key">
              <label :for="'boss-' + field.key"><strong>{{ field.label }}</strong></label>
              <Select v-model="configuration.bossSpawning[field.key]" :disabled="isBusy"><SelectTrigger :id="'boss-' + field.key" class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="option in field.options" :key="option[0]" :value="option[0]">{{ option[1] }}</SelectItem></SelectContent></Select>
              <small>{{ field.help }}</small>
            </div>
          <p v-if="!isBossSpawningValid" class="rules-error" role="alert">Enter whole numbers within the ranges shown.</p>
        </div>
      </details>
      <details class="rules-group">
        <summary class="rules-group-heading">
          <span>MUTATION AGENT HORDES</span>
          <span class="rules-group-status" :class="{ 'rules-error': !isMutationAgentValid }">{{ !isMutationAgentValid ? 'Check values' : configuration.mutationAgent.isEnabled ? 'Enabled' : 'Disabled' }}</span>
          <span class="rules-chevron" aria-hidden="true"></span>
        </summary>
        <div class="rules-group-content" v-if="configuration.mutationAgent">
          <p class="rules-description">Control Mutation Agent eligibility and horde pacing. These defaults are server estimates. Eligibility uses completed campaign progression, including when replaying earlier missions. Below the minimum progression or with Mutation Agents disabled, ordinary horde rules apply; the delay also applies to ordinary horde follow-up waves.</p>
          <fieldset :disabled="isBusy" class="rules-fields">
            <label class="management-toggle"><Checkbox v-model="configuration.mutationAgent.isEnabled" :disabled="isBusy" /><span><strong>ENABLE MUTATION AGENTS</strong><small>Allow Mutation Agents in all missions after reaching the minimum completed campaign progression.</small></span></label>
            <div class="rules-numbers">
              <label for="mutation-minimum-chain"><strong>MINIMUM COMPLETED PROGRESSION</strong><Input id="mutation-minimum-chain" v-model.number="configuration.mutationAgent.minimumChainLevel" type="number" min="1" max="72" step="1" inputmode="numeric" /><small>Completed campaign index from 1–72. Default: 7 (complete mission 2-3 once).</small></label>
              <label for="mutation-wave-count"><strong>HORDE WAVE COUNT</strong><Input id="mutation-wave-count" v-model.number="configuration.mutationAgent.waveCount" type="number" min="1" max="10" step="1" inputmode="numeric" /><small>Allowed: 1–10. Default: 3.</small></label>
              <label for="mutation-spawn-wave"><strong>MUTATION AGENT SPAWN WAVE</strong><Input id="mutation-spawn-wave" v-model.number="configuration.mutationAgent.spawnWave" type="number" min="1" :max="configuration.mutationAgent.waveCount" step="1" inputmode="numeric" /><small>Must be between 1 and the horde wave count. Default: 3.</small></label>
              <label for="mutation-wave-delay"><strong>BETWEEN WAVES (MS)</strong><Input id="mutation-wave-delay" v-model.number="configuration.mutationAgent.interWaveDelayMillisecond" type="number" min="0" max="30000" step="1" inputmode="numeric" /><small>Delay before the next wave begins. Allowed: 0–30000. Default: 1500.</small></label>
            </div>
            <label class="management-toggle"><Checkbox v-model="configuration.mutationAgent.isAgentDefeatCompleting" :disabled="isBusy" /><span><strong>AGENT DEFEAT COMPLETES HORDE</strong><small>Finish the horde when its Mutation Agent is defeated. When disabled, continue clearing the configured waves.</small></span></label>
          </fieldset>
          <p v-if="!isMutationAgentValid" class="rules-error" role="alert">Enter whole numbers within the ranges shown. The spawn wave cannot exceed the horde wave count. Reload if the settings version has changed.</p>
        </div>
        <p v-else class="rules-description rules-group-content">Mutation Agent settings are unavailable. Update the local server and reload.</p>
      </details>
      <details class="rules-group">
        <summary class="rules-group-heading">
          <span>ALLY ALERT</span>
          <span class="rules-group-status" :class="{ 'rules-error':!isAllyAlertValid }">{{ !isAllyAlertValid ? 'Check values' : configuration.allyAlert.isEnabled ? 'Enabled' : 'Disabled' }}</span>
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
      <p v-if="!isAllyAlertValid" class="rules-error" role="alert">Enter whole numbers within the ranges shown.</p>
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
