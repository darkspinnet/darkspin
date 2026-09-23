<script setup>
import { Dialog, DialogContent } from '@/components/ui/dialog'

defineProps({ isDismissible: { type: Boolean, default: false } })
const emit = defineEmits(['close', 'interact'])
const previousFocus = document.activeElement

function restoreFocus(event) {
  event.preventDefault()
  if (previousFocus?.isConnected) previousFocus.focus()
}
</script>

<template>
  <Dialog :open="true" @update:open="isDismissible && emit('close')">
    <DialogContent
      class="launcher-dialog"
      :show-close-button="false"
      :aria-describedby="undefined"
      @escape-key-down="!isDismissible && $event.preventDefault()"
      @interact-outside="!isDismissible && $event.preventDefault()"
      @close-auto-focus="restoreFocus"
      @pointerdown.capture="emit('interact', $event)"
      @keydown.capture="emit('interact', $event)"
    >
      <slot />
    </DialogContent>
  </Dialog>
</template>
