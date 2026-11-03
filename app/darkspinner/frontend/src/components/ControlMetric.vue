<script setup>
import { computed, watch } from 'vue'
import RemoteControlPersonalActivity from './RemoteControlPersonalActivity.vue'

const props = defineProps({
  personalMetric:String, personalActivity:Object, personalError:String,
  isPersonalAvailable:Boolean, isPersonalBusy:Boolean,
  isServerAvailable:{ type:Boolean, default:true }, range:String, modelValue:String,
  isPlayerReviewAvailable:Boolean, playerActivity:Object, playerLabel:String, isPlayerBusy:Boolean,
})
const emit = defineEmits(['personal'])
const scope = computed(() => props.modelValue === 'player' && !props.isPlayerReviewAvailable ? 'server' : props.modelValue || (props.isServerAvailable ? 'server' : 'personal'))
watch(() => `${scope.value}/${props.range}`, () => {
  if (scope.value === 'personal') emit('personal')
}, { immediate:true })
</script>

<template>
  <div class="control-metric">
    <template v-if="scope === 'server'"><slot /></template>
    <template v-else-if="scope === 'personal'">
      <p v-if="personalError" role="alert">{{ $t(personalError) }}</p>
      <p v-else-if="isPersonalBusy || !personalActivity" role="status">{{ $t('LOADING...') }}</p>
      <RemoteControlPersonalActivity v-else is-embedded :metric="personalMetric" :supplied-activity="personalActivity" :range="range" />
    </template>
    <template v-else-if="isPlayerReviewAvailable">
      <p v-if="isPlayerBusy" role="status">{{ $t('LOADING...') }}</p>
      <template v-else-if="playerActivity">
        <p class="player-label">{{ playerLabel }}</p>
        <RemoteControlPersonalActivity is-embedded :metric="personalMetric" :supplied-activity="playerActivity" :range="range" />
      </template>
      <p v-else>{{ $t('Select a Crogenitor above.') }}</p>
    </template>
  </div>
</template>

<style scoped>
.control-metric { min-width:0; display:flex; flex-direction:column; padding:16px; border:1px solid var(--border); border-radius:8px; background:var(--background); }
.control-metric :deep(figure) { padding:0; border:0; border-radius:0; margin:0; }
.control-metric p { color:var(--muted-foreground); font-size:12px; }
.player-label { margin-bottom:10px; }
</style>
