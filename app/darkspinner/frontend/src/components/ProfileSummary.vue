<script setup>
import { computed } from 'vue'
import { campaignDetailFor, campaignLabelFor, campaignLevelFor, profileCreateDate } from '../profile'

const props = defineProps({ profile:{type:Object, required:true}, label:{type:String, default:'LOCAL CROGENITOR'} })
const displayName = computed(() => props.profile.displayName || props.profile.loginName)
const campaignLevel = computed(() => campaignLevelFor(props.profile))
</script>

<template>
  <div class="profile-info-card" :aria-label="`${displayName} profile summary`">
    <time v-if="profileCreateDate(profile)" class="profile-create-date" :datetime="profile.createDt">{{ profileCreateDate(profile) }}</time>
    <div class="selected-profile-avatar-frame">
      <div class="selected-profile-avatar" :class="{ empty:!profile.avatarUrl }">
        <img v-if="profile.avatarUrl" :src="profile.avatarUrl" :alt="`${displayName} Crogenitor photo`">
      </div>
      <span class="profile-level-badge" :aria-label="`Crogenitor level ${profile.crogenitorLevel}`">{{ profile.crogenitorLevel }}</span>
    </div>
    <div class="profile-info-content">
      <div class="profile-info-heading"><small>{{ label }}</small><strong>{{ displayName }}</strong></div>
      <div class="profile-progress">
        <span><small>CROGENITOR</small><strong>{{ profile.crogenitorLevel }}</strong><em>{{ profile.cumulativeXp }} XP</em></span>
        <span><small>PROGRESS</small><strong>{{ campaignLabelFor(campaignLevel) }}</strong><em>{{ campaignDetailFor(campaignLevel) }}</em></span>
      </div>
    </div>
  </div>
</template>
