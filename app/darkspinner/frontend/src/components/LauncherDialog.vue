<script setup>
import { Dialog, DialogContent } from '@/components/ui/dialog'

defineProps({ isDismissible: { type: Boolean, default: false }, width: { type:Number, default:null }, height: { type:Number, default:null } })
const emit = defineEmits(['close', 'interact', 'openAutoFocus'])
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
      :style="{ ...(width ? { width:`min(${width}px, calc(100% - 32px))`, maxWidth:`${width}px` } : {}), ...(height ? { height:`min(${height}px, calc(100dvh - 32px))` } : {}) }"
      :show-close-button="false"
      :aria-describedby="undefined"
      @open-auto-focus="emit('openAutoFocus', $event)"
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
