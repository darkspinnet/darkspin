<script setup>
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Card } from '@/components/ui/card'
import { DialogTitle } from '@/components/ui/dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Progress } from '@/components/ui/progress'
import LauncherDialog from '@/components/LauncherDialog.vue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { CancelPatch, CloseDetachedGameInstances, CloseRunningGame, CloseRunningProfile, CreateProfile, DeleteProfile, DeleteRemoteProfile, DiscardInterruptedMission, GetInstallationStatus, GetInterruptedMission, GetLauncherIntegrationStatus, GetProfileAvatars, GetProfiles, GetRemoteProfiles, GetServerConfiguration, GetStatus, HasDetachedGameInstances, IsProfileRunning, LaunchRemoteProfile, LoginRemoteProfile, OpenReportFolder, OpenSteamDemoInstall, Patch, Play, RefreshInstallationStatus, RefreshRemoteProfiles, RegisterRemoteProfile, RelocateToGameRoot, RemoveLauncherIntegration, RepairLauncherIntegration, RestartLauncher, ScanRemoteServers, SendReport, SetIdentity, SetServerConfiguration, StartDetachedGameInstance, UninstallDarkspinner } from '../wailsjs/go/main/App'
import { BrowserOpenURL, ClipboardSetText, EventsOn, Quit } from '../wailsjs/runtime/runtime'

const status = ref({ state:'starting', message:'Starting DarkSpinner', identity:'', auth:'Starting', server:'Starting', patch:'Pending', game:'Checking', avatar:'Preparing', profile:'Starting', content:'Pending', identityError:'', authError:'', serverError:'', patchError:'', gameError:'', avatarError:'', profileError:'', contentError:'', lastRun:'', launcherNotice:'', version:'', buildChannel:'production', progress:0, patchProgress:0, avatarProgress:0, contentProgress:0, isAuthenticated:false, isAuthOnline:false, isServerOnline:false, isPatchComplete:false, isPatchActive:false, isGameReady:false, isAvatarReady:false, isProfileStoreReady:false, isContentReady:false, isPlayReady:false, isCinematicSkipped:false, isLastRunFailure:false, isStartupBlocked:false })
const retainedProfileName = localStorage.getItem('darkspinner.selectedProfile') || localStorage.getItem('darkspinner.identity') || ''
const retainedDetachedProfileName = localStorage.getItem('darkspinner.detachedProfile') || ''
const identity = ref(localStorage.getItem('darkspinner.identity') || '')
const selectedProfile = ref(retainedProfileName)
const newProfileName = ref('')
const maximumProfileNameLength = 20
const profiles = ref([])
const profileAvatars = ref([])
const selectedAvatarId = ref(1)
const isTutorialSkipped = ref(true)
const installation = ref({ isLocalReady:true, isSteamInstalled:false, isGameInstalled:false, canRelocate:false, message:'' })
const isInstallationLoaded = ref(false)
const isInstallationBusy = ref(false)
const isStartMenuShortcut = ref(true)
const isDesktopShortcut = ref(false)
const isSteamLaunch = ref(false)
const profilePendingDelete = ref(null)
const remoteProfilePendingDelete = ref(null)
const interruptedMission = ref(null)
const detachedInterruptedMission = ref(null)
const isMissionChoiceBusy = ref(false)
const isProfileDeletionBusy = ref(false)
const isRemoteDeletionBusy = ref(false)
const remoteDeletionError = ref('')
const activePage = ref('play')
const remoteServers = ref([])
const remoteProfiles = ref([])
const remoteServerAddress = ref(localStorage.getItem('darkspinner.remoteServer') || '')
const remoteIdentity = ref('')
const remotePassword = ref('')
const isRemotePasswordRemembered = ref(true)
const isRemoteRegistration = ref(false)
const isRemoteBusy = ref(false)
const remoteMessage = ref('Enter a server address or ask for LAN suggestions.')
const serverConfiguration = ref({ port:42127, isMultiplayerEnabled:false, locale:'en-us', locales:[], snapshotMode:'off', isBorderlessFullscreenEnabled:false })
const configuredServerPort = ref(42127)
const isConfiguredMultiplayerEnabled = ref(false)
const configuredLocale = ref('en-us')
const configuredSnapshotMode = ref('off')
const isConfiguredBorderlessFullscreenEnabled = ref(false)
const isServerConfigurationBusy = ref(false)
const serverConfigurationMessage = ref('')
const integrationStatus = ref({ isStartMenuInstalled:false, isDesktopInstalled:false, isSteamLaunchInstalled:false, isManagementSupported:false, message:'' })
const isManagementBusy = ref(false)
const detachedProfile = ref(retainedDetachedProfileName)
const isDetachedLaunchBusy = ref(false)
const isSelectedProfileRunning = ref(false)
const isDetachedProfileRunning = ref(false)
const isDetachedClientDetected = ref(false)
const isClientStopBusy = ref(false)
const isUninstallConfirmOpen = ref(false)
const isMultiplayerConfirmOpen = ref(false)
const isRunningGameConfirmOpen = ref(false)
const isRunningGameConfirmBusy = ref(false)
const launcherNotice = ref('')
let dismissedLauncherNotice = ''
const isRemoteServerMenuOpen = ref(false)
const isProfileListLoaded = ref(false)
const isProfileCreationBusy = ref(false)
const isReportBusy = ref(false)
const isReportComposerOpen = ref(false)
const reportTitle = ref('')
const reportDescription = ref('')
const reportResult = ref(null)
const reportShareMessage = ref('')
const myReportsURL = 'https://github.com/darkspinnet/darkspin/issues?q=is%3Aissue%20author%3A%40me%20sort%3Aupdated-desc'
const reportIssueBody = computed(() => {
  const report = reportResult.value
  if (!report) return ''
  return `## ${report.title}\n\n${report.description}\n\n### Darkspinner build\n${report.version || 'Unknown'}\n\n### Diagnostic archives\n${report.names.map(name => `- ${name}`).join('\n')}\n\nAttach all ${report.names.length} ZIP(s) from the bug folder before submitting this issue. Each ZIP is at most 25 MB.`
})
const reportIssueDraft = computed(() => {
  const report = reportResult.value
  if (!report) return { url:'', isLong:false }
  const url = new URL('https://github.com/darkspinnet/darkspin/issues/new')
  url.searchParams.set('title', Array.from(report.title).slice(0, 160).join(''))
  url.searchParams.set('body', reportIssueBody.value)
  const isLong = url.href.length > 7000
  if (isLong) url.searchParams.set('body', 'Paste the complete report copied by Darkspinner here, then attach all report ZIPs from the bug folder before submitting.')
  return { url:url.href, isLong }
})
const isChangelogOpen = ref(false)
const isChangelogLoading = ref(false)
const changelogText = ref('')
const changelogError = ref('')
const retainedAutoPatch = localStorage.getItem('darkspinner.autoPatch')
const isAutoPatchEnabled = ref(retainedAutoPatch === null || retainedAutoPatch === 'true')
const isAutoLaunchEnabled = ref(localStorage.getItem('darkspinner.autoLaunch') === 'true')
const isPatchRequestBusy = ref(false)
const isPatchCancelRequested = ref(false)
const isLauncherFailureCopied = ref(false)
const hasAutoLaunchAttempted = ref(false)
const autoLaunchCountdown = ref(0)
const isGameLaunchedThisSession = ref(false)
const activeClientRoute = ref('')
const campaignPlanets = [
  "Zelem's Nexus", "Zelem's Nexus", 'Nocturna', 'Nocturna',
  'Verdanth', 'Verdanth', "Zelem's Nexus", "Zelem's Nexus",
  'Cryos', 'Cryos', 'Verdanth', 'Verdanth',
  'Infinity', 'Infinity', 'Cryos', 'Cryos',
  'Nocturna', 'Nocturna', 'Infinity', 'Infinity',
  'Scaldron', 'Scaldron', 'Scaldron', 'Scaldron',
]
let autoLaunchTimer = null
let processStateTimer = null
let preparationProgressTimer = null
let isProcessStateRefreshing = false
const preparationProgressClock = ref(Date.now())
const contentPhaseStartedAt = ref(Date.now())
const contentPhaseEstimates = {
  'Preparing visual assets': { start:0, end:8.55, duration:5200 },
  'Checking prepared content': { start:8.55, end:8.56, duration:1 },
  'Validating installed content': { start:8.56, end:8.57, duration:1 },
  'Indexing content packages': { start:8.57, end:9.55, duration:595 },
  'Creating content database': { start:9.55, end:9.57, duration:8 },
  'Recording package inventory': { start:9.57, end:9.58, duration:1 },
  'Importing runtime resources': { start:9.58, end:11.16, duration:959 },
  'Indexing Lua content': { start:11.16, end:11.82, duration:404 },
  'Importing equipment models': { start:11.82, end:41.31, duration:17923 },
  'Importing equipment affixes': { start:41.31, end:49.88, duration:5209 },
  'Importing loot tuning': { start:49.88, end:57.72, duration:4765 },
  'Importing combat tuning': { start:57.72, end:57.76, duration:22 },
  'Importing levels and navigation': { start:57.76, end:68.59, duration:6582 },
  'Importing campaign chain': { start:68.59, end:68.61, duration:11 },
  'Importing crystal tuning': { start:68.61, end:73.00, duration:2669 },
  'Linking level scripts': { start:73.00, end:73.30, duration:180 },
  'Importing hero templates': { start:73.30, end:74.00, duration:428 },
  'Importing enemy attributes': { start:74.00, end:93.71, duration:11980 },
  'Importing object physics': { start:93.71, end:97.09, duration:2052 },
  'Importing localized text': { start:97.09, end:97.79, duration:428 },
  'Building content indexes': { start:97.79, end:98.50, duration:430 },
  'Verifying prepared content': { start:98.50, end:99.94, duration:875 },
  'Prepared content ready': { start:100, end:100, duration:1 },
}
const isBusy = computed(() => ['authorizing','patching','launching','running'].includes(status.value.state))
const isPlayEnabled = computed(() => status.value.isPlayReady && !selectedProfileRecord.value?.isTutorialCompletionPending && !isBusy.value)
const isDetachedReady = computed(() => status.value.isAuthOnline && status.value.isServerOnline && status.value.isPatchComplete && status.value.isGameReady)
const isCreatingProfile = computed(() =>
  isInstallationLoaded.value && installation.value.isLocalReady &&
  isProfileListLoaded.value && status.value.isAvatarReady && profileAvatars.value.length > 0 &&
  (profiles.value.length === 0 || selectedProfile.value === '__create__'))
const isFirstRunOnboarding = computed(() => isProfileListLoaded.value && profiles.value.length === 0)
const isInstallationRequired = computed(() => isInstallationLoaded.value && !installation.value.isLocalReady)
const isIntegrationSelected = computed(() => isStartMenuShortcut.value || isDesktopShortcut.value || isSteamLaunch.value)
const isReportReady = computed(() => !!reportTitle.value.trim() && !!reportDescription.value.trim() && !isReportBusy.value)
const isNewProfileNameValid = computed(() => {
  const name = newProfileName.value.trim()
  return /^[A-Za-z0-9]{1,20}$/.test(name)
})
const selectedProfileLabel = computed(() => {
  const profile = profiles.value.find(candidate => candidate.loginName === selectedProfile.value)
  return profile?.displayName || 'SELECT CROGENITOR'
})
const detachedProfileRecord = computed(() => profiles.value.find(candidate => candidate.loginName === detachedProfile.value))
const detachedProfileLabel = computed(() => detachedProfileRecord.value?.displayName || 'SELECT CROGENITOR')
const detachedProfileAvatar = computed(() => detachedProfileRecord.value?.avatarUrl || '')
const verificationStage = computed(() => {
  const stages = [
    { detail:status.value.profile, progress:0, ready:status.value.isProfileStoreReady, error:status.value.profileError },
    { detail:status.value.avatar, progress:status.value.avatarProgress, ready:status.value.isAvatarReady, error:status.value.avatarError },
    { detail:status.value.content, progress:status.value.contentProgress, ready:status.value.isContentReady, error:status.value.contentError },
    { detail:status.value.patch, progress:status.value.patchProgress, ready:status.value.isPatchComplete, error:status.value.patchError },
    { detail:status.value.game, progress:0, ready:status.value.isGameReady, error:status.value.gameError },
  ]
  return stages.find(stage => stage.error) || stages.find(stage => !stage.ready) || { detail:'Complete', progress:100, ready:true, error:'' }
})
const statusRows = computed(() => [
  { label:'Verify', ...verificationStage.value },
  { label:'Auth', detail:status.value.auth, ready:status.value.isAuthOnline, error:status.value.authError },
  { label:'Server', detail:status.value.server, ready:status.value.isServerOnline, error:status.value.serverError },
])
const readySystemCount = computed(() => statusRows.value.filter(row => row.ready).length)
const systemCount = computed(() => statusRows.value.length)
const estimatedContentProgress = computed(() => {
  if (status.value.isContentReady) return 100
  const estimate = contentPhaseEstimates[status.value.content]
  if (!estimate) return Math.max(0, Math.min(100, status.value.contentProgress || 0))
  const elapsed = Math.max(0, preparationProgressClock.value - contentPhaseStartedAt.value)
  const fraction = Math.min(.98, elapsed / estimate.duration)
  return estimate.start + (estimate.end - estimate.start) * fraction
})
const launcherProgressFailureLabel = computed(() => {
  const failedSystem = statusRows.value.find(row => row.error)
  if (failedSystem) return `${failedSystem.label} failed`
  if (status.value.state === 'error') return 'Preparation failed'
  return ''
})
const isLauncherUpdateActive = computed(() =>
  ['Downloading launcher update', 'Restarting with update'].includes(status.value.patch))
const launcherProgress = computed(() => {
  if (launcherProgressFailureLabel.value || isLauncherUpdateActive.value) return 0
  if (statusRows.value.every(row => row.ready)) return 100
  const contentProgress = status.value.isContentReady ? 100 : estimatedContentProgress.value
  // A silent update check has no measurable work and must not hold content progress at zero.
  const patchProgress = status.value.isPatchComplete || status.value.patch === 'Pending'
    ? 100 : Math.max(0, status.value.patchProgress || 0)
  return Math.max(0, Math.min(97, Math.min(contentProgress, patchProgress) * .97))
})
const isLauncherProgressIndeterminate = computed(() =>
  isLauncherUpdateActive.value && !launcherProgressFailureLabel.value)
const launcherProgressLabel = computed(() => {
  if (launcherProgressFailureLabel.value) return launcherProgressFailureLabel.value
  if (isLauncherUpdateActive.value) return status.value.patch
  const pendingSystem = statusRows.value.find(row => !row.ready)
  if (pendingSystem) return visibleLauncherText(pendingSystem.detail || `${pendingSystem.label} pending`)
  if (status.value.state === 'launching') return visibleLauncherText(status.value.message || 'Launching game')
  if (status.value.state === 'running') return 'Client active'
  if (isGameLaunchedThisSession.value && status.value.lastRun) return visibleLauncherText(status.value.lastRun)
  return visibleLauncherText(status.value.message || 'Ready')
})
const launcherFailure = computed(() => {
  const failedSystem = statusRows.value.find(row => row.error)
  if (failedSystem) return `${failedSystem.label}: ${failedSystem.error}`
  if (status.value.state === 'error') return status.value.message || 'Darkspinner could not finish preparing.'
  return ''
})
const isPreparationActive = computed(() => !launcherProgressFailureLabel.value &&
  !statusRows.value.every(row => row.ready) &&
  !['cancelled', 'error', 'installation-required'].includes(status.value.state))
const isPatchBusy = computed(() => isPatchRequestBusy.value || status.value.isPatchActive || isPreparationActive.value)
const isAutoLaunchCounting = computed(() => autoLaunchCountdown.value > 0)
const isActiveProfileRunning = computed(() => activePage.value === 'play' && isSelectedProfileRunning.value)
const isActiveClientDetected = computed(() => activePage.value === 'launcher'
  ? isDetachedClientDetected.value
  : activePage.value === 'play' && isSelectedProfileRunning.value)
const activeInterruptedMission = computed(() => activePage.value === 'launcher'
  ? detachedInterruptedMission.value
  : activePage.value === 'play' ? interruptedMission.value : null)
const isHeaderPlayEnabled = computed(() => activePage.value === 'launcher'
  ? isDetachedReady.value && !!detachedProfile.value && !isDetachedLaunchBusy.value && !isMissionChoiceBusy.value
  : activePage.value === 'remote'
    ? isRemoteFormReady.value && !isRemoteBusy.value && !isActiveRouteRunning.value &&
      (isRemoteRegistration.value || (status.value.isPatchComplete && status.value.isGameReady))
  : !isActiveProfileRunning.value && isPlayEnabled.value && !isMissionChoiceBusy.value)
const isActiveRouteRunning = computed(() => status.value.state === 'running' && activeClientRoute.value === activePage.value)
const isAttachedKillVisible = computed(() => (activePage.value === 'play' || activePage.value === 'remote') && isActiveRouteRunning.value)
const isDetachedKillVisible = computed(() => activePage.value === 'launcher' && isDetachedClientDetected.value)
const dockPlayLabel = computed(() => {
  if (isAutoLaunchCounting.value) return `PLAY IN ${autoLaunchCountdown.value} · CANCEL`
  if (isActiveRouteRunning.value) return 'RUNNING'
  if (isActiveProfileRunning.value) return 'RUNNING'
  if (activeInterruptedMission.value) return 'CONTINUE'
  if (activePage.value === 'remote') {
    if (isRemoteRegistration.value) return isRemoteBusy.value ? 'REGISTERING...' : 'REGISTER'
    return 'PLAY REMOTE'
  }
  if (activePage.value === 'launcher') return 'PLAY DETACHED'
  return 'PLAY'
})
const dockPlayTitle = computed(() => activePage.value === 'config'
  ? 'Play is unavailable while editing configuration.'
  : isAutoLaunchCounting.value
    ? 'Cancel automatic launch'
    : activePage.value === 'remote'
    ? isRemoteRegistration.value
      ? 'Register this Crogenitor on the selected remote server'
      : 'Launch the selected remote Crogenitor'
    : activePage.value === 'launcher'
      ? `Launch another detached client for ${detachedProfileRecord.value?.displayName || 'selected Crogenitor'}`
      : activeInterruptedMission.value
        ? `Continue ${activeInterruptedMission.value.label}`
        : 'Launch game')
const selectedRemoteProfile = computed(() => remoteProfiles.value.find(profile =>
  isSameRemoteServer(profile.serverAddress, remoteServerAddress.value) && profile.loginName === remoteIdentity.value))
const selectedRemoteServer = computed(() => remoteServers.value.find(server =>
  isSameRemoteServer(server.address, remoteServerAddress.value)) || remoteServers.value[0])
const selectedRemoteProfileLabel = computed(() => selectedRemoteProfile.value?.displayName || 'SELECT CROGENITOR')
const selectedRemoteProfileAvatar = computed(() => selectedRemoteProfile.value?.avatarUrl || '')
const selectedRemoteCampaignLevel = computed(() => Math.max(1, selectedRemoteProfile.value?.highestCampaignUnlocked || 1))
const selectedRemoteCampaignLabel = computed(() => campaignLabelFor(selectedRemoteCampaignLevel.value))
const selectedRemoteCampaignDetail = computed(() => campaignDetailFor(selectedRemoteCampaignLevel.value))
const isRemoteFormReady = computed(() => !!remoteServerAddress.value &&
  /^[A-Za-z0-9]{1,20}$/.test(remoteIdentity.value.trim()) &&
  (!!remotePassword.value || (!isRemoteRegistration.value && selectedRemoteProfile.value?.isPasswordRemembered)))
const isServerPortValid = computed(() => isPortValid(configuredServerPort.value) && Number(configuredServerPort.value) <= 65534)
// Remove this channel gate when remote, detached, and multiplayer configuration are ready for every build.
const isExperimentalLauncherFeatureVisible = computed(() => ['development', 'unstable'].includes(status.value.buildChannel))

watch(identity, next => localStorage.setItem('darkspinner.identity', next))
watch(remoteServerAddress, next => localStorage.setItem('darkspinner.remoteServer', next))
watch(newProfileName, next => {
  const restricted = next.replace(/[^A-Za-z0-9]/g, '').slice(0, maximumProfileNameLength)
  if (restricted !== next) newProfileName.value = restricted
})
watch(selectedProfile, next => {
  if (next && next !== '__create__') localStorage.setItem('darkspinner.selectedProfile', next)
  cancelAutoLaunchCountdown()
  void refreshRunningProfiles()
})
watch(detachedProfile, next => {
  if (next) localStorage.setItem('darkspinner.detachedProfile', next)
  void refreshRunningProfiles()
  void refreshDetachedInterruptedMission()
})
watch(isAutoPatchEnabled, next => {
  localStorage.setItem('darkspinner.autoPatch', String(next))
  if (next) void maybeAutoPatch()
})
watch(isAutoLaunchEnabled, next => {
  localStorage.setItem('darkspinner.autoLaunch', String(next))
  if (!next) {
    cancelAutoLaunchCountdown()
    return
  }
  hasAutoLaunchAttempted.value = false
  if (activePage.value === 'play') void maybeAutoLaunch()
})
watch(launcherFailure, () => { isLauncherFailureCopied.value = false })
watch(() => status.value.content, () => {
  contentPhaseStartedAt.value = Date.now()
  preparationProgressClock.value = contentPhaseStartedAt.value
})
const isProfileCreationReady = computed(() =>
  installation.value.isLocalReady && status.value.isAvatarReady &&
  isNewProfileNameValid.value && profileAvatars.value.length > 0 && status.value.isProfileStoreReady)
const selectedProfileRecord = computed(() => profiles.value.find(candidate => candidate.loginName === selectedProfile.value))
const selectedProfileAvatar = computed(() => selectedProfileRecord.value?.avatarUrl || '')
const readinessSummary = computed(() => {
  const failedSystem = statusRows.value.find(row => row.error)
  if (failedSystem) return { level:'error', label:`${failedSystem.label} failure`, message:failedSystem.error }
  const waitingSystem = statusRows.value.find(row => !row.ready)
  if (waitingSystem) return { level:'waiting', label:`${waitingSystem.label} pending`, message:waitingSystem.detail || status.value.message }
  if (!identity.value.trim()) return { level:'waiting', label:'Identity required', message:'Select a Crogenitor before launching.' }
  if (selectedProfileRecord.value?.isTutorialCompletionPending) return { level:'waiting', label:'Crogenitor update pending', message:'Tutorial completion is still being finalized.' }
  if (isSelectedProfileRunning.value) return { level:'ready', label:'Client active', message:'The selected Crogenitor already has a managed game client running.' }
  if (isBusy.value) return { level:'waiting', label:'Launch in progress', message:status.value.message || 'Waiting for the game client.' }
  if (!status.value.isPlayReady) return { level:'waiting', label:'Play unavailable', message:status.value.message || 'Launcher readiness has not completed.' }
  return { level:'ready', label:'Ready to launch', message:'Verification, authentication, and the local server are online.' }
})
const footerStatus = computed(() => {
  if (isAutoLaunchCounting.value) return { level:'waiting', label:`AUTO-LAUNCH IN ${autoLaunchCountdown.value}`, message:'Press Play to cancel the countdown.' }
  if (isInstallationBusy.value) return { level:'waiting', label:'INSTALLATION', message:installation.value.message || 'Preparing the local installation.' }
  if (isProfileCreationBusy.value) return { level:'waiting', label:'CROGENITOR', message:'Creating the new Crogenitor.' }
  if (isManagementBusy.value) return { level:'waiting', label:'CONFIG', message:'Applying the requested launcher change.' }
  if (isReportBusy.value) return { level:'waiting', label:'REPORT', message:'Preparing diagnostic files.' }
  if (isDetachedLaunchBusy.value) return { level:'waiting', label:'DETACHED CLIENT', message:'Starting the selected Crogenitor client.' }
  if (activePage.value === 'launcher') return null
  if (status.value.state === 'running') return { level:'ready', label:'CLIENT ACTIVE', message:'The game client is running.' }
  const summary = readinessSummary.value
  if (summary.level !== 'ready') return summary
  return null
})
function campaignLevelFor(profile) {
  if (!profile?.isTutorialCompleted) return 0
  return Math.min(campaignPlanets.length, Math.max(1, profile.highestCampaignUnlocked || 1))
}

function profileCreateDate(profile) {
  if (!profile?.createDt) return ''
  const createDT = new Date(profile.createDt)
  if (Number.isNaN(createDT.getTime())) return ''
  return createDT.toISOString().slice(0, 10)
}
function profileLastConnectedDate(profile) {
  if (!profile?.lastConnectedDt) return ''
  const lastConnectedDT = new Date(profile.lastConnectedDt)
  if (Number.isNaN(lastConnectedDT.getTime())) return ''
  return lastConnectedDT.toISOString().slice(0, 10)
}
function profileLastPlayedDate(profile) {
  const connectionDT = profile?.lastConnectionDt || profile?.lastConnectedDt
  if (!connectionDT) return 'NEVER'
  const lastPlayedDT = new Date(connectionDT)
  if (Number.isNaN(lastPlayedDT.getTime())) return 'NEVER'
  return lastPlayedDT.toISOString().slice(0, 10)
}
function markProfilePlayed(loginName) {
  const connectionDT = new Date().toISOString()
  profiles.value = profiles.value.map(profile =>
    profile.loginName === loginName ? { ...profile, lastConnectionDt:connectionDT } : profile,
  )
}
function campaignLabelFor(chainLevel) {
  if (!chainLevel) return 'TUTORIAL'
  const planet = Math.floor((chainLevel - 1) / 4) + 1
  const mission = ((chainLevel - 1) % 4) + 1
  return `${planet}-${mission}`
}
function campaignDetailFor(chainLevel) {
  return chainLevel
    ? campaignPlanets[chainLevel - 1]
    : 'CRYOS TRAINING'
}
const selectedCampaignLevel = computed(() => campaignLevelFor(selectedProfileRecord.value))
const selectedCampaignLabel = computed(() => campaignLabelFor(selectedCampaignLevel.value))
const selectedCampaignDetail = computed(() => campaignDetailFor(selectedCampaignLevel.value))
const detachedCampaignLevel = computed(() => campaignLevelFor(detachedProfileRecord.value))
const detachedCampaignLabel = computed(() => campaignLabelFor(detachedCampaignLevel.value))
const detachedCampaignDetail = computed(() => campaignDetailFor(detachedCampaignLevel.value))
watch(isPlayEnabled, next => {
  if (!next) {
    cancelAutoLaunchCountdown()
    return
  }
  void refreshInterruptedMission()
  void maybeAutoLaunch()
})
watch(isDetachedReady, next => {
  if (next) void refreshDetachedInterruptedMission()
})

onMounted(async () => {
  EventsOn('darkspinner:status', next => {
    status.value = { ...status.value, ...next }
    showLauncherNotice(next.launcherNotice)
    if (next.isProfileStoreReady && !isProfileListLoaded.value) void refreshProfiles()
    if (next.isAvatarReady && profileAvatars.value.length === 0) void refreshAvatarAssets()
    if (next.isServerOnline && profiles.value.some(profile => profile.isTutorialCompletionPending)) void refreshProfiles()
    if (next.patch === 'Update available') void maybeAutoPatch()
  })
  status.value = { ...status.value, ...(await GetStatus()) }
  showLauncherNotice(status.value.launcherNotice)
  installation.value = await GetInstallationStatus()
  integrationStatus.value = await GetLauncherIntegrationStatus()
  isInstallationLoaded.value = true
  if (isInstallationRequired.value) return
  if (status.value.isAutoPlayRequested) isAutoLaunchEnabled.value = true
  if (status.value.identity) identity.value = status.value.identity
  await refreshProfiles()
  await refreshServerConfiguration()
  await refreshAvatarAssets()
  await refreshRunningProfiles()
  processStateTimer = window.setInterval(() => void refreshRunningProfiles(), 750)
  preparationProgressTimer = window.setInterval(() => { preparationProgressClock.value = Date.now() }, 100)
  await maybeAutoPatch()
  await maybeAutoLaunch()
})

onBeforeUnmount(() => {
  cancelAutoLaunchCountdown()
  if (processStateTimer !== null) window.clearInterval(processStateTimer)
  if (preparationProgressTimer !== null) window.clearInterval(preparationProgressTimer)
})

async function refreshRunningProfiles() {
  if (isProcessStateRefreshing) return
  isProcessStateRefreshing = true
  const selectedName = selectedProfile.value
  const detachedName = detachedProfile.value
  try {
    const names = [...new Set([selectedName, detachedName]
      .filter(name => name && name !== '__create__'))]
    const [states, isDetachedDetected] = await Promise.all([
      Promise.all(names.map(async name => [name, await IsProfileRunning(name)])),
      HasDetachedGameInstances(),
    ])
    const statesByName = new Map(states)
    isDetachedClientDetected.value = isDetachedDetected
    if (selectedProfile.value === selectedName) {
      const wasSelectedProfileRunning = isSelectedProfileRunning.value
      isSelectedProfileRunning.value = statesByName.get(selectedName) || false
      if (wasSelectedProfileRunning && !isSelectedProfileRunning.value) await refreshInterruptedMission()
    }
    if (detachedProfile.value === detachedName) {
      const wasDetachedProfileRunning = isDetachedProfileRunning.value
      isDetachedProfileRunning.value = statesByName.get(detachedName) || false
      if (wasDetachedProfileRunning && !isDetachedProfileRunning.value) {
        await refreshDetachedInterruptedMission()
      }
    }
  } catch (error) { recordError(error) }
  finally { isProcessStateRefreshing = false }
}

async function refreshProfiles() {
  try {
    profiles.value = await GetProfiles() || []
    isProfileListLoaded.value = true
    if (!profiles.value.length) {
      selectedProfile.value = '__create__'
      await applyIdentity('')
      return
    }

    const retained = profiles.value.find(profile => profile.loginName === selectedProfile.value)
    const active = profiles.value.find(profile => profile.loginName === status.value.identity)
    const current = profiles.value.find(profile => profile.loginName === identity.value)
    const profile = retained || active || current || profiles.value[0]
    selectedProfile.value = profile.loginName
    if (!profiles.value.some(candidate => candidate.loginName === detachedProfile.value)) {
      detachedProfile.value = profiles.value.find(candidate => candidate.loginName !== profile.loginName)?.loginName || profile.loginName
    }
    await applyIdentity(profile.loginName)
  }
  catch (error) { recordError(error) }
}

async function refreshRemoteProfiles() {
  try {
    remoteProfiles.value = await GetRemoteProfiles() || []
    const selected = remoteProfiles.value.find(profile =>
      isSameRemoteServer(profile.serverAddress, remoteServerAddress.value) && profile.loginName === remoteIdentity.value) ||
      remoteProfiles.value.find(profile => isSameRemoteServer(profile.serverAddress, remoteServerAddress.value)) ||
      remoteProfiles.value[0]
    if (selected && !selectedRemoteProfile.value) selectRemoteProfile(selected)
    void refreshStaleRemoteProfiles()
  }
  catch (error) { recordError(error) }
}

async function refreshStaleRemoteProfiles() {
  try { remoteProfiles.value = await RefreshRemoteProfiles() || remoteProfiles.value }
  catch { /* Cached remote profiles remain usable while their server is offline. */ }
}

async function scanRemoteServers() {
  if (isRemoteBusy.value) return
  isRemoteBusy.value = true
  remoteMessage.value = 'Looking for Darkspinner instances on private IPv4 networks...'
  try {
    remoteServers.value = await ScanRemoteServers(remoteServerAddress.value) || []
    if (remoteServers.value.length) {
      remoteServerAddress.value = remoteServers.value[0].address
      isRemoteServerMenuOpen.value = true
      remoteMessage.value = `${remoteServers.value.length} Darkspinner instance${remoteServers.value.length === 1 ? '' : 's'} detected.`
    } else {
      isRemoteServerMenuOpen.value = false
      remoteMessage.value = 'No Darkspinner instances responded. You can still enter an address manually.'
    }
  }
  catch (error) {
    isRemoteServerMenuOpen.value = false
    remoteMessage.value = visibleLauncherText(error)
  }
  finally { isRemoteBusy.value = false }
}

function selectRemoteServer(server) {
  remoteServerAddress.value = server.address
  isRemoteServerMenuOpen.value = false
}

function isSameRemoteServer(firstAddress, secondAddress) {
  const withDefaultPort = address => {
    const trimmedAddress = String(address || '').trim().toLowerCase()
    return trimmedAddress && !trimmedAddress.includes(':') ? `${trimmedAddress}:42127` : trimmedAddress
  }
  return withDefaultPort(firstAddress) === withDefaultPort(secondAddress)
}

function isPortValid(port) {
  return Number.isInteger(Number(port)) && Number(port) >= 1 && Number(port) <= 65535
}

async function refreshServerConfiguration() {
  try {
    serverConfiguration.value = await GetServerConfiguration()
    configuredServerPort.value = serverConfiguration.value.port
    isConfiguredMultiplayerEnabled.value = serverConfiguration.value.isMultiplayerEnabled
    configuredLocale.value = serverConfiguration.value.locale
    configuredSnapshotMode.value = serverConfiguration.value.snapshotMode
    isConfiguredBorderlessFullscreenEnabled.value = serverConfiguration.value.isBorderlessFullscreenEnabled
  }
  catch (error) { serverConfigurationMessage.value = visibleLauncherText(error) }
}

async function saveServerConfiguration() {
  if (!isServerPortValid.value || isServerConfigurationBusy.value) return
  if (isConfiguredMultiplayerEnabled.value && !serverConfiguration.value.isMultiplayerEnabled) {
    isMultiplayerConfirmOpen.value = true
    return
  }
  await applyServerConfiguration()
}

async function confirmMultiplayerConfiguration() {
  isMultiplayerConfirmOpen.value = false
  await applyServerConfiguration()
}

function cancelMultiplayerConfiguration() {
  isMultiplayerConfirmOpen.value = false
  isConfiguredMultiplayerEnabled.value = serverConfiguration.value.isMultiplayerEnabled
}

async function applyServerConfiguration() {
  isServerConfigurationBusy.value = true
  const isNetworkChanged = Number(configuredServerPort.value) !== Number(serverConfiguration.value.port) ||
    isConfiguredMultiplayerEnabled.value !== serverConfiguration.value.isMultiplayerEnabled
  serverConfigurationMessage.value = isNetworkChanged
    ? 'Saving darkspin.toml and restarting the local server...'
    : 'Saving darkspin.toml...'
  try {
    serverConfiguration.value = await SetServerConfiguration(
      Number(configuredServerPort.value), isConfiguredMultiplayerEnabled.value,
      configuredLocale.value, configuredSnapshotMode.value,
      isConfiguredBorderlessFullscreenEnabled.value,
    )
    configuredServerPort.value = serverConfiguration.value.port
    isConfiguredMultiplayerEnabled.value = serverConfiguration.value.isMultiplayerEnabled
    configuredLocale.value = serverConfiguration.value.locale
    configuredSnapshotMode.value = serverConfiguration.value.snapshotMode
    isConfiguredBorderlessFullscreenEnabled.value = serverConfiguration.value.isBorderlessFullscreenEnabled
    if (isNetworkChanged) {
      const interfaceLabel = serverConfiguration.value.isMultiplayerEnabled ? 'all network interfaces' : 'loopback only'
      serverConfigurationMessage.value = `Local server restarted on ${interfaceLabel}, port ${serverConfiguration.value.port}. Language: ${serverConfiguration.value.locale}.`
    } else {
      serverConfigurationMessage.value = `Configuration saved. Game language: ${serverConfiguration.value.locale}. Sync Snapshot: ${serverConfiguration.value.snapshotMode}.`
    }
  }
  catch (error) { serverConfigurationMessage.value = visibleLauncherText(error) }
  finally { isServerConfigurationBusy.value = false }
}

function chooseRemoteProfileKey(key) {
  const profile = remoteProfiles.value.find(candidate => `${candidate.serverAddress}:${candidate.loginName}` === key)
  if (profile) selectRemoteProfile(profile)
}

function chooseRemoteServerAddress(address) {
  const server = remoteServers.value.find(candidate => candidate.address === address)
  if (server) selectRemoteServer(server)
}

function selectRemoteProfile(profile) {
  remoteServerAddress.value = profile.serverAddress
  remoteIdentity.value = profile.loginName
  remotePassword.value = ''
  isRemotePasswordRemembered.value = profile.isPasswordRemembered
  isRemoteRegistration.value = false
  remoteMessage.value = profile.isPasswordRemembered ? 'Saved password ready.' : 'Enter the password for this Crogenitor.'
}

function beginRemoteRegistration() {
  isRemoteServerMenuOpen.value = false
  isRemoteRegistration.value = true
  remoteIdentity.value = ''
  remotePassword.value = ''
  isRemotePasswordRemembered.value = true
  isTutorialSkipped.value = true
  remoteMessage.value = 'Choose a Crogenitor photo, name, and password for this server.'
}

function cancelRemoteRegistration() {
  isRemoteRegistration.value = false
  isRemoteServerMenuOpen.value = false
  remotePassword.value = ''
  isTutorialSkipped.value = true
  const profile = remoteProfiles.value.find(candidate =>
    isSameRemoteServer(candidate.serverAddress, remoteServerAddress.value)) || remoteProfiles.value[0]
  if (profile) selectRemoteProfile(profile)
}

async function submitRemoteAccount() {
  if (!isRemoteFormReady.value || isRemoteBusy.value) return
  isRemoteBusy.value = true
  try {
    if (isRemoteRegistration.value) {
      await RegisterRemoteProfile(remoteServerAddress.value, remoteIdentity.value.trim(), remotePassword.value, selectedAvatarId.value, isRemotePasswordRemembered.value, isTutorialSkipped.value)
      remoteMessage.value = 'Remote Crogenitor registered or signed in and saved.'
      isRemoteRegistration.value = false
      isTutorialSkipped.value = true
    } else {
      await LoginRemoteProfile(remoteServerAddress.value, remoteIdentity.value.trim(), remotePassword.value, isRemotePasswordRemembered.value)
      remoteMessage.value = 'Remote credentials verified.'
    }
    await refreshRemoteProfiles()
    if (isRemotePasswordRemembered.value) remotePassword.value = ''
  }
  catch (error) { remoteMessage.value = visibleLauncherText(error) }
  finally { isRemoteBusy.value = false }
}

async function launchRemote() {
  if (!isRemoteFormReady.value || isRemoteBusy.value) return
  isRemoteBusy.value = true
  try {
    await LaunchRemoteProfile(remoteServerAddress.value, remoteIdentity.value.trim(), remotePassword.value, isRemotePasswordRemembered.value)
    activeClientRoute.value = 'remote'
    remoteMessage.value = 'Remote game launch requested.'
    await refreshRemoteProfiles()
    if (isRemotePasswordRemembered.value) remotePassword.value = ''
  }
  catch (error) { remoteMessage.value = visibleLauncherText(error) }
  finally { isRemoteBusy.value = false }
}

async function refreshAvatarAssets() {
  try {
    profileAvatars.value = await GetProfileAvatars() || []
    if (!profileAvatars.value.some(avatar => avatar.id === selectedAvatarId.value) && profileAvatars.value.length) {
      selectedAvatarId.value = profileAvatars.value[0].id
    }
    if (profileAvatars.value.length) await refreshProfilePortraits()
  }
  catch (error) { recordError(error) }
}

async function refreshProfilePortraits() {
  if (!isProfileListLoaded.value) return
  try { profiles.value = await GetProfiles() || [] }
  catch (error) { recordError(error) }
}

async function createProfile() {
  const name = newProfileName.value.trim()
  if (!/^[A-Za-z0-9]{1,20}$/.test(name)) {
    recordError('Crogenitor name must be 1–20 letters or numbers')
    return
  }
  isProfileCreationBusy.value = true
  try {
    await CreateProfile(name, selectedAvatarId.value, isTutorialSkipped.value)
    selectedProfile.value = name
    await refreshProfiles()
    newProfileName.value = ''
    isTutorialSkipped.value = true
  }
  catch (error) { recordError(error) }
  finally { isProfileCreationBusy.value = false }
}

async function selectProfile() {
  if (selectedProfile.value === '__create__') return
  await applyIdentity(selectedProfile.value)
}

async function applyIdentity(profileName) {
  identity.value = profileName
  status.value = { ...status.value, ...(await SetIdentity(profileName)) }
  await refreshInterruptedMission()
}

async function refreshInterruptedMission() {
  const profileName = selectedProfile.value
  interruptedMission.value = null
  if (!profileName || profileName === '__create__' || !status.value.isServerOnline) return
  try {
    const mission = await GetInterruptedMission(profileName)
    if (selectedProfile.value === profileName && mission?.isAvailable) {
      interruptedMission.value = mission
      cancelAutoLaunchCountdown()
    }
  }
  catch (error) { recordError(error) }
}

async function refreshDetachedInterruptedMission() {
  const profileName = detachedProfile.value
  detachedInterruptedMission.value = null
  if (!profileName || profileName === '__create__' || !status.value.isServerOnline) return
  try {
    const mission = await GetInterruptedMission(profileName)
    if (detachedProfile.value === profileName && mission?.isAvailable) {
      detachedInterruptedMission.value = mission
    }
  }
  catch (error) { recordError(error) }
}

async function chooseProfile(profileName) {
  selectedProfile.value = profileName
  if (profileName !== '__create__') await selectProfile()
}


function chooseDetachedProfile(profileName) {
  detachedProfile.value = profileName
}

async function createDetachedProfile() {
  showPage('play')
  await chooseProfile('__create__')
}


async function play() {
  try {
    const profileName = selectedProfile.value
    if (!profileName || profileName === '__create__') return
    if (await IsProfileRunning(profileName)) {
      isRunningGameConfirmOpen.value = true
      return
    }
    await launchStandardGame()
  } catch (error) { recordError(error) }
}

async function launchStandardGame() {
    const profileName = selectedProfile.value
    if (!profileName || profileName === '__create__') return
    const offeredGameID = interruptedMission.value?.gameId
    await applyIdentity(profileName)
    const mission = await GetInterruptedMission(profileName)
    if (selectedProfile.value !== profileName) return
    interruptedMission.value = mission?.isAvailable ? mission : null
    // A newly discovered resume must expose Continue / Start Fresh first.
    if (mission?.isAvailable && offeredGameID !== mission.gameId) return
    await launchSelectedProfile()
}

async function replaceRunningGame() {
  if (isRunningGameConfirmBusy.value) return
  isRunningGameConfirmBusy.value = true
  try {
    await CloseRunningProfile(selectedProfile.value)
    isRunningGameConfirmOpen.value = false
    await launchStandardGame()
  }
  catch (error) { recordError(error) }
  finally { isRunningGameConfirmBusy.value = false }
}

async function launchSelectedProfile() {
  await Play()
  markProfilePlayed(selectedProfile.value)
  activeClientRoute.value = 'play'
  isGameLaunchedThisSession.value = true
}

async function startFresh() {
  if (isMissionChoiceBusy.value) return
  const isDetached = activePage.value === 'launcher'
  const profileName = isDetached ? detachedProfile.value : selectedProfile.value
  if (!profileName || profileName === '__create__') return
  isMissionChoiceBusy.value = true
  try {
    await DiscardInterruptedMission(profileName)
    if (isDetached) {
      detachedInterruptedMission.value = null
      await launchDetachedProfile(profileName)
      await refreshRunningProfiles()
    } else {
      interruptedMission.value = null
      await applyIdentity(profileName)
      await launchSelectedProfile()
    }
  }
  catch (error) { recordError(error) }
  finally { isMissionChoiceBusy.value = false }
}

async function deleteProfile() {
  const profile = profilePendingDelete.value
  if (!profile || isBusy.value) return
  isProfileDeletionBusy.value = true
  try {
    await DeleteProfile(profile.loginName)
    if (identity.value === profile.loginName) {
      localStorage.removeItem('darkspinner.identity')
      identity.value = ''
    }
    if (selectedProfile.value === profile.loginName) {
      localStorage.removeItem('darkspinner.selectedProfile')
      selectedProfile.value = ''
    }
    if (detachedProfile.value === profile.loginName) {
      localStorage.removeItem('darkspinner.detachedProfile')
      detachedProfile.value = ''
    }
    profilePendingDelete.value = null
    await refreshProfiles()
  }
  catch (error) { recordError(error) }
  finally { isProfileDeletionBusy.value = false }
}

function requestDeleteProfile() {
  if (!selectedProfileRecord.value || isBusy.value) return
  profilePendingDelete.value = selectedProfileRecord.value
}

function requestDetachedProfileDelete() {
  if (!detachedProfileRecord.value || isBusy.value || isDetachedLaunchBusy.value) return
  profilePendingDelete.value = detachedProfileRecord.value
}

function requestRemoteProfileDelete() {
  if (!selectedRemoteProfile.value || isRemoteBusy.value) return
  remoteProfilePendingDelete.value = selectedRemoteProfile.value
  remoteDeletionError.value = ''
}

function cancelRemoteProfileDelete() {
  if (isRemoteDeletionBusy.value) return
  remoteProfilePendingDelete.value = null
  remoteDeletionError.value = ''
}

async function deleteRemoteProfile() {
  const profile = remoteProfilePendingDelete.value
  if (!profile || isRemoteDeletionBusy.value) return
  isRemoteDeletionBusy.value = true
  remoteDeletionError.value = ''
  try {
    await DeleteRemoteProfile(profile.serverAddress, profile.loginName, remotePassword.value)
    remoteProfilePendingDelete.value = null
    remotePassword.value = ''
    await refreshRemoteProfiles()
    if (!remoteProfiles.value.length) {
      remoteIdentity.value = ''
    }
    remoteMessage.value = `Remote Crogenitor deleted from ${profile.serverAddress}.`
  }
  catch (error) { remoteDeletionError.value = visibleLauncherText(error) }
  finally { isRemoteDeletionBusy.value = false }
}

async function refreshInstallation() {
  isInstallationBusy.value = true
  try {
    installation.value = await RefreshInstallationStatus()
    if (installation.value.isLocalReady) await RestartLauncher()
  }
  catch (error) { recordError(error) }
  finally { isInstallationBusy.value = false }
}

async function openSteamInstall() {
  isInstallationBusy.value = true
  try { await OpenSteamDemoInstall() }
  catch (error) { recordError(error) }
  finally { isInstallationBusy.value = false }
}

function openReportComposer() {
  reportResult.value = null
  isReportComposerOpen.value = true
}

function closeReportComposer() {
  if (isReportBusy.value) return
  isReportComposerOpen.value = false
}

async function sendReport() {
  const title = reportTitle.value.trim()
  const description = reportDescription.value.trim()
  if (!title || !description || isReportBusy.value) return
  isReportBusy.value = true
  try {
    const report = await SendReport(title, description)
    reportResult.value = { ...report, title, description, version:status.value.version }
    reportShareMessage.value = ''
    isReportComposerOpen.value = false
    reportTitle.value = ''
    reportDescription.value = ''
  }
  catch (error) { recordError(error) }
  finally { isReportBusy.value = false }
}

async function openReportFolder() {
  try { await OpenReportFolder() }
  catch (error) { recordError(error) }
}

function openMyReports() {
  BrowserOpenURL(myReportsURL)
}

async function openReportIssue() {
  if (!reportResult.value) return
  reportShareMessage.value = ''
  try {
    if (reportIssueDraft.value.isLong) {
      const isCopied = await ClipboardSetText(reportIssueBody.value)
      if (!isCopied) throw new Error('Could not copy the report. The complete description is also in report.txt inside the ZIP.')
      reportShareMessage.value = 'Full report copied. Paste it into the GitHub description, then attach every report ZIP.'
    } else {
      reportShareMessage.value = 'GitHub opened with your report details. Open the bug folder and drag the ZIP into the issue before submitting.'
    }
    BrowserOpenURL(reportIssueDraft.value.url)
  }
  catch (error) { reportShareMessage.value = visibleLauncherText(error) }
}

async function openChangelog() {
  isChangelogOpen.value = true
  if (changelogText.value || isChangelogLoading.value) return
  isChangelogLoading.value = true
  changelogError.value = ''
  try {
    const response = await fetch('/changelog.md', { cache:'no-store' })
    if (!response.ok) throw new Error(`Changelog unavailable (${response.status})`)
    changelogText.value = await response.text()
  }
  catch (error) { changelogError.value = visibleLauncherText(error) }
  finally { isChangelogLoading.value = false }
}

async function relocateLauncher() {
  isInstallationBusy.value = true
  try {
    await RelocateToGameRoot({
      isStartMenuShortcut: isStartMenuShortcut.value,
      isDesktopShortcut: isDesktopShortcut.value,
      isSteamLaunch: isSteamLaunch.value,
    })
  }
  catch (error) {
    recordError(error)
    isInstallationBusy.value = false
  }
}

function cancelProfileCreation() {
  if (isFirstRunOnboarding.value) return
  selectedProfile.value = identity.value || profiles.value[0]?.loginName || ''
  newProfileName.value = ''
  isTutorialSkipped.value = true
}

async function maybeAutoLaunch() {
  if (activePage.value !== 'play' || !isAutoLaunchEnabled.value || hasAutoLaunchAttempted.value || !isPlayEnabled.value) return
  if (statusRows.value.some(row => row.error)) return
  await refreshInterruptedMission()
  if (interruptedMission.value || activePage.value !== 'play' || !isAutoLaunchEnabled.value || !isPlayEnabled.value || hasAutoLaunchAttempted.value) return
  hasAutoLaunchAttempted.value = true
  autoLaunchCountdown.value = 3
  autoLaunchTimer = window.setInterval(async () => {
    autoLaunchCountdown.value -= 1
    if (autoLaunchCountdown.value > 0) return
    window.clearInterval(autoLaunchTimer)
    autoLaunchTimer = null
    if (activePage.value !== 'play' || !isAutoLaunchEnabled.value || !isPlayEnabled.value) {
      return
    }
    await play()
  }, 1000)
}

async function requestPatch() {
  if (status.value.isPatchActive || isPreparationActive.value) {
    if (isPatchCancelRequested.value) return
    isPatchCancelRequested.value = true
    try { await CancelPatch() }
    catch (error) { recordError(error) }
    finally { isPatchCancelRequested.value = false }
    return
  }
  if (status.value.state === 'cancelled') {
    isPatchRequestBusy.value = true
    try { await RestartLauncher() }
    catch (error) { recordError(error) }
    finally { isPatchRequestBusy.value = false }
    return
  }
  if (isPatchRequestBusy.value || !status.value.isGameReady || status.value.isStartupBlocked) return
  isPatchRequestBusy.value = true
  try { await Patch() }
  catch (error) { recordError(error) }
  finally { isPatchRequestBusy.value = false }
}

async function maybeAutoPatch() {
  if (!isAutoPatchEnabled.value || status.value.state !== 'ready' ||
    status.value.patch !== 'Update available' || isPatchBusy.value) return
  await requestPatch()
}

function cancelAutoLaunchCountdown() {
  if (autoLaunchTimer !== null) window.clearInterval(autoLaunchTimer)
  autoLaunchTimer = null
  autoLaunchCountdown.value = 0
}

function suppressAutoLaunch() {
  hasAutoLaunchAttempted.value = true
  cancelAutoLaunchCountdown()
}

function handleLauncherInteraction(event) {
  if (!isAutoLaunchEnabled.value) return
  const target = event.target instanceof Element ? event.target : null
  if (target?.closest('.play-action')) return
  suppressAutoLaunch()
}

function handlePlayButton() {
  if (isAutoLaunchCounting.value) {
    cancelAutoLaunchCountdown()
    return
  }
  if (activePage.value === 'launcher') {
    void startDetachedGame()
    return
  }
  if (activePage.value === 'remote') {
    if (isRemoteRegistration.value) {
      void submitRemoteAccount()
      return
    }
    void launchRemote()
    return
  }
  void play()
}

async function stopActiveClient() {
	if (isClientStopBusy.value) return
	const isDetached = activePage.value === 'launcher'
	if (!isDetached && !isAttachedKillVisible.value) return
	isClientStopBusy.value = true
	cancelAutoLaunchCountdown()
	try {
		if (isDetached) await CloseDetachedGameInstances()
		else {
			await CloseRunningGame()
			activeClientRoute.value = ''
		}
		await refreshRunningProfiles()
  }
  catch (error) { recordError(error) }
  finally { isClientStopBusy.value = false }
}

function showPage(page) {
  if (page === activePage.value) return
  suppressAutoLaunch()
  if (page === 'launcher') {
    void refreshDetachedInterruptedMission()
  }
  if (page === 'remote') {
    void refreshRemoteProfiles()
  }
  if (page === 'config') {
    void refreshServerConfiguration()
    void refreshIntegrations()
  }
  activePage.value = page
}

async function refreshIntegrations() {
  try { integrationStatus.value = await GetLauncherIntegrationStatus() }
  catch (error) { recordError(error) }
}

async function repairIntegration(kind) {
  if (isManagementBusy.value) return
  isManagementBusy.value = true
  try { integrationStatus.value = await RepairLauncherIntegration(kind) }
  catch (error) { recordError(error) }
  finally { isManagementBusy.value = false }
}

async function removeIntegration(kind) {
  if (isManagementBusy.value) return
  isManagementBusy.value = true
  try { integrationStatus.value = await RemoveLauncherIntegration(kind) }
  catch (error) { recordError(error) }
  finally { isManagementBusy.value = false }
}

async function startDetachedGame() {
  if (!detachedProfile.value || isDetachedLaunchBusy.value) return
  isDetachedLaunchBusy.value = true
  try {
    const profileName = detachedProfile.value
    const offeredGameID = detachedInterruptedMission.value?.gameId
    const mission = await GetInterruptedMission(profileName)
    if (detachedProfile.value !== profileName) return
    detachedInterruptedMission.value = mission?.isAvailable ? mission : null
    if (mission?.isAvailable && offeredGameID !== mission.gameId) return
    await launchDetachedProfile(profileName)
    await refreshRunningProfiles()
  }
  catch (error) { recordError(error) }
  finally { isDetachedLaunchBusy.value = false }
}

async function launchDetachedProfile(profileName) {
  await StartDetachedGameInstance(profileName)
  markProfilePlayed(profileName)
  isGameLaunchedThisSession.value = true
}

async function uninstallDarkspinner() {
  if (isManagementBusy.value) return
  isManagementBusy.value = true
  try { await UninstallDarkspinner() }
  catch (error) {
    recordError(error)
    isManagementBusy.value = false
    isUninstallConfirmOpen.value = false
  }
}

function visibleLauncherText(message) {
  return String(message ?? '')
    .replace(/\bprofiles\b/giu, 'Crogenitors')
    .replace(/\bprofile\b/giu, 'Crogenitor')
}
function showLauncherNotice(message) {
  const notice = visibleLauncherText(message)
  if (notice && notice !== dismissedLauncherNotice) launcherNotice.value = notice
}
function dismissLauncherNotice() {
	if (status.value.isStartupBlocked) {
		Quit()
		return
	}
  dismissedLauncherNotice = launcherNotice.value
  launcherNotice.value = ''
}
function recordError(error) {
  status.value = { ...status.value, lastRun:visibleLauncherText(error), isLastRunFailure:true }
}
async function copyLauncherFailure() {
  if (!launcherFailure.value) return
  isLauncherFailureCopied.value = await ClipboardSetText(launcherFailure.value)
}
</script>

<template>
  <main class="spinner-shell" :class="{ onboarding:isInstallationRequired, management:activePage === 'launcher' || activePage === 'remote' || activePage === 'config', 'remote-accent':activePage === 'remote', 'detached-accent':activePage === 'launcher' }" @pointerdown.capture="handleLauncherInteraction" @keydown.capture="handleLauncherInteraction">
    <Tabs :model-value="activePage" @update:model-value="showPage" class="workspace" :class="{ 'navigation-hidden':isInstallationRequired }">
      <div v-if="!isInstallationRequired" class="workspace-navigation">
        <div class="workspace-wordmark" aria-label="Dark Spin"><span>DARK</span><strong>SPIN</strong></div>
        <TabsList aria-label="Launcher pages">
            <TabsTrigger value="play">Launch</TabsTrigger>
            <TabsTrigger v-if="isExperimentalLauncherFeatureVisible" value="remote">Remote</TabsTrigger>
            <TabsTrigger v-if="isExperimentalLauncherFeatureVisible" value="launcher">Detached</TabsTrigger>
            <TabsTrigger value="config">Config</TabsTrigger>
        </TabsList>
        <Button variant="outline" class="report-button" type="button" :disabled="isReportBusy" @click="openReportComposer">SEND REPORT</Button>
      </div>

      <section v-if="isInstallationRequired" class="onboarding-frame install-frame">
      <Card class="onboarding-card install-card">
        <div class="onboarding-index">00</div>
        <p class="eyebrow">INSTALLATION CHECK</p>
        <template v-if="!installation.isSteamInstalled">
          <h2>STEAM REQUIRED</h2>
          <p class="onboarding-copy">Steam must be installed before DarkSpinner can locate and launch the required game demo.</p>
          <div class="install-state error-state"><span></span><strong>STEAM NOT DETECTED</strong><small>Install Steam, then retry the scan.</small></div>
        </template>
        <template v-else-if="!installation.isGameInstalled">
          <h2>GAME DEMO REQUIRED</h2>
          <p class="onboarding-copy">The required demo is not installed in any detected Steam library.</p>
          <div class="install-state"><span></span><strong>STEAM READY</strong><small>Open the install prompt, then refresh when Steam finishes.</small></div>
        </template>
        <template v-else>
          <h2>FILES LOCATED</h2>
          <p v-if="installation.canRelocate" class="onboarding-copy">DarkSpinner works from the game’s root folder. Allow it to copy this launcher beside the detected files, restart there, and close this copy.</p>
          <p v-else class="onboarding-copy">DarkSpinner found the game, but automatic relocation is unavailable on this platform. Move this launcher beside the detected game folders, then restart it.</p>
          <div class="install-state ready-state"><span></span><strong>INSTALLATION READY</strong><small>No shipped game file will be replaced.</small></div>
          <div v-if="installation.canRelocate" class="integration-options">
            <label><Checkbox v-model="isStartMenuShortcut" /><span><strong>START MENU SHORTCUT</strong><small>Add Darkspinner to your personal Start menu.</small></span></label>
            <label><Checkbox v-model="isDesktopShortcut" /><span><strong>DESKTOP SHORTCUT</strong><small>Add Darkspinner to your personal desktop.</small></span></label>
            <label><Checkbox v-model="isSteamLaunch" /><span><strong>STEAM PLAY INTEGRATION</strong><small>Make Steam's Play button open Darkspinner using an additive compatibility DLL.</small></span></label>
          </div>
        </template>
        <p class="install-message">{{ installation.message }}</p>
        <div class="install-actions">
          <Button variant="default" type="button" v-if="installation.isSteamInstalled && !installation.isGameInstalled" class="onboarding-create" :disabled="isInstallationBusy" @click="openSteamInstall">OPEN STEAM INSTALL <span>&rsaquo;</span></Button>
          <Button variant="default" type="button" v-if="installation.canRelocate" class="onboarding-create" :disabled="isInstallationBusy" @click="relocateLauncher">{{ isInstallationBusy ? 'RESTARTING...' : isIntegrationSelected ? 'INSTALL AND RESTART' : 'COPY AND RESTART' }} <span>&rsaquo;</span></Button>
          <Button variant="outline" type="button" class="onboarding-cancel" :disabled="isInstallationBusy" @click="refreshInstallation">{{ installation.isSteamInstalled ? 'REFRESH' : 'RETRY SCAN' }}</Button>
        </div>
      </Card>
    </section>

    <TabsContent v-else-if="activePage === 'remote'" value="remote" as-child><section class="command-frame launch-command-frame remote-command-frame">
      <div class="launch-main">
        <Card class="bay launcher-bay">
          <div class="launch-pane">
            <div class="profile-core remote-profile-core">
              <div class="pilot-select-row">
                <Button variant="outline" class="identity-create" type="button" :disabled="isRemoteBusy || !profileAvatars.length" @click="beginRemoteRegistration">+ NEW</Button>
                <Select v-if="remoteProfiles.length" :model-value="selectedRemoteProfile ? `${selectedRemoteProfile.serverAddress}:${selectedRemoteProfile.loginName}` : undefined" @update:model-value="chooseRemoteProfileKey" :disabled="isRemoteBusy">
                  <SelectTrigger class="profile-select-trigger" aria-label="Remote Crogenitor"><SelectValue>{{ selectedRemoteProfileLabel }}</SelectValue></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="profile in remoteProfiles" :key="`${profile.serverAddress}:${profile.loginName}`" :value="`${profile.serverAddress}:${profile.loginName}`">
                      <span class="profile-option profile-detail-option"><span class="profile-option-photo"><img v-if="profile.avatarUrl" :src="profile.avatarUrl" alt=""></span><span class="profile-option-copy"><strong>{{ profile.displayName }}</strong><small>{{ profile.serverAddress }}</small></span><span class="profile-option-level">LEVEL {{ profile.crogenitorLevel }}</span><small class="profile-option-last-played">LAST PLAYED {{ profileLastPlayedDate(profile) }}</small></span>
                    </SelectItem>
                  </SelectContent>
                </Select>
                <Button variant="outline" v-if="remoteProfiles.length" class="identity-delete" type="button" :disabled="isRemoteBusy || !selectedRemoteProfile" @click="requestRemoteProfileDelete">DELETE</Button>
              </div>
              <div v-if="selectedRemoteProfile" class="launch-profile-summary">
                <div class="profile-info-card remote-profile-info-card" :aria-label="`${selectedRemoteProfileLabel} remote profile summary`">
                  <time v-if="profileCreateDate(selectedRemoteProfile)" class="profile-create-date" :datetime="selectedRemoteProfile.createDt">{{ profileCreateDate(selectedRemoteProfile) }}</time>
                  <div class="selected-profile-avatar-frame">
                    <div class="selected-profile-avatar" :class="{ empty:!selectedRemoteProfileAvatar }">
                      <img v-if="selectedRemoteProfileAvatar" :src="selectedRemoteProfileAvatar" :alt="`${selectedRemoteProfileLabel} Crogenitor photo`">
                    </div>
                    <span class="profile-level-badge" :aria-label="`Crogenitor level ${selectedRemoteProfile.crogenitorLevel}`">{{ selectedRemoteProfile.crogenitorLevel }}</span>
                  </div>
                  <div class="profile-info-content">
                    <div class="profile-info-heading"><small>REMOTE CROGENITOR</small><strong>{{ selectedRemoteProfileLabel }}</strong></div>
                    <div class="profile-progress">
                      <span><small>CROGENITOR</small><strong>{{ selectedRemoteProfile.crogenitorLevel }}</strong><em>{{ selectedRemoteProfile.cumulativeXp }} XP</em></span>
                      <span><small>PROGRESS</small><strong>{{ selectedRemoteCampaignLabel }}</strong><em>{{ selectedRemoteCampaignDetail }}</em></span>
                      <span class="remote-profile-server"><small>SERVER</small><strong>{{ selectedRemoteProfile.serverAddress }}</strong><em>{{ profileLastConnectedDate(selectedRemoteProfile) }}</em></span>
                    </div>
                    <div v-if="!selectedRemoteProfile.isPasswordRemembered" class="remote-profile-password">
                      <label for="remote-profile-password">PASSWORD</label>
                      <Input id="remote-profile-password" v-model="remotePassword" type="password" placeholder="ENTER PASSWORD" autocomplete="current-password" :disabled="isRemoteBusy" @keyup.enter="launchRemote" />
                      <label class="management-toggle remote-remember"><Checkbox v-model="isRemotePasswordRemembered" /><span><strong>REMEMBER PASSWORD</strong></span></label>
                    </div>
                  </div>
                </div>
              </div>
              <div v-else class="remote-empty-state">
                <strong>NO REMOTE CROGENITOR</strong>
                <small>Create one or sign into an existing account using + New.</small>
              </div>
            </div>
          </div>
        </Card>
      </div>
    </section></TabsContent>

    <TabsContent v-else-if="activePage === 'config'" value="config" as-child><section class="management-frame config-frame">
      <div class="config-grid">
        <Card class="management-card config-port-card">
          <template v-if="isExperimentalLauncherFeatureVisible">
            <label class="management-toggle config-multiplayer"><Checkbox v-model="isConfiguredMultiplayerEnabled" :disabled="isServerConfigurationBusy" /><span><strong>ALLOW MULTIPLAYER CONNECTIONS</strong><small>Still under development. Listen on LAN interfaces. Windows may request firewall access when enabled.</small></span></label>
            <label for="config-server-port">PORT</label>
            <Input id="config-server-port" v-model.number="configuredServerPort" type="number" min="1" max="65534" inputmode="numeric" :disabled="isServerConfigurationBusy" />
          </template>
          <label class="config-locale" for="config-client-locale"><strong>LANGUAGE</strong></label>
          <Select v-model="configuredLocale" :disabled="isServerConfigurationBusy">
            <SelectTrigger id="config-client-locale" class="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
            <SelectItem v-for="locale in serverConfiguration.locales" :key="locale.code" :value="locale.code">{{ locale.label }} — {{ locale.code }}</SelectItem>
          </SelectContent>
          </Select>
          <label class="config-locale" for="config-snapshot-mode"><strong>SYNC SNAPSHOT</strong></label>
          <Select v-model="configuredSnapshotMode" :disabled="isServerConfigurationBusy">
            <SelectTrigger id="config-snapshot-mode" class="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
            <SelectItem value="off">OFF — no rolling capture</SelectItem>
            <SelectItem value="manual">MANUAL — capture for /ss dump</SelectItem>
            <SelectItem value="auto">AUTO — capture and detect ordering anomalies</SelectItem>
          </SelectContent>
          </Select>
          <label v-if="status.buildChannel === 'development'" class="management-toggle config-borderless"><Checkbox v-model="isConfiguredBorderlessFullscreenEnabled" :disabled="isServerConfigurationBusy" /><span><strong>EXPERIMENTAL BORDERLESS FULLSCREEN</strong><small>Use Fang's borderless window and live-resolution compatibility hooks on the next game launch.</small></span></label>
          <p v-if="serverConfigurationMessage" class="management-message">{{ serverConfigurationMessage }}</p>
          <Button variant="default" class="config-save" type="button" :disabled="!isServerPortValid || !configuredLocale || isServerConfigurationBusy" @click="saveServerConfiguration">{{ isServerConfigurationBusy ? 'SAVING...' : 'SAVE CONFIGURATION' }}</Button>
        </Card>
        <Card class="management-card shortcuts-card">
          <div class="integration-row">
            <div><strong>START MENU</strong><small>Personal Darkspinner shortcut</small></div>
            <span :class="{ installed:integrationStatus.isStartMenuInstalled }">{{ integrationStatus.isStartMenuInstalled ? 'INSTALLED' : 'NOT INSTALLED' }}</span>
            <Button variant="outline" type="button" :disabled="isManagementBusy || !integrationStatus.isManagementSupported" @click="repairIntegration('start-menu')">{{ integrationStatus.isStartMenuInstalled ? 'REPAIR' : 'INSTALL' }}</Button>
            <Button variant="outline" type="button" :disabled="isManagementBusy || !integrationStatus.isStartMenuInstalled" @click="removeIntegration('start-menu')">REMOVE</Button>
          </div>
          <div class="integration-row">
            <div><strong>DESKTOP</strong><small>Personal Darkspinner shortcut</small></div>
            <span :class="{ installed:integrationStatus.isDesktopInstalled }">{{ integrationStatus.isDesktopInstalled ? 'INSTALLED' : 'NOT INSTALLED' }}</span>
            <Button variant="outline" type="button" :disabled="isManagementBusy || !integrationStatus.isManagementSupported" @click="repairIntegration('desktop')">{{ integrationStatus.isDesktopInstalled ? 'REPAIR' : 'INSTALL' }}</Button>
            <Button variant="outline" type="button" :disabled="isManagementBusy || !integrationStatus.isDesktopInstalled" @click="removeIntegration('desktop')">REMOVE</Button>
          </div>
          <div class="integration-row">
            <div><strong>STEAM PLAY</strong><small>Route Steam launch through Darkspinner</small></div>
            <span :class="{ installed:integrationStatus.isSteamLaunchInstalled }">{{ integrationStatus.isSteamLaunchInstalled ? 'INSTALLED' : 'NOT INSTALLED' }}</span>
            <Button variant="outline" type="button" :disabled="isManagementBusy || !integrationStatus.isManagementSupported" @click="repairIntegration('steam')">{{ integrationStatus.isSteamLaunchInstalled ? 'REPAIR' : 'INSTALL' }}</Button>
            <Button variant="outline" type="button" :disabled="isManagementBusy || !integrationStatus.isSteamLaunchInstalled" @click="removeIntegration('steam')">REMOVE</Button>
          </div>
          <p class="management-message">{{ integrationStatus.message }}</p>
          <div class="shortcut-danger">
            <p><strong>UNINSTALL DARKSPINNER</strong><small>Remove local runtime data and owned shortcuts while preserving the installed game.</small></p>
            <Button variant="destructive" type="button" class="danger-confirm" :disabled="isManagementBusy" @click="isUninstallConfirmOpen = true">UNINSTALL</Button>
          </div>
        </Card>
      </div>
    </section></TabsContent>

    <TabsContent v-else-if="activePage === 'play'" value="play" as-child><section class="command-frame launch-command-frame">
      <div class="launch-main">
        <Card class="bay launcher-bay">
          <div class="launch-pane">
            <div class="profile-core">
              <template v-if="profiles.length">
                <div class="pilot-select-row">
                  <Button variant="outline" class="identity-create" type="button" :disabled="isBusy" @click="chooseProfile('__create__')">+ NEW</Button>
                  <Select :model-value="selectedProfile" @update:model-value="chooseProfile" :disabled="isBusy">
                  <SelectTrigger class="profile-select-trigger" aria-label="Crogenitor"><SelectValue>{{ selectedProfileLabel }}</SelectValue></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="profile in profiles" :key="profile.loginName" :value="profile.loginName">
                      <span class="profile-option profile-detail-option"><span class="profile-option-photo"><img v-if="profile.avatarUrl" :src="profile.avatarUrl" alt=""></span><span class="profile-option-copy"><strong>{{ profile.displayName }}</strong><small v-if="profile.isTutorialCompletionPending || profile.displayName !== profile.loginName">{{ profile.isTutorialCompletionPending ? 'Starter loadout queued' : profile.loginName }}</small></span><span class="profile-option-level">LEVEL {{ profile.crogenitorLevel }}</span><small class="profile-option-last-played">LAST PLAYED {{ profileLastPlayedDate(profile) }}</small></span>
                    </SelectItem>
                  </SelectContent>
                </Select>
                  <Button variant="outline" class="identity-delete" type="button" :disabled="isBusy || !selectedProfileRecord" @click="requestDeleteProfile">DELETE</Button>
                </div>
                <div class="launch-profile-summary">
                  <div v-if="selectedProfileRecord" class="profile-info-card" :aria-label="`${selectedProfileLabel} profile summary`">
                    <time v-if="profileCreateDate(selectedProfileRecord)" class="profile-create-date" :datetime="selectedProfileRecord.createDt">{{ profileCreateDate(selectedProfileRecord) }}</time>
                    <div class="selected-profile-avatar-frame">
                      <div class="selected-profile-avatar" :class="{ empty:!selectedProfileAvatar }">
                        <img v-if="selectedProfileAvatar" :src="selectedProfileAvatar" :alt="`${selectedProfileLabel} Crogenitor photo`">
                      </div>
                      <span class="profile-level-badge" :aria-label="`Crogenitor level ${selectedProfileRecord.crogenitorLevel}`">{{ selectedProfileRecord.crogenitorLevel }}</span>
                    </div>
                    <div class="profile-info-content">
                      <div class="profile-info-heading"><small>LOCAL CROGENITOR</small><strong>{{ selectedProfileLabel }}</strong></div>
                      <div class="profile-progress">
                        <span><small>CROGENITOR</small><strong>{{ selectedProfileRecord.crogenitorLevel }}</strong><em>{{ selectedProfileRecord.cumulativeXp }} XP</em></span>
                        <span><small>PROGRESS</small><strong>{{ selectedCampaignLabel }}</strong><em>{{ selectedCampaignDetail }}</em></span>
                      </div>
                    </div>
                  </div>
                </div>
              </template>
            </div>
          </div>
        </Card>
      </div>
    </section></TabsContent>

    <TabsContent v-else value="launcher" as-child><section class="management-frame detached-frame">
      <div class="management-grid detached-grid">
        <Card class="bay launcher-bay detached-card">
          <div class="launch-pane">
            <div class="profile-core">
              <template v-if="profiles.length">
                <div class="pilot-select-row">
                  <Button variant="outline" class="identity-create" type="button" :disabled="isBusy || isDetachedLaunchBusy || isMissionChoiceBusy" @click="createDetachedProfile">+ NEW</Button>
                  <Select :model-value="detachedProfile" @update:model-value="chooseDetachedProfile" :disabled="isDetachedLaunchBusy || isMissionChoiceBusy">
                  <SelectTrigger class="profile-select-trigger" aria-label="Detached Crogenitor"><SelectValue>{{ detachedProfileLabel }}</SelectValue></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="profile in profiles" :key="profile.loginName" :value="profile.loginName">
                      <span class="profile-option profile-detail-option"><span class="profile-option-photo"><img v-if="profile.avatarUrl" :src="profile.avatarUrl" alt=""></span><span class="profile-option-copy"><strong>{{ profile.displayName }}</strong><small v-if="profile.isTutorialCompletionPending || profile.displayName !== profile.loginName">{{ profile.isTutorialCompletionPending ? 'Starter loadout queued' : profile.loginName }}</small></span><span class="profile-option-level">LEVEL {{ profile.crogenitorLevel }}</span><small class="profile-option-last-played">LAST PLAYED {{ profileLastPlayedDate(profile) }}</small></span>
                    </SelectItem>
                  </SelectContent>
                </Select>
                  <Button variant="outline" class="identity-delete" type="button" :disabled="isBusy || isDetachedLaunchBusy || !detachedProfileRecord" @click="requestDetachedProfileDelete">DELETE</Button>
                </div>
                <div class="launch-profile-summary">
                  <div v-if="detachedProfileRecord" class="profile-info-card" :aria-label="`${detachedProfileLabel} detached profile summary`">
                    <time v-if="profileCreateDate(detachedProfileRecord)" class="profile-create-date" :datetime="detachedProfileRecord.createDt">{{ profileCreateDate(detachedProfileRecord) }}</time>
                    <div class="selected-profile-avatar-frame">
                      <div class="selected-profile-avatar" :class="{ empty:!detachedProfileAvatar }">
                        <img v-if="detachedProfileAvatar" :src="detachedProfileAvatar" :alt="`${detachedProfileLabel} Crogenitor photo`">
                      </div>
                      <span class="profile-level-badge" :aria-label="`Crogenitor level ${detachedProfileRecord.crogenitorLevel}`">{{ detachedProfileRecord.crogenitorLevel }}</span>
                    </div>
                    <div class="profile-info-content">
                      <div class="profile-info-heading"><small>DETACHED CROGENITOR</small><strong>{{ detachedProfileLabel }}</strong></div>
                      <div class="profile-progress">
                        <span><small>CROGENITOR</small><strong>{{ detachedProfileRecord.crogenitorLevel }}</strong><em>{{ detachedProfileRecord.cumulativeXp }} XP</em></span>
                        <span><small>PROGRESS</small><strong>{{ detachedCampaignLabel }}</strong><em>{{ detachedCampaignDetail }}</em></span>
                      </div>
                    </div>
                  </div>
                </div>
              </template>
            </div>
          </div>
        </Card>
      </div>
    </section></TabsContent>

    </Tabs>

    <footer :class="{ 'launch-footer':!isInstallationRequired }">
      <div v-if="!isInstallationRequired" class="launcher-dock">
        <div v-if="launcherFailure" class="launcher-failure" role="alert">
          <p>{{ launcherFailure }}</p>
          <Button variant="outline" class="launcher-failure-copy" type="button" :class="{ copied:isLauncherFailureCopied }" :title="isLauncherFailureCopied ? 'Status copied' : 'Copy status'" :aria-label="isLauncherFailureCopied ? 'Status copied' : 'Copy status to clipboard'" @click="copyLauncherFailure">
            <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M5 4V2.75C5 1.78 5.78 1 6.75 1h5.5C13.22 1 14 1.78 14 2.75v7.5c0 .97-.78 1.75-1.75 1.75H11V6.75C11 5.23 9.77 4 8.25 4H5Zm-3 2.75C2 5.78 2.78 5 3.75 5h4.5C9.22 5 10 5.78 10 6.75v6.5c0 .97-.78 1.75-1.75 1.75h-4.5C2.78 15 2 14.22 2 13.25v-6.5Z"/></svg>
          </Button>
        </div>
        <div class="launcher-progress-row">
          <Button variant="ghost" class="footer-build" type="button" title="View changelog" aria-label="View build changelog" aria-haspopup="dialog" @click="openChangelog">{{ status.version || '—' }}</Button>
          <Progress class="launcher-progress" :class="{ indeterminate:isLauncherProgressIndeterminate }" :model-value="isLauncherProgressIndeterminate ? null : launcherProgress" :aria-label="launcherProgressLabel" :title="launcherProgressLabel" />
          <div class="launcher-status-dots" aria-label="System readiness">
            <span v-for="row in statusRows" :key="row.label" tabindex="0" :class="{ ready:row.ready, error:!!row.error }" :aria-label="`${row.label}: ${row.error || row.detail}`" :data-tooltip="`${row.label}: ${row.error || row.detail}`"></span>
          </div>
        </div>
        <div class="launcher-control-row">
          <Button variant="outline" class="launcher-action patch-action" type="button" :disabled="isPatchCancelRequested || status.state === 'cancelling' || (!isPatchBusy && status.state !== 'cancelled' && (!status.isGameReady || status.isStartupBlocked))" @click="requestPatch">{{ status.state === 'cancelling' || isPatchCancelRequested ? 'CANCELLING...' : isPatchBusy ? 'CANCEL' : 'PATCH' }}</Button>
          <div class="launcher-control-center">
            <div class="launcher-progress-status" :title="launcherProgressLabel">{{ launcherProgressLabel }}</div>
            <div class="launcher-auto-options">
              <label><Checkbox v-model="isAutoPatchEnabled" /><span>AUTO PATCH</span></label>
              <label><Checkbox v-model="isAutoLaunchEnabled" /><span>AUTO PLAY</span></label>
            </div>
          </div>
          <div class="launcher-play-group">
            <Button variant="destructive" v-if="isDetachedKillVisible" class="launcher-action detach-action kill-detached-action" type="button" :disabled="isClientStopBusy" title="Close every detached or untracked game client" @click="stopActiveClient">{{ isClientStopBusy ? 'CLOSING...' : 'KILL ALL' }}</Button>
			<Button variant="destructive" v-else-if="isAttachedKillVisible" class="launcher-action detach-action kill-detached-action" type="button" :disabled="isClientStopBusy" title="Close the active game client" @click="stopActiveClient">{{ isClientStopBusy ? 'CLOSING...' : 'KILL' }}</Button>
            <Button variant="outline" v-else-if="activeInterruptedMission" class="launcher-action detach-action mission-fresh" type="button" :disabled="!isHeaderPlayEnabled || isMissionChoiceBusy" title="Discard the interrupted deployment and start fresh" @click="startFresh">{{ isMissionChoiceBusy ? 'STARTING...' : 'START FRESH' }}</Button>
            <Button variant="default" class="launcher-action play-action" type="button" :class="{ counting:isAutoLaunchCounting }" :disabled="activePage === 'config' || (!isHeaderPlayEnabled && !isAutoLaunchCounting)" :title="dockPlayTitle" @click="handlePlayButton">{{ dockPlayLabel }}</Button>
          </div>
        </div>
      </div>
      <Button variant="ghost" v-else class="footer-build" type="button" title="View changelog" aria-label="View build changelog" aria-haspopup="dialog" @click="openChangelog">{{ status.version || '—' }}</Button>
      <div v-if="isInstallationRequired && footerStatus" class="footer-status" :class="footerStatus.level" role="status" aria-live="polite" :title="footerStatus.message">
        <span>{{ footerStatus.label }}</span>
        <small>{{ footerStatus.message }}</small>
      </div>
    </footer>

    <LauncherDialog @interact="handleLauncherInteraction" v-if="isCreatingProfile && activePage !== 'remote' && activePage !== 'config'" :is-dismissible="!isFirstRunOnboarding && !isProfileCreationBusy" @close="cancelProfileCreation">
      <div class="notice-card onboarding-card profile-creation-notice">
        <p v-if="isFirstRunOnboarding" class="eyebrow">FIRST CONTACT</p>
        <DialogTitle as="h2">{{ isFirstRunOnboarding ? 'CREATE YOUR CROGENITOR' : 'ADD A CROGENITOR' }}</DialogTitle>
        <p class="onboarding-copy">Choose the name used for this local Crogenitor.</p>
        <label>CROGENITOR PHOTO</label>
        <div v-if="profileAvatars.length" class="avatar-picker" role="radiogroup" aria-label="Crogenitor photo">
          <Button variant="outline" v-for="avatar in profileAvatars" :key="avatar.id" type="button" role="radio" :aria-checked="avatar.id === selectedAvatarId" :class="{ selected:avatar.id === selectedAvatarId }" @click="selectedAvatarId = avatar.id">
            <img :src="avatar.url" :alt="`Crogenitor photo ${avatar.id}`">
          </Button>
        </div>
        <div v-else class="avatar-preparing">{{ status.avatarError ? 'CROGENITOR PHOTOS UNAVAILABLE' : 'RECOVERING CROGENITOR PHOTOS...' }}</div>
        <label for="onboarding-profile">CROGENITOR NAME</label>
        <Input id="onboarding-profile" v-model="newProfileName" type="text" autofocus placeholder="ENTER NAME" :maxlength="maximumProfileNameLength" pattern="[A-Za-z0-9]*" autocomplete="off" spellcheck="false" :disabled="isProfileCreationBusy" @keyup.enter="createProfile" />
        <p class="onboarding-rule">Use 1–20 letters or numbers.</p>
        <label class="tutorial-skip"><Checkbox v-model="isTutorialSkipped" /><span><strong>SKIP TUTORIAL</strong><small>Begin on the ship with tutorial progression, starter heroes, DNA, and reward gear already granted.</small></span></label>
        <p v-if="isTutorialSkipped && !status.isContentReady" class="onboarding-rule waiting">The Crogenitor will be created now. Starter heroes and rewards will finish automatically when content preparation completes.</p>
        <div class="onboarding-actions">
          <Button variant="outline" type="button" v-if="!isFirstRunOnboarding" class="onboarding-cancel" :disabled="isProfileCreationBusy" @click="cancelProfileCreation">CANCEL</Button>
          <Button variant="default" type="button" class="onboarding-create" :disabled="isProfileCreationBusy || !isProfileCreationReady" @click="createProfile">
            {{ isProfileCreationBusy ? 'CREATING...' : 'CREATE CROGENITOR' }} <span>&rsaquo;</span>
          </Button>
        </div>
      </div>
    </LauncherDialog>

    <LauncherDialog @interact="handleLauncherInteraction" v-if="isRemoteRegistration" :is-dismissible="!isRemoteBusy" @close="cancelRemoteRegistration">
      <div class="notice-card onboarding-card profile-creation-notice remote-registration-notice">
        <p class="eyebrow">REMOTE CROGENITOR REGISTRATION</p>
        <DialogTitle as="h2">ADD A CROGENITOR</DialogTitle>
        <p class="onboarding-copy">Enter a server and credentials. If that account already exists, Darkspinner signs into it instead.</p>
        <label for="remote-server">SERVER</label>
        <div class="remote-registration-server">
          <Button variant="outline" type="button" :disabled="isRemoteBusy" @click="scanRemoteServers">{{ isRemoteBusy ? 'SUGGESTING...' : 'SUGGEST' }}</Button>
          <Input id="remote-server" v-model="remoteServerAddress" type="text" placeholder="darkspin.local:42127" autocomplete="off" spellcheck="false" :disabled="isRemoteBusy" />
        </div>
        <Select v-if="remoteServers.length" v-model:open="isRemoteServerMenuOpen" :model-value="remoteServerAddress" @update:model-value="chooseRemoteServerAddress" :disabled="isRemoteBusy">
                  <SelectTrigger class="profile-select-trigger" aria-label="Detected Darkspinner instances"><SelectValue>{{ selectedRemoteServer?.address || remoteServerAddress }}</SelectValue></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="server in remoteServers" :key="server.address" :value="server.address">
                      <span class="profile-option"><span><strong>{{ server.address }}</strong><small>{{ server.serverVersion || 'Darkspin' }}</small></span></span>
                    </SelectItem>
                  </SelectContent>
                </Select>
        <label>CROGENITOR PHOTO</label>
        <div class="avatar-picker" role="radiogroup" aria-label="Remote Crogenitor photo">
          <Button variant="outline" v-for="avatar in profileAvatars" :key="avatar.id" type="button" role="radio" :aria-checked="avatar.id === selectedAvatarId" :class="{ selected:avatar.id === selectedAvatarId }" @click="selectedAvatarId = avatar.id">
            <img :src="avatar.url" :alt="`Crogenitor photo ${avatar.id}`">
          </Button>
        </div>
        <label for="remote-identity">CROGENITOR NAME</label>
        <Input id="remote-identity" v-model="remoteIdentity" type="text" placeholder="ENTER NAME" maxlength="20" pattern="[A-Za-z0-9]*" autocomplete="username" spellcheck="false" :disabled="isRemoteBusy" />
        <label for="remote-password">PASSWORD</label>
        <Input id="remote-password" v-model="remotePassword" type="password" placeholder="ENTER PASSWORD" autocomplete="new-password" :disabled="isRemoteBusy" @keyup.enter="submitRemoteAccount" />
        <label class="tutorial-skip"><Checkbox v-model="isTutorialSkipped" :disabled="isRemoteBusy" /><span><strong>SKIP TUTORIAL</strong><small>Begin on the ship with tutorial progression, starter heroes, DNA, and reward gear already granted.</small></span></label>
        <label class="management-toggle remote-remember"><Checkbox v-model="isRemotePasswordRemembered" /><span><strong>REMEMBER PASSWORD</strong><small>Store this password in darkspin/saves/remote.db on this computer.</small></span></label>
        <p class="onboarding-rule">{{ remoteMessage }}</p>
        <div class="onboarding-actions">
          <Button variant="outline" class="onboarding-cancel" type="button" :disabled="isRemoteBusy" @click="cancelRemoteRegistration">CANCEL</Button>
          <Button variant="default" class="onboarding-create" type="button" :disabled="!isRemoteFormReady || isRemoteBusy" @click="submitRemoteAccount">{{ isRemoteBusy ? 'WORKING...' : 'REGISTER OR SIGN IN' }} <span>&rsaquo;</span></Button>
        </div>
      </div>
    </LauncherDialog>

    <LauncherDialog @interact="handleLauncherInteraction" v-if="isRunningGameConfirmOpen" :is-dismissible="!isRunningGameConfirmBusy" @close="isRunningGameConfirmOpen = false">
      <div class="notice-card">
        <p class="eyebrow">ACTIVE GAME SESSION</p>
        <DialogTitle as="h2">REPLACE RUNNING CLIENT?</DialogTitle>
        <p>The selected Crogenitor already owns a running client. Close only that Crogenitor's client before relaunching it, or keep the current session open.</p>
        <div class="notice-actions">
          <Button variant="outline" class="onboarding-cancel" type="button" :disabled="isRunningGameConfirmBusy" @click="isRunningGameConfirmOpen = false">KEEP CURRENT CLIENT</Button>
          <Button variant="destructive" class="danger-confirm" type="button" :disabled="isRunningGameConfirmBusy" @click="replaceRunningGame">{{ isRunningGameConfirmBusy ? 'CLOSING...' : 'CLOSE & START SELECTED' }}</Button>
        </div>
      </div>
    </LauncherDialog>

    <LauncherDialog @interact="handleLauncherInteraction" v-if="isMultiplayerConfirmOpen" :is-dismissible="true" @close="cancelMultiplayerConfiguration">
      <div class="notice-card network-notice">
        <p class="eyebrow">WINDOWS NETWORK ACCESS</p>
        <DialogTitle as="h2">ENABLE LAN MULTIPLAYER?</DialogTitle>
        <p>After Darkspinner restarts, Windows may ask which networks can reach it. Enable <strong>Private networks</strong>, then choose <strong>Allow access</strong>. Public networks are not required for normal LAN play.</p>
        <p class="network-recovery">If the Windows prompt does not appear or was previously dismissed, use <strong>Open Firewall</strong> in Config.</p>
        <div class="notice-actions">
          <Button variant="outline" class="onboarding-cancel" type="button" @click="cancelMultiplayerConfiguration">CANCEL</Button>
          <Button variant="default" class="onboarding-create" type="button" @click="confirmMultiplayerConfiguration">ENABLE &amp; RESTART</Button>
        </div>
      </div>
    </LauncherDialog>

    <LauncherDialog @interact="handleLauncherInteraction" v-if="launcherNotice">
      <div class="notice-card danger-notice">
        <p class="eyebrow">{{ status.isStartupBlocked ? 'PRIVILEGE CHECK' : 'LAUNCHER ATTENTION REQUIRED' }}</p>
        <DialogTitle as="h2">{{ status.isStartupBlocked ? 'STANDARD USER REQUIRED' : 'SETUP COULD NOT FINISH' }}</DialogTitle>
        <p>{{ launcherNotice }}</p>
        <div class="notice-actions">
          <Button variant="outline" class="onboarding-cancel" type="button" @click="dismissLauncherNotice">{{ status.isStartupBlocked ? 'EXIT DARKSPINNER' : 'CLOSE' }}</Button>
        </div>
      </div>
    </LauncherDialog>

    <LauncherDialog @interact="handleLauncherInteraction" v-if="isReportComposerOpen" :is-dismissible="!isReportBusy" @close="closeReportComposer">
      <div class="notice-card report-composer">
        <div class="report-composer-header">
          <Button variant="ghost" class="report-manage-link report-open-folder" type="button" @click="openReportFolder">OPEN BUG FOLDER ↗</Button>
          <a class="report-manage-link" :href="myReportsURL" @click.prevent="openMyReports">MANAGE MY REPORTS ↗</a>
        </div>
        <p class="eyebrow">LOCAL DIAGNOSTIC REPORT</p>
        <DialogTitle as="h2">WHAT HAPPENED?</DialogTitle>
        <p>Give the report a short title, then describe exactly what you were doing, what you expected, and what happened instead. More detail makes the captured logs easier to understand.</p>
        <form class="report-form" @submit.prevent="sendReport">
          <label for="report-title-input">TITLE</label>
          <Input id="report-title-input" v-model="reportTitle" type="text" autocomplete="off" placeholder="Example: Revenant ability crashed the game" :disabled="isReportBusy" />
          <label for="report-description-input">DESCRIPTION</label>
          <Textarea id="report-description-input" v-model="reportDescription" placeholder="Describe everything that may be relevant. You can write as much as you need." :disabled="isReportBusy"></Textarea>
          <div class="notice-actions">
            <Button variant="outline" class="onboarding-cancel" type="button" :disabled="isReportBusy" @click="closeReportComposer">CANCEL</Button>
            <Button variant="default" class="report-folder-button" type="submit" :disabled="!isReportReady">{{ isReportBusy ? 'PREPARING...' : 'CREATE REPORT' }}</Button>
          </div>
        </form>
      </div>
    </LauncherDialog>

    <LauncherDialog @interact="handleLauncherInteraction" v-if="profilePendingDelete" :is-dismissible="!isProfileDeletionBusy" @close="profilePendingDelete = null">
      <div class="notice-card danger-notice">
        <p class="eyebrow">IRREVERSIBLE ACTION</p>
        <DialogTitle as="h2">DELETE {{ profilePendingDelete.displayName }}?</DialogTitle>
        <p>This permanently removes the Crogenitor and all locally saved progress. This action cannot be undone.</p>
        <div class="notice-actions">
          <Button variant="outline" type="button" class="onboarding-cancel" :disabled="isProfileDeletionBusy" @click="profilePendingDelete = null">KEEP CROGENITOR</Button>
          <Button variant="destructive" type="button" class="danger-confirm" :disabled="isProfileDeletionBusy" @click="deleteProfile">{{ isProfileDeletionBusy ? 'DELETING...' : 'DELETE PERMANENTLY' }}</Button>
        </div>
      </div>
    </LauncherDialog>

    <LauncherDialog @interact="handleLauncherInteraction" v-if="remoteProfilePendingDelete" :is-dismissible="!isRemoteDeletionBusy" @close="cancelRemoteProfileDelete">
      <div class="notice-card danger-notice remote-delete-notice">
        <p class="eyebrow">REMOTE IRREVERSIBLE ACTION</p>
        <DialogTitle as="h2">DELETE {{ remoteProfilePendingDelete.displayName }}?</DialogTitle>
        <p>This permanently removes the Crogenitor and all progress from <strong>{{ remoteProfilePendingDelete.serverAddress }}</strong>, then removes its cached connection from this launcher.</p>
        <div v-if="!remoteProfilePendingDelete.isPasswordRemembered" class="remote-delete-password">
          <label for="remote-delete-password">PASSWORD</label>
          <Input id="remote-delete-password" v-model="remotePassword" type="password" autocomplete="current-password" placeholder="ENTER PASSWORD" :disabled="isRemoteDeletionBusy" @keyup.enter="deleteRemoteProfile" />
        </div>
        <p v-if="remoteDeletionError" class="remote-delete-error">{{ remoteDeletionError }}</p>
        <div class="notice-actions">
          <Button variant="outline" class="onboarding-cancel" type="button" :disabled="isRemoteDeletionBusy" @click="cancelRemoteProfileDelete">KEEP CROGENITOR</Button>
          <Button variant="destructive" class="danger-confirm" type="button" :disabled="isRemoteDeletionBusy || (!remoteProfilePendingDelete.isPasswordRemembered && !remotePassword)" @click="deleteRemoteProfile">{{ isRemoteDeletionBusy ? 'DELETING...' : 'DELETE REMOTELY' }}</Button>
        </div>
      </div>
    </LauncherDialog>

    <LauncherDialog @interact="handleLauncherInteraction" v-if="isChangelogOpen" :is-dismissible="true" @close="isChangelogOpen = false">
      <div class="notice-card changelog-notice">
        <p class="eyebrow">RELEASE HISTORY</p>
        <DialogTitle as="h2">DARK SPIN {{ status.version }}</DialogTitle>
        <p v-if="isChangelogLoading" class="changelog-state">LOADING CHANGELOG...</p>
        <p v-else-if="changelogError" class="changelog-state changelog-error">{{ changelogError }}</p>
        <pre v-else>{{ changelogText }}</pre>
        <div class="notice-actions">
          <Button variant="outline" class="onboarding-cancel" type="button" @click="isChangelogOpen = false">CLOSE</Button>
        </div>
      </div>
    </LauncherDialog>

    <LauncherDialog @interact="handleLauncherInteraction" v-if="reportResult" :is-dismissible="true" @close="reportResult = null">
      <div class="notice-card report-notice">
        <p class="eyebrow">DIAGNOSTIC ARCHIVE READY</p>
        <DialogTitle as="h2">REPORT CREATED</DialogTitle>
        <p>{{ reportResult.names.length }} ZIP(s) contain all available Darkspinner and protocol logs, with each ZIP at most 25 MB. Nothing was uploaded automatically; review all parts before sharing them.</p>
        <ul class="report-archives"><li v-for="name in reportResult.names" :key="name">{{ name }}</li></ul>
        <Button variant="outline" class="report-path" type="button" title="Open report folder" @click="openReportFolder">{{ reportResult.directory }}</Button>
        <p class="report-count">{{ reportResult.fileCount }} COMPLETE LOG FILES INCLUDED</p>
        <p>Sign in to GitHub to create an issue with your report details. Then open the bug folder and attach every listed ZIP before submitting. Large logs may span numbered chunks; the ZIPs include reassembly instructions.</p>
        <p v-if="reportIssueDraft.isLong">Your description is too long for a browser link. Create GitHub issue will copy the full report for you to paste into the issue.</p>
        <p v-if="reportShareMessage" class="report-share-message" role="status">{{ reportShareMessage }}</p>
        <div class="notice-actions">
          <Button variant="outline" class="onboarding-cancel" type="button" @click="reportResult = null">CLOSE</Button>
          <Button variant="default" class="report-folder-button" type="button" @click="openReportIssue">1. CREATE GITHUB ISSUE</Button>
          <Button variant="default" class="report-folder-button" type="button" @click="openReportFolder">2. OPEN BUG FOLDER</Button>
        </div>
      </div>
    </LauncherDialog>

    <LauncherDialog @interact="handleLauncherInteraction" v-if="isUninstallConfirmOpen" :is-dismissible="!isManagementBusy" @close="isUninstallConfirmOpen = false">
      <div class="notice-card danger-notice">
        <p class="eyebrow">IRREVERSIBLE ACTION</p>
        <DialogTitle as="h2">UNINSTALL DARKSPINNER?</DialogTitle>
        <p>This removes Darkspinner, all local Crogenitors and runtime data, its shortcuts, and its owned Steam launch integration. The original game installation remains intact.</p>
        <div class="notice-actions">
          <Button variant="outline" type="button" class="onboarding-cancel" :disabled="isManagementBusy" @click="isUninstallConfirmOpen = false">CANCEL</Button>
          <Button variant="destructive" type="button" class="danger-confirm" :disabled="isManagementBusy" @click="uninstallDarkspinner">{{ isManagementBusy ? 'UNINSTALLING...' : 'UNINSTALL PERMANENTLY' }}</Button>
        </div>
      </div>
    </LauncherDialog>

  </main>
</template>
