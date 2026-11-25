<script setup>
import { ref, watch } from 'vue'
import { Button } from '@/components/ui/button'

const props = defineProps({ disabled:{ type:Boolean, default:false }, isMutationDisabled:Boolean, isControlPanelAvailable:Boolean, isEditable:Boolean, isResettable:{ type:Boolean, default:true }, isDeletable:{ type:Boolean, default:true }, isShiftHeld:Boolean })
const emit = defineEmits(['control-panel', 'edit', 'reset-display', 'delete'])
const menu = ref(null)

watch(() => props.disabled, isDisabled => {
  if (isDisabled && menu.value) menu.value.open = false
})

function choose(action, event) {
  if (props.disabled || (action !== 'control-panel' && props.isMutationDisabled)) return
  if (menu.value) menu.value.open = false
  emit(action, action === 'delete' && (props.isShiftHeld || !!event?.shiftKey))
}
</script>

<template>
  <details ref="menu" class="crogenitor-actions" :inert="disabled" :aria-disabled="disabled">
    <Button as="summary" variant="outline" :aria-label='$t("Crogenitor actions")'>{{ $t("ACTIONS") }}</Button>
    <div class="crogenitor-actions-list">
      <button v-if="isControlPanelAvailable" type="button" @click="choose('control-panel')">{{ $t("CONTROL PANEL") }}</button>
      <button v-if="isEditable" type="button" :disabled="isMutationDisabled" @click="choose('edit')">{{ $t("EDIT") }}</button>
      <button v-if="isResettable" type="button" :disabled="isMutationDisabled" @click="choose('reset-display')">{{ $t("RESET DISPLAY") }}</button>
      <button v-if="isDeletable" type="button" class="crogenitor-delete-action" :disabled="isMutationDisabled" @click="choose('delete', $event)">{{ $t(isShiftHeld ? 'LOCAL DELETE' : 'DELETE') }}</button>
    </div>
  </details>
</template>

<style scoped>
.crogenitor-actions-list > button:disabled { opacity:0.5; cursor:default; background:transparent; }
</style>
