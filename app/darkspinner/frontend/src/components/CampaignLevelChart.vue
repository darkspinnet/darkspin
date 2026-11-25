<script setup>
import { computed } from 'vue'
import { translate } from '../localization'
import ConfigHelp from './ConfigHelp.vue'

const props = defineProps({ activity:{ type:Object, required:true }, isPersonal:Boolean })
const title = computed(() => props.isPersonal ? 'PERSONAL MOST COMPLETED CAMPAIGN LEVELS' : 'MOST COMPLETED CAMPAIGN LEVELS')
const isKnown = computed(() => Array.isArray(props.activity.campaign_levels))
const levels = computed(() => (isKnown.value ? props.activity.campaign_levels : []).filter(level =>
  Number.isSafeInteger(level.campaign_number) && level.campaign_number > 0 &&
  Number.isSafeInteger(level.level_number) && level.level_number > 0 &&
  Number.isSafeInteger(level.completion_count) && level.completion_count > 0 &&
  Number.isSafeInteger(level.repeat_count) && level.repeat_count >= 0 && level.repeat_count <= level.completion_count
).slice(0, 10))
const maximum = computed(() => Math.max(1, ...levels.value.map(level => level.completion_count)))
function number(count) { return count.toLocaleString() }
function levelLabel(level) { return translate('Campaign {0}-{1}', [level.campaign_number, level.level_number]) }
function completionLabel(level) {
  return translate('{0} successful completions; {1} repeat completions', [number(level.completion_count), number(level.repeat_count)])
}
</script>

<template>
  <figure class="campaign-level-chart">
    <figcaption>{{ $t(title) }}<ConfigHelp :text="title">{{ $t('The ten campaign levels completed most often. The smaller bar shows repeat completions within the total.') }}</ConfigHelp></figcaption>
    <div class="campaign-level-legend"><span><i aria-hidden="true" />{{ $t('ALL SUCCESSFUL COMPLETIONS') }}</span><span><i class="repeat-swatch" aria-hidden="true" />{{ $t('REPEAT COMPLETIONS') }}</span></div>
    <ol v-if="levels.length" class="campaign-levels">
      <li v-for="level in levels" :key="`${level.campaign_number}-${level.level_number}`">
        <span class="campaign-level-name">{{ levelLabel(level) }}</span>
        <div class="campaign-level-bars" role="img" :aria-label="`${levelLabel(level)}: ${completionLabel(level)}`">
          <span class="completion-bar" :style="{ width:`${level.completion_count / maximum * 100}%` }" />
          <span class="repeat-bar" :style="{ width:`${level.repeat_count / maximum * 100}%` }" />
        </div>
        <span class="campaign-level-count">{{ $t('{0} total / {1} repeat', [number(level.completion_count), number(level.repeat_count)]) }}</span>
      </li>
    </ol>
    <p v-else>{{ $t(isKnown ? 'No recorded completions.' : 'Campaign level completion statistics are unavailable from this server.') }}</p>
  </figure>
</template>

<style scoped>
.campaign-level-chart { min-width:0; margin:0; padding:14px; border:1px solid var(--border); border-radius:8px; background:var(--background); }
figcaption { font-size:12px; text-align:left; margin-bottom:10px; }
p { color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; }
.campaign-level-legend { display:flex; flex-wrap:wrap; gap:14px; margin-bottom:14px; font-size:11px; color:var(--muted-foreground); }
.campaign-level-legend span { display:flex; align-items:center; gap:6px; }
.campaign-level-legend i { display:inline-block; width:22px; height:10px; background:var(--primary); }
.campaign-level-legend .repeat-swatch { height:4px; background:var(--foreground); }
.campaign-levels { list-style:none; padding:0; margin:0; display:grid; gap:12px; }
.campaign-levels li { display:grid; grid-template-columns:minmax(80px, auto) minmax(30px, 1fr) auto; align-items:center; gap:10px; }
.campaign-level-name, .campaign-level-count { font-size:12px; font-variant-numeric:tabular-nums; }
.campaign-level-count { color:var(--muted-foreground); }
.campaign-level-bars { position:relative; height:18px; background:var(--muted); }
.completion-bar, .repeat-bar { display:block; position:absolute; left:0; background:var(--primary); height:18px; }
.repeat-bar { top:7px; height:4px; background:var(--foreground); }
@media (max-width:600px) {
  .campaign-levels li { grid-template-columns:auto minmax(30px, 1fr); gap:6px 10px; }
  .campaign-level-count { grid-column:2; }
}
</style>
