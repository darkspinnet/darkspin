<script setup>
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Input } from '@/components/ui/input'
import { ListRemoteControlUsers, GetUserControlActivity } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, range:String, refreshKey:Number })
const emit = defineEmits(['busy', 'ready', 'clear', 'invalidated'])
const search = ref('')
const users = ref([])
const selectedUser = ref(null)
const nextUserID = ref(0)
const activeIndex = ref(-1)
const isOpen = ref(false)
const isSearching = ref(false)
const isReading = ref(false)
const error = ref('')
const list = ref(null)
let searchTimer = null
let searchGeneration = 0
let readGeneration = 0
let isDisposed = false
watch(isReading, next => emit('busy', next), { flush:'sync' })
function clearReview() {
  readGeneration++
  isReading.value = false
  error.value = ''
  emit('clear')
}
function failed(failure) {
  error.value = panelError(failure)
  if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
}
function searchChanged(next) {
  search.value = String(next)
  selectedUser.value = null
  clearReview()
  searchGeneration++
  isSearching.value = false
  users.value = []
  nextUserID.value = 0
  activeIndex.value = -1
  isOpen.value = true
  window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(loadSearchResults, 250)
}
function loadSearchResults() { void loadUsers(false) }
function focusSearch() {
  isOpen.value = true
  if (selectedUser.value || !users.value.length) loadSearchResults()
}
async function loadUsers(isMore) {
  if (isDisposed || isSearching.value || (isMore && !nextUserID.value)) return
  const current = searchGeneration
  const query = selectedUser.value ? '' : search.value.trim()
  isSearching.value = true
  try {
    const result = await ListRemoteControlUsers(props.profile.serverAddress, props.profile.loginName, query, isMore ? nextUserID.value : 0)
    if (isDisposed || current !== searchGeneration) return
    users.value = isMore ? [...users.value, ...(result.users || [])] : result.users || []
    nextUserID.value = result.next_user_id || 0
  } catch (failure) {
    if (!isDisposed && current === searchGeneration) failed(failure)
  } finally {
    if (!isDisposed && current === searchGeneration) isSearching.value = false
  }
}
function chooseUser(user) {
  window.clearTimeout(searchTimer)
  searchGeneration++
  isSearching.value = false
  selectedUser.value = { ...user }
  search.value = user.display_name || user.login_name
  isOpen.value = false
  activeIndex.value = -1
  void readPlayer()
}
async function readPlayer() {
  clearReview()
  if (!selectedUser.value) return
  const current = readGeneration
  const user = { ...selectedUser.value }
  isReading.value = true
  try {
    const result = await GetUserControlActivity(props.profile.serverAddress, props.profile.loginName, user.id, props.range, 'Dashboard player filter')
    if (isDisposed || current !== readGeneration) return
    emit('ready', { activity:result, label:user.display_name || user.login_name })
  } catch (failure) {
    if (!isDisposed && current === readGeneration) failed(failure)
  } finally {
    if (!isDisposed && current === readGeneration) isReading.value = false
  }
}
function loadMore(event) {
  const element = event.target
  if (element.scrollTop + element.clientHeight >= element.scrollHeight - 24) void loadUsers(true)
}
async function navigate(event) {
  if (event.key === 'Escape') { isOpen.value = false; return }
  if (event.key === 'Enter') {
    event.preventDefault()
    if (isOpen.value && users.value[activeIndex.value]) chooseUser(users.value[activeIndex.value])
    return
  }
  if (!['ArrowDown', 'ArrowUp'].includes(event.key)) return
  event.preventDefault()
  isOpen.value = true
  activeIndex.value = Math.max(0, Math.min(users.value.length - 1, activeIndex.value + (event.key === 'ArrowDown' ? 1 : -1)))
  await nextTick()
  list.value?.children[activeIndex.value]?.scrollIntoView({ block:'nearest' })
}
watch(() => `${props.range}/${props.refreshKey}`, () => { void readPlayer() })
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}`, () => {
  window.clearTimeout(searchTimer)
  searchGeneration++
  clearReview()
  search.value = ''
  selectedUser.value = null
  users.value = []
  nextUserID.value = 0
  isSearching.value = false
  isOpen.value = false
}, { flush:'sync' })
onBeforeUnmount(() => {
  isDisposed = true
  searchGeneration++
  readGeneration++
  window.clearTimeout(searchTimer)
  emit('busy', false)
  emit('clear')
  users.value = []
})
</script>

<template>
  <div class="dashboard-player-search">
    <Input :model-value="search" role="combobox" aria-autocomplete="list" :aria-expanded="isOpen" aria-controls="dashboard-crogenitor-list" :aria-activedescendant="isOpen && activeIndex >= 0 && users[activeIndex] ? `dashboard-crogenitor-${users[activeIndex].id}` : undefined" :aria-label="$t('Search Crogenitors')" :placeholder="$t('Search Crogenitors')" :maxlength="128" autocomplete="off" @update:model-value="searchChanged" @focus="focusSearch" @blur="isOpen = false" @keydown="navigate" />
    <div v-if="isOpen" id="dashboard-crogenitor-list" ref="list" class="crogenitor-results" role="listbox" :aria-label="$t('Crogenitors')" @scroll="loadMore">
      <button v-for="(user, index) in users" :id="`dashboard-crogenitor-${user.id}`" :key="user.id" type="button" role="option" tabindex="-1" :aria-selected="selectedUser?.id === user.id" :class="{ active:index === activeIndex }" @mousedown.prevent @click="chooseUser(user)">{{ user.display_name }}<small>{{ user.login_name }}</small></button>
      <p v-if="isSearching" role="status">{{ $t('LOADING...') }}</p>
      <p v-else-if="!users.length">{{ $t('No matching accounts.') }}</p>
    </div>
    <p v-if="error" class="player-search-error" role="alert">{{ $t(error) }}</p>
  </div>
</template>

<style scoped>
.dashboard-player-search { position:relative; flex:1; min-width:200px; max-width:360px; }
.crogenitor-results { position:absolute; top:calc(100% + 5px); left:0; right:0; z-index:30; max-height:240px; overflow-y:auto; padding:5px; border:1px solid var(--border); border-radius:8px; background:var(--popover, var(--background)); box-shadow:0 8px 24px #0006; }
.crogenitor-results button { display:flex; align-items:center; justify-content:space-between; gap:10px; width:100%; padding:9px 10px; border-radius:4px; text-align:left; cursor:pointer; font-size:12px; }
.crogenitor-results button:hover, .crogenitor-results button.active { background:var(--accent); }
.crogenitor-results small, .crogenitor-results p { color:var(--muted-foreground); font-size:11px; }
.crogenitor-results p { padding:9px 10px; }
.player-search-error { margin-top:6px; font-size:12px; color:var(--destructive); }
</style>
