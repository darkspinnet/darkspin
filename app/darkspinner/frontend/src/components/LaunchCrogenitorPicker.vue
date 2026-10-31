<script setup>
import { computed, ref } from 'vue'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import CrogenitorActions from './CrogenitorActions.vue'
import { profileLastPlayedDate } from '../profile'

const props = defineProps({ profiles:Array, selectedKey:String, isDisabled:Boolean, isCreationDisabled:Boolean, isActionDisabled:Boolean, isRemoteAvailable:Boolean, isMod:Boolean })
const selectedProfile = computed(() => props.profiles.find(entry => entry.key === props.selectedKey)?.profile)
const emit = defineEmits(['select', 'create', 'edit', 'reset-display', 'delete'])
const newMenu = ref(null)
function create(kind) {
  newMenu.value.open = false
  emit('create', kind)
}
</script>

<template>
  <div class="pilot-select-row">
    <details ref="newMenu" class="crogenitor-new" :inert="isDisabled || isCreationDisabled">
      <Button as="summary" variant="outline" class="identity-create">+ NEW</Button>
      <div class="crogenitor-actions-list">
        <button type="button" @click="create('play')">LOCAL CROGENITOR</button>
        <button v-if="isRemoteAvailable" type="button" @click="create('remote')">REMOTE CROGENITOR</button>
        <button type="button" @click="create('mod')">MODDED CROGENITOR</button>
      </div>
    </details>
    <Select :model-value="selectedKey || undefined" :disabled="isDisabled" @update:model-value="emit('select', $event)">
      <SelectTrigger class="profile-select-trigger" aria-label="Crogenitor"><SelectValue placeholder="SELECT CROGENITOR">{{ selectedProfile?.displayName || selectedProfile?.loginName || 'SELECT CROGENITOR' }}</SelectValue></SelectTrigger>
      <SelectContent>
        <SelectItem v-for="entry in profiles" :key="entry.key" :value="entry.key" class="launch-profile-option" :data-profile-kind="entry.kind" :aria-label="`${entry.profile.displayName || entry.profile.loginName}, ${entry.kind === 'remote' ? 'Remote' : entry.kind === 'mod' ? 'Modded' : 'Local'}`">
          <span class="profile-option profile-detail-option">
            <span class="profile-option-photo"><img v-if="entry.profile.avatarUrl" :src="entry.profile.avatarUrl" alt=""></span>
            <span class="profile-option-copy"><strong>{{ entry.profile.displayName || entry.profile.loginName }}</strong><small v-if="entry.kind === 'remote'">{{ entry.profile.serverAddress }}</small><small v-if="entry.profile.isTutorialCompletionPending">Starter loadout queued</small></span>
            <span class="profile-option-level">LEVEL {{ entry.profile.crogenitorLevel }}</span>
            <small class="profile-option-last-played">LAST PLAYED {{ profileLastPlayedDate(entry.profile) }}</small>
          </span>
        </SelectItem>
      </SelectContent>
    </Select>
    <CrogenitorActions :disabled="isDisabled || isActionDisabled || !selectedKey" :is-editable="isMod" @edit="emit('edit')" @reset-display="emit('reset-display')" @delete="emit('delete')" />
  </div>
</template>

<style scoped>
.launch-profile-option .profile-option-photo { border:1px solid var(--crogenitor-local-accent); }
.launch-profile-option[data-profile-kind="remote"] .profile-option-photo { border-color:var(--crogenitor-remote-accent); }
.launch-profile-option[data-profile-kind="mod"] .profile-option-photo { border-color:var(--crogenitor-mod-accent); }
.launch-profile-option .profile-option-photo img { width:100%; height:100%; }
</style>
