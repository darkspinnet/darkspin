<script setup>
import { computed, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { Card } from '@/components/ui/card'
import { DialogTitle } from '@/components/ui/dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import LauncherDialog from './LauncherDialog.vue'
import ModPortrait from './ModPortrait.vue'
import ProfileSummary from './ProfileSummary.vue'
import { GetModConfiguration, GetProfileAvatars, LaunchModProfile, SaveModProfile } from '../platform'

const props = defineProps({ status:Object, isActive:Boolean })
const emit = defineEmits(['readiness', 'launch', 'configure', 'profiles', 'selection', 'busy'])
const configuration = ref({ sporePath:'', creatures:[], heroes:[], profiles:[], sourceError:'' })
const avatars = ref([])
const retainedLaunchKey = localStorage.getItem('darkspinner.launchProfile') || ''
const selectedProfile = ref(retainedLaunchKey.startsWith('mod:') ? retainedLaunchKey.slice(4) : localStorage.getItem('darkspinner.modProfile') || '')
const bindings = ref([])
const selectedCreation = ref('')
const selectedHero = ref('1667741389')
const selectedCategory = ref('Hero')
const characterSearch = ref('')
const search = ref('')
const isBusy = ref(false)
const isConfigurationValid = ref(false)
const error = ref('')
const isNew = ref(false)
const isEditing = ref(false)
const wizardStep = ref('profile')
const name = ref('')
const avatarID = ref(1)
const isTutorialSkipped = ref(true)
const visibleCreatures = computed(() => configuration.value.creatures.filter(creature => creature.name.toLowerCase().includes(search.value.toLowerCase())).slice(0, 60))
const profile = computed(() => configuration.value.profiles.find(candidate => candidate.loginName === selectedProfile.value))
const creation = computed(() => configuration.value.creatures.find(candidate => candidate.id === selectedCreation.value))
const hero = computed(() => configuration.value.heroes.find(candidate => String(candidate.noun) === selectedHero.value))
const categories = computed(() => ['Hero','Minion','Lieutenant','Captain','Elite','Boss','Agent','Support','Pet','Neutral','Destructible','Other'].filter(category => ['Hero','Minion','Captain','Elite'].includes(category) || configuration.value.heroes.some(character => character.category === category)))
const visibleCharacters = computed(() => configuration.value.heroes.filter(character => character.category === selectedCategory.value && `${character.name} ${character.nounName}`.toLowerCase().includes(characterSearch.value.toLowerCase())))
watch(selectedCategory, () => {
  characterSearch.value = ''
  selectedHero.value = selectedCategory.value === 'Hero' ? '1667741389' : String(configuration.value.heroes.find(character => character.category === selectedCategory.value && !bindings.value.some(binding => binding.heroNoun === character.noun))?.noun || '')
})

const isLaunchReady = computed(() => props.isActive && isConfigurationValid.value && !!profile.value && !isEditing.value && !isBusy.value && props.status?.isGameReady && props.status?.isContentReady && props.status?.isPatchComplete && !['running','launching','stopping'].includes(props.status?.state))
const isProfileReady = computed(() => /^[A-Za-z0-9]{1,20}$/.test(name.value) && avatars.value.some(avatar => avatar.id === avatarID.value))
watch([isBusy, isEditing], () => emit('busy', isBusy.value || isEditing.value))
watch(isLaunchReady, isReady => emit('readiness', isReady), { immediate:true })

async function launch() {
  if (!isLaunchReady.value) return
  await perform(async () => {
    await LaunchModProfile(selectedProfile.value)
    const connectionDT = new Date().toISOString()
    configuration.value.profiles = configuration.value.profiles.map(candidate =>
      candidate.loginName === selectedProfile.value ? { ...candidate, lastConnectionDt:connectionDT } : candidate,
    )
    emit('profiles', configuration.value.profiles)
    emit('launch')
  })
}
function selectProfile(loginName) {
  selectedProfile.value = loginName
  loadProfile()
}
defineExpose({ launch, newProfile, editProfile, selectProfile, refresh })

function heroFor(noun) { return configuration.value.heroes.find(candidate => candidate.noun === noun) }
function creatureFor(id) { return configuration.value.creatures.find(candidate => candidate.id === id) }

async function refresh() {
  configuration.value = await GetModConfiguration()
  emit('profiles', configuration.value.profiles)
  if (!profile.value) selectedProfile.value = configuration.value.profiles[0]?.loginName || ''
  loadProfile()
  isConfigurationValid.value = true
}

async function perform(operation) {
  if (isBusy.value) return
  isBusy.value = true
  error.value = ''
  try { await operation() } catch (failure) { error.value = String(failure?.message || failure) }
  finally { isBusy.value = false }
}

function loadProfile() {
  bindings.value = (profile.value?.bindings || []).map(binding => ({ ...binding }))
  localStorage.setItem('darkspinner.modProfile', selectedProfile.value)
  emit('selection', selectedProfile.value)
  isEditing.value = false
  isNew.value = false
}

function newProfile() {
  if (isBusy.value || !isConfigurationValid.value || !avatars.value.length) return
  wizardStep.value = 'profile'
  name.value = ''
  avatarID.value = avatars.value[0]?.id || 1
  isTutorialSkipped.value = true
  bindings.value = []
  selectedCreation.value = ''
  selectedCategory.value = 'Hero'
  selectedHero.value = '1667741389'
  isNew.value = true
  isEditing.value = true
  error.value = ''
}

function editProfile() {
  if (isBusy.value || !isConfigurationValid.value || !profile.value) return
  loadProfile()
  wizardStep.value = 'bindings'
  isEditing.value = true
  error.value = ''
}

function bind() {
  if (!creation.value || !hero.value) return
  if (bindings.value.some(binding => binding.heroNoun === hero.value.noun)) return
  bindings.value.push({ heroNoun:hero.value.noun, creationId:creation.value.id })
}

async function save(isResyncRequested = false) {
  if (isNew.value && (!isProfileReady.value || wizardStep.value !== 'bindings')) return
  await perform(async () => {
    const loginName = isNew.value ? name.value : selectedProfile.value
    await SaveModProfile({ loginName, avatarId:avatarID.value, isTutorialSkipped:isTutorialSkipped.value, isNew:isNew.value, isResyncRequested, bindings:bindings.value })
    selectedProfile.value = loginName
    await refresh()
  })
}

async function validate() {
  if (!props.status?.isProfileStoreReady || !props.status?.isContentReady) return
  await perform(async () => {
    isConfigurationValid.value = false
    await refresh()
    if (!avatars.value.length) avatars.value = await GetProfileAvatars()
  })
}

watch(() => props.isActive, isActive => {
  if (isActive) {
    if (!isEditing.value) void validate()
    return
  }
  loadProfile()
}, { flush:'sync' })

watch(() => props.status?.isProfileStoreReady && props.status?.isContentReady, isReady => {
  if (isReady && !isEditing.value) void validate()
}, { immediate:true })
</script>

<template>
  <section class="command-frame launch-command-frame">
    <div class="launch-main">
      <Card class="bay launcher-bay">
        <div class="launch-pane">
          <div class="profile-core">
            <slot name="picker" />
            <p v-if="error && !isEditing" class="mod-error" role="alert">{{ error }}</p>
            <div class="launch-profile-summary">
              <ProfileSummary v-if="profile" :profile="profile" label="MODDED CROGENITOR" />
              <div v-else-if="isConfigurationValid && !isBusy" class="mod-empty"><h2>CREATE YOUR CROGENITOR</h2><p>Create a modded Crogenitor and choose creature bindings in the creation wizard.</p><Button :disabled="!avatars.length" @click="newProfile">+ NEW CROGENITOR</Button></div>
            </div>
          </div>
        </div>
      </Card>
    </div>

    <LauncherDialog v-if="isEditing" :is-dismissible="!isBusy" :width="wizardStep === 'bindings' ? 980 : undefined" @close="loadProfile">
      <div class="notice-card onboarding-card profile-creation-notice mod-editor">
        <p v-if="isNew" class="eyebrow">{{ wizardStep === 'profile' ? 'STEP 1 OF 2 · CROGENITOR' : 'STEP 2 OF 2 · CREATURE BINDINGS' }}</p>
        <DialogTitle as="h2">{{ isNew ? 'CREATE YOUR CROGENITOR' : `EDIT ${profile?.displayName || selectedProfile}` }}</DialogTitle>
        <template v-if="isNew && wizardStep === 'profile'">
          <label>CROGENITOR PHOTO</label>
          <div v-if="avatars.length" class="avatar-picker" role="radiogroup" aria-label="Crogenitor photo">
            <Button variant="outline" v-for="avatar in avatars" :key="avatar.id" type="button" role="radio" :aria-checked="avatar.id === avatarID" :class="{ selected:avatar.id === avatarID }" :disabled="isBusy" @click="avatarID = avatar.id"><img :src="avatar.url" :alt="`Crogenitor photo ${avatar.id}`"></Button>
          </div>
          <div v-else class="avatar-preparing">RECOVERING CROGENITOR PHOTOS...</div>
          <label for="mod-name">CROGENITOR NAME</label><Input id="mod-name" v-model="name" type="text" autofocus maxlength="20" pattern="[A-Za-z0-9]*" placeholder="ENTER NAME" autocomplete="off" spellcheck="false" :disabled="isBusy" @keyup.enter="isProfileReady && (wizardStep = 'bindings')" />
          <p class="onboarding-rule">Use 1–20 letters or numbers.</p>
          <label class="tutorial-skip"><Checkbox v-model="isTutorialSkipped" :disabled="isBusy" /><span><strong>SKIP TUTORIAL</strong><small>Begin on the ship with tutorial progression, starter heroes, DNA, and reward gear already granted.</small></span></label>
        </template>
        <template v-else>
          <div v-if="!configuration.sporePath || configuration.sourceError" class="mod-source-status"><p class="mod-error">{{ configuration.sourceError || 'Set your Spore folder in Config to choose creatures.' }}</p><Button variant="outline" :disabled="isBusy" @click="emit('configure')">OPEN CONFIG</Button></div>
          <div class="mod-grid">
            <Card class="mod-panel">
              <h3>CREATURE &rarr; CHARACTER</h3>
              <Input v-model="search" placeholder="SEARCH CREATURES" aria-label="Search creatures" :disabled="!isEditing || isBusy" />
              <div class="mod-selectors">
              <div class="mod-selector"><label>CREATURE</label>
              <Select v-model="selectedCreation" :disabled="!isEditing || isBusy || !configuration.creatures.length"><SelectTrigger aria-label="Creature"><span v-if="creation" class="mod-option"><ModPortrait :creation-id="creation.id" :label="creation.name" />{{ creation.name }}</span><SelectValue v-else placeholder="SELECT A CREATURE" /></SelectTrigger><SelectContent class="mod-selection"><SelectItem v-for="candidate in visibleCreatures" :key="candidate.id" :value="candidate.id"><span class="mod-option"><ModPortrait :creation-id="candidate.id" :label="candidate.name" /><span>{{ candidate.name }}<small v-if="candidate.isPersonal">YOUR CREATIONS</small></span></span></SelectItem><p v-if="visibleCreatures.length === 60" class="mod-copy">Search to narrow the list.</p></SelectContent></Select>
              </div>
              <div class="mod-selector"><label>CHARACTER</label>
              <Select v-model="selectedCategory" :disabled="!isEditing || isBusy"><SelectTrigger aria-label="Character category" title="Bindings apply to the selected noun, including promoted variants that share it"><SelectValue placeholder="SELECT CATEGORY" /></SelectTrigger><SelectContent><SelectItem v-for="category in categories" :key="category" :value="category">{{ category }}</SelectItem></SelectContent></Select>
              <Input v-model="characterSearch" placeholder="SEARCH CHARACTERS" aria-label="Search characters" :disabled="!isEditing || isBusy" />
              <Select v-model="selectedHero" :disabled="!isEditing || !selectedCreation || isBusy"><SelectTrigger aria-label="Character"><span v-if="hero" class="mod-option"><img v-if="hero.portraitUrl" :src="hero.portraitUrl" alt="">{{ hero.name }}</span><SelectValue v-else placeholder="SELECT A CHARACTER" /></SelectTrigger><SelectContent class="mod-selection"><SelectItem v-for="candidate in visibleCharacters" :key="candidate.noun" :value="String(candidate.noun)" :disabled="bindings.some(binding => binding.heroNoun === candidate.noun)"><span class="mod-option"><img v-if="candidate.portraitUrl" :src="candidate.portraitUrl" alt=""><span>{{ candidate.name }}<small v-if="candidate.nounName">{{ candidate.nounName }}</small></span></span></SelectItem></SelectContent></Select>
              </div>
              </div>
              <Button :disabled="!isEditing || !creation || !hero || isBusy || bindings.some(binding => binding.heroNoun === hero?.noun)" @click="bind">ADD BINDING</Button>
            </Card>

            <Card class="mod-panel">
              <h3>PROFILE BINDINGS <small>{{ bindings.length }}</small></h3>
              <p v-if="!bindings.length" class="mod-copy">Select a creature, category and character.</p>
              <div v-for="binding in bindings" :key="binding.heroNoun" class="mod-binding">
                <ModPortrait :creation-id="binding.creationId" :label="creatureFor(binding.creationId)?.name" />
                <span class="mod-binding-name">{{ creatureFor(binding.creationId)?.name || 'Imported creature' }}<small>→ {{ heroFor(binding.heroNoun)?.name || binding.heroNoun }} &middot; {{ heroFor(binding.heroNoun)?.category }}</small></span>
                <img v-if="heroFor(binding.heroNoun)?.portraitUrl" class="mod-hero-portrait" :src="heroFor(binding.heroNoun).portraitUrl" alt="">
                <div v-if="isEditing" class="mod-binding-actions"><button type="button" :disabled="isBusy" @click="bindings = bindings.filter(candidate => candidate !== binding)">DELETE</button></div>
              </div>
            </Card>
          </div>
        </template>
        <p v-if="error" class="mod-error" role="alert">{{ error }}</p>
        <p v-if="!isNew" class="mod-copy">Resync saves these bindings and restores non-imported creatures to their installed appearance. Imported creatures, inventory and Crogenitor progress are kept.</p>
        <div class="onboarding-actions">
          <Button variant="outline" :disabled="isBusy" @click="loadProfile">CANCEL</Button>
          <Button v-if="!isNew" variant="outline" :disabled="isBusy" title="Save these bindings and refresh non-imported creatures from their installed templates. Keep previous appearance references, imported bindings, inventory and progression." @click="save(true)">RESYNC</Button>
          <Button v-if="isNew && wizardStep === 'bindings'" variant="outline" :disabled="isBusy" @click="wizardStep = 'profile'">BACK</Button>
          <Button v-if="isNew && wizardStep === 'profile'" :disabled="isBusy || !isProfileReady" @click="wizardStep = 'bindings'">NEXT · CREATURE BINDINGS</Button>
          <Button v-else :disabled="isBusy || (isNew && !isProfileReady)" @click="save(false)">{{ isBusy ? 'SAVING...' : isNew ? 'CREATE CROGENITOR' : 'SAVE CROGENITOR' }}</Button>
        </div>
      </div>
    </LauncherDialog>
  </section>
</template>

<style scoped>
.mod-copy, .mod-panel small { color:var(--muted-foreground); font-size:12px; line-height:1.5; }
.mod-grid { display:grid; grid-template-columns:minmax(0, 1.3fr) minmax(0, 1fr); gap:16px; }
.mod-selectors { display:grid; grid-template-columns:repeat(2, minmax(0, 1fr)); gap:12px; }
.mod-selector { display:flex; flex-direction:column; gap:8px; min-width:0; }
.mod-panel { padding:16px; display:flex; flex-direction:column; gap:12px; min-width:0; }
.mod-panel h3 { font-size:12px; letter-spacing:.12em; margin:0; }
.mod-panel label { display:flex; gap:10px; flex-wrap:wrap; font-size:11px; letter-spacing:.08em; align-items:center; }
.mod-option { display:flex; gap:12px; align-items:center; min-height:44px; }
.mod-panel :deep([data-slot="select-trigger"]) { width:100%; height:40px; min-width:0; overflow:hidden; }
.mod-selector .mod-option { min-height:28px; gap:8px; font-size:12px; overflow:hidden; white-space:nowrap; }
.mod-selector .mod-option img, .mod-selector :deep(.mod-portrait) { width:28px; height:28px; flex-shrink:0; }
.mod-option img, .mod-hero-portrait { width:44px; height:44px; object-fit:contain; }
.mod-panel small { display:block; letter-spacing:normal; }
.mod-binding { display:flex; gap:10px; align-items:center; padding:10px 0; border-bottom:1px solid var(--border); }
.mod-binding-name { flex:1; min-width:0; overflow-wrap:anywhere; font-size:13px; }
.mod-binding-actions { display:flex; flex-direction:column; gap:8px; }
.mod-binding-actions button { color:var(--primary); font-size:11px; text-align:left; cursor:pointer; }
.mod-error { color:#ff9b8c; font-size:13px; overflow-wrap:anywhere; }
.mod-actions { display:flex; align-items:center; gap:12px; justify-content:flex-end; }
.mod-selection { max-height:360px; }
.mod-empty { display:flex; flex-direction:column; gap:16px; text-align:center; align-items:center; color:var(--muted-foreground); font-size:13px; }
.mod-empty h2 { color:var(--foreground); }
.mod-source-status { display:flex; align-items:center; gap:12px; margin-bottom:16px; }
@media (max-width:800px) { .mod-grid { grid-template-columns:1fr; } }
</style>
