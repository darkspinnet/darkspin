<script setup>
import { TooltipArrow, TooltipContent, TooltipPortal, TooltipProvider, TooltipRoot, TooltipTrigger } from 'reka-ui'

defineProps({ text: { type: String, required: true } })
</script>

<template>
  <TooltipProvider :delay-duration="250">
    <TooltipRoot>
      <TooltipTrigger as-child>
        <span class="config-help" tabindex="0" role="img" :aria-label="$t(text)" @click.stop.prevent>?</span>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent class="config-help-content" side="top" :side-offset="8" :collision-padding="12">
          {{ $t(text) }}
          <TooltipArrow class="config-help-arrow" :width="10" :height="5" />
        </TooltipContent>
      </TooltipPortal>
    </TooltipRoot>
  </TooltipProvider>
</template>

<style scoped>
.config-help { display: inline-flex; align-items: center; justify-content: center; width: 14px; height: 14px; margin-left: 6px; border: 1px solid currentColor; border-radius: 50%; color: var(--muted-foreground); font-size: 10px; font-weight: 600; line-height: 1; letter-spacing: 0; vertical-align: middle; cursor: help; }
.config-help:hover, .config-help:focus-visible { color: var(--primary); }
.config-help:focus-visible { outline: 2px solid var(--ring); outline-offset: 3px; }
.config-help-content { z-index: 150; max-width: min(300px, calc(100vw - 24px)); padding: 10px 12px; border: 1px solid var(--border); border-radius: 8px; background: var(--popover); color: var(--popover-foreground); box-shadow: 0 6px 20px #0006; font-size: 12px; font-weight: 400; line-height: 1.5; letter-spacing: normal; overflow-wrap: anywhere; }
.config-help-arrow { fill: var(--popover); }
</style>
