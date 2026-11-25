<script setup>
import { formatControlTime } from '../control_time'
import { onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import ConfigHelp from './ConfigHelp.vue'
import { GetUserControlLoginHistory } from '../platform'
import { panelError } from '../control_panel'

const props = defineProps({ profile:Object, targetUserID:Number })
const emit = defineEmits(['busy', 'invalidated'])
const range = ref('day')
const reason = ref('')
const entries = ref([])
const nextIP = ref('')
const until = ref('')
const isBusy = ref(false)
const isLoaded = ref(false)
const error = ref('')
const ranges = [['hour','LAST HOUR'],['day','LAST 24 HOURS'],['week','LAST 7 DAYS'],['month','LAST 30 DAYS'],['year','LAST YEAR']]
let generation = 0
let isDisposed = false
watch(isBusy, next => emit('busy', next), { flush:'sync' })
function clear() {
  generation++
  entries.value = []
  nextIP.value = ''
  until.value = ''
  isLoaded.value = false
  isBusy.value = false
  error.value = ''
}
watch(() => `${props.profile?.serverAddress}/${props.profile?.loginName}/${props.targetUserID}`, () => { clear(); reason.value = '' }, { flush:'sync' })
watch([range, reason], clear, { flush:'sync' })
async function read(isMore = false) {
  if (isBusy.value || !props.targetUserID || !reason.value.trim() || (isMore && !nextIP.value)) return
  const current = generation
  const profile = { ...props.profile }
  isBusy.value = true
  error.value = ''
  const afterIP = isMore ? nextIP.value : ''
  const end = isMore ? until.value : ''
  if (!isMore) { entries.value = []; nextIP.value = ''; until.value = ''; isLoaded.value = false }
  try {
    const result = await GetUserControlLoginHistory(profile.serverAddress, profile.loginName, props.targetUserID, range.value, reason.value.trim(), afterIP, end)
    if (isDisposed || current !== generation) return
    entries.value = isMore ? [...entries.value, ...(result.entries || [])] : result.entries || []
    nextIP.value = result.next_ip || ''
    until.value = result.until
    isLoaded.value = true
  } catch (failure) {
    if (isDisposed || current !== generation) return
    entries.value = []; nextIP.value = ''; until.value = ''; isLoaded.value = false
    error.value = panelError(failure)
    if (/controlPanel:(unauthorized|forbidden)/.test(String(failure))) emit('invalidated', error.value)
  } finally { if (!isDisposed && current === generation) isBusy.value = false }
}
function date(timestamp, isDaily = false) { return formatControlTime(timestamp, isDaily) }
onBeforeUnmount(() => { isDisposed = true; clear(); emit('busy', false) })
</script>

<template>
  <section class="login-history" :aria-label="$t('LOGIN HISTORY REVIEW')" :aria-busy="isBusy">
    <h3>{{ $t('LOGIN HISTORY REVIEW') }}<ConfigHelp text="LOGIN HISTORY REVIEW">{{ $t('IP addresses seen at successful logins, with first and last use and lifetime login counts. Each review is logged.') }}</ConfigHelp></h3>
    <form @submit.prevent="read(false)">
      <label><span>{{ $t('DATE RANGE') }}</span><Select v-model="range" :disabled="isBusy"><SelectTrigger :aria-label="$t('DATE RANGE')"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="option in ranges" :key="option[0]" :value="option[0]">{{ $t(option[1]) }}</SelectItem></SelectContent></Select></label>
      <label class="reason"><span>{{ $t('REVIEW REASON') }}</span><Input v-model="reason" :maxlength="128" :disabled="isBusy" autocomplete="off" /></label>
      <Button type="submit" variant="outline" :disabled="isBusy || !reason.trim()">{{ $t('READ LOGIN HISTORY') }}</Button>
    </form>
    <p v-if="error" role="alert">{{ $t(error) }}</p><p v-if="isBusy" role="status">{{ $t('LOADING...') }}</p>
    <div v-if="entries.length" class="table-scroll"><table>
      <thead><tr><th>{{ $t('IP ADDRESS') }}</th><th>{{ $t('FIRST OBSERVED (LOCAL)') }}</th><th>{{ $t('LAST OBSERVED (LOCAL)') }}</th><th>{{ $t('RETAINED LOGIN COUNT') }}</th></tr></thead>
      <tbody><tr v-for="entry in entries" :key="entry.ip_address"><td>{{ entry.ip_address }}</td><td>{{ date(entry.first_seen_at) }}</td><td>{{ date(entry.last_seen_at) }}</td><td>{{ entry.login_count }}</td></tr></tbody>
    </table></div>
    <p v-else-if="isLoaded && !isBusy">{{ $t('No retained address spans overlap this range.') }}</p>
    <Button v-if="nextIP" variant="outline" :disabled="isBusy" @click="read(true)">{{ $t('LOAD MORE') }}</Button>
  </section>
</template>

<style scoped>
.login-history { display:flex; flex-direction:column; gap:14px; }
form { display:flex; flex-wrap:wrap; align-items:end; gap:12px; }
label { display:flex; flex-direction:column; gap:6px; min-width:180px; font-size:11px; }
.reason { flex:1; }
h3 { font-size:14px; }
p { color:var(--muted-foreground); font-size:12px; }
.table-scroll { overflow:auto; }
table { width:100%; border-collapse:collapse; text-align:left; font-size:12px; }
th, td { padding:10px; border-bottom:1px solid var(--border); overflow-wrap:anywhere; }
</style>
