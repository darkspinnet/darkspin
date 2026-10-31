<script setup>
import { computed } from 'vue'
import { campaignDetailFor, campaignLabelFor, campaignLevelFor, profileCreateDate } from '../profile'

const props = defineProps({ profile:{type:Object, required:true}, label:{type:String, default:'LOCAL CROGENITOR'} })
const displayName = computed(() => props.profile.displayName || props.profile.loginName)
const campaignLevel = computed(() => campaignLevelFor(props.profile))
</script>

<template>
  <div class="profile-info-card" :aria-label="$t(`${displayName} profile summary`)">
    <time v-if="profileCreateDate(profile)" class="profile-create-date" :datetime="profile.createDt">{{ $t(profileCreateDate(profile)) }}</time>
    <div class="selected-profile-avatar-frame">
      <div class="selected-profile-avatar" :class="{ empty:!profile.avatarUrl }">
        <img v-if="profile.avatarUrl" :src="profile.avatarUrl" :alt="$t(`${displayName} Crogenitor photo`)">
      </div>
      <span class="profile-level-badge" :aria-label="$t(`Crogenitor level ${profile.crogenitorLevel}`)">{{ $t(profile.crogenitorLevel) }}</span>
    </div>
    <div class="profile-info-content">
      <div class="profile-info-heading"><small>{{ $t(label) }}</small><strong>{{ displayName }}</strong></div>
      <div class="profile-progress">
        <span><small>{{ $t("CROGENITOR") }}</small><strong>{{ $t(profile.crogenitorLevel) }}</strong><em>{{ $t(profile.cumulativeXp) }} XP</em></span>
        <span><small>{{ $t("PROGRESS") }}</small><strong>{{ $t(campaignLabelFor(campaignLevel)) }}</strong><em>{{ $t(campaignDetailFor(campaignLevel)) }}</em></span>
      </div>
    </div>
  </div>
</template>
