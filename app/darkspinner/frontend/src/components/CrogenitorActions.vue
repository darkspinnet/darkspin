<script setup>
import { ref, watch } from 'vue'

const props = defineProps({ disabled:{ type:Boolean, default:false } })
const emit = defineEmits(['reset-display', 'delete'])
const menu = ref(null)

watch(() => props.disabled, isDisabled => {
  if (isDisabled && menu.value) menu.value.open = false
})

function choose(action) {
  if (props.disabled) return
  if (menu.value) menu.value.open = false
  emit(action)
}
</script>

<template>
  <details ref="menu" class="crogenitor-actions" :inert="disabled" :aria-disabled="disabled">
    <summary aria-label="Crogenitor actions">ACTIONS <span aria-hidden="true">⌄</span></summary>
    <div class="crogenitor-actions-list">
      <button type="button" @click="choose('reset-display')">RESET DISPLAY</button>
      <button type="button" class="crogenitor-delete-action" @click="choose('delete')">DELETE</button>
    </div>
  </details>
</template>
