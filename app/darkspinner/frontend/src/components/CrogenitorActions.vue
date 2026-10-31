<script setup>
import { ref, watch } from 'vue'
import { Button } from '@/components/ui/button'

const props = defineProps({ disabled:{ type:Boolean, default:false }, isEditable:Boolean, isResettable:{ type:Boolean, default:true }, isDeletable:{ type:Boolean, default:true } })
const emit = defineEmits(['edit', 'reset-display', 'delete'])
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
    <Button as="summary" variant="outline" aria-label="Crogenitor actions">ACTIONS</Button>
    <div class="crogenitor-actions-list">
      <button v-if="isEditable" type="button" @click="choose('edit')">EDIT</button>
      <button v-if="isResettable" type="button" @click="choose('reset-display')">RESET DISPLAY</button>
      <button v-if="isDeletable" type="button" class="crogenitor-delete-action" @click="choose('delete')">DELETE</button>
    </div>
  </details>
</template>
