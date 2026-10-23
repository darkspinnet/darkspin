<script setup>
import { ref, watch } from 'vue'
import { GetModPortrait } from '../platform'

const props = defineProps({ creationId:String, label:String })
const url = ref('')
const portraits = new Map()
watch(() => props.creationId, async id => {
  url.value = ''
  if (!id) return
  if (portraits.has(id)) { url.value = portraits.get(id); return }
  try {
    const portrait = await GetModPortrait(id)
    portraits.set(id, portrait)
    if (props.creationId === id) url.value = portrait
  } catch { /* Keep a labelled placeholder for creations without an image. */ }
}, { immediate:true })
</script>

<template>
  <img v-if="url" :src="url" :alt="label || 'Creature'" class="mod-portrait">
  <span v-else class="mod-portrait mod-portrait-placeholder" :aria-label="label || 'Portrait unavailable'">?</span>
</template>

<style scoped>
.mod-portrait { display:inline-flex; width:44px; height:44px; object-fit:contain; flex-shrink:0; border-radius:4px; background:#0b1520; }
.mod-portrait-placeholder { align-items:center; justify-content:center; color:#718697; }
</style>
