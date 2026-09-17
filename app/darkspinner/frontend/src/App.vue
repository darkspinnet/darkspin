<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { CancelPatch, CloseDetachedGameInstances, CloseRunningGame, CloseRunningProfile, CreateProfile, DeleteProfile, DeleteRemoteProfile, DiscardInterruptedMission, GetInstallationStatus, GetInterruptedMission, GetLauncherIntegrationStatus, GetProfileAvatars, GetProfiles, GetRemoteProfiles, GetServerConfiguration, GetStatus, HasDetachedGameInstances, IsProfileRunning, LaunchRemoteProfile, LoginRemoteProfile, OpenReportFolder, OpenSteamDemoInstall, Patch, Play, RefreshInstallationStatus, RegisterRemoteProfile, RelocateToGameRoot, RemoveLauncherIntegration, RepairLauncherIntegration, RestartLauncher, ScanRemoteServers, SendReport, SetIdentity, SetServerConfiguration, StartDetachedGameInstance, UninstallDarkspinner } from '../wailsjs/go/main/App'
import { BrowserOpenURL, ClipboardSetText, EventsOn, Quit } from '../wailsjs/runtime/runtime'

const status = ref({ state:'starting', message:'Starting DarkSpinner', identity:'', auth:'Starting', server:'Starting', patch:'Pending', game:'Checking', avatar:'Preparing', profile:'Starting', content:'Pending', identityError:'', authError:'', serverError:'', patchError:'', gameError:'', avatarError:'', profileError:'', contentError:'', lastRun:'', launcherNotice:'', version:'', progress:0, patchProgress:0, avatarProgress:0, contentProgress:0, isAuthenticated:false, isAuthOnline:false, isServerOnline:false, isPatchComplete:false, isPatchActive:false, isGameReady:false, isAvatarReady:false, isProfileStoreReady:false, isContentReady:false, isPlayReady:false, isCinematicSkipped:false, isLastRunFailure:false, isStartupBlocked:false })
const retainedProfileName = localStorage.getItem('darkspinner.selectedProfile') || localStorage.getItem('darkspinner.identity') || ''
const retainedDetachedProfileName = localStorage.getItem('darkspinner.detachedProfile') || ''
const identity = ref(localStorage.getItem('darkspinner.identity') || '')
const selectedProfile = ref(retainedProfileName)
const newProfileName = ref('')
const maximumProfileNameLength = 20
const profiles = ref([])
const profileAvatars = ref([])
const selectedAvatarId = ref(1)
const isTutorialSkipped = ref(false)
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
const serverConfiguration = ref({ port:42127, isMultiplayerEnabled:false, locale:'en-us', locales:[], snapshotMode:'off' })
const configuredServerPort = ref(42127)
const isConfiguredMultiplayerEnabled = ref(false)
const configuredLocale = ref('en-us')
const configuredSnapshotMode = ref('off')
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
const isProfileMenuOpen = ref(false)
const isDetachedProfileMenuOpen = ref(false)
const isRemoteProfileMenuOpen = ref(false)
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
  return `## ${report.title}\n\n${report.description}\n\n### Darkspinner build\n${report.version || 'Unknown'}\n\n### Diagnostic archive\n${report.name}\n\nAttach the ZIP from the bug folder before submitting this issue.`
})
const reportIssueDraft = computed(() => {
  const report = reportResult.value
  if (!report) return { url:'', isLong:false }
  const url = new URL('https://github.com/darkspinnet/darkspin/issues/new')
  url.searchParams.set('title', Array.from(report.title).slice(0, 160).join(''))
  url.searchParams.set('body', reportIssueBody.value)
  const isLong = url.href.length > 7000
  if (isLong) url.searchParams.set('body', 'Paste the complete report copied by Darkspinner here, then attach the ZIP from the bug folder before submitting.')
  return { url:url.href, isLong }
})
const isChangelogOpen = ref(false)
const isChangelogLoading = ref(false)
const changelogText = ref('')
const changelogError = ref('')
const profileSelect = ref(null)
const detachedProfileSelect = ref(null)
const remoteProfileSelect = ref(null)
const remoteServerSelect = ref(null)
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
  document.addEventListener('pointerdown', closeProfileMenu)
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
  document.removeEventListener('pointerdown', closeProfileMenu)
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
  }
  catch (error) { recordError(error) }
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
    )
    configuredServerPort.value = serverConfiguration.value.port
    isConfiguredMultiplayerEnabled.value = serverConfiguration.value.isMultiplayerEnabled
    configuredLocale.value = serverConfiguration.value.locale
    configuredSnapshotMode.value = serverConfiguration.value.snapshotMode
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

function selectRemoteProfile(profile) {
  remoteServerAddress.value = profile.serverAddress
  remoteIdentity.value = profile.loginName
  remotePassword.value = ''
  isRemotePasswordRemembered.value = profile.isPasswordRemembered
  isRemoteRegistration.value = false
  isRemoteProfileMenuOpen.value = false
  remoteMessage.value = profile.isPasswordRemembered ? 'Saved password ready.' : 'Enter the password for this Crogenitor.'
}

function beginRemoteRegistration() {
  isRemoteProfileMenuOpen.value = false
  isRemoteServerMenuOpen.value = false
  isRemoteRegistration.value = true
  remoteIdentity.value = ''
  remotePassword.value = ''
  isRemotePasswordRemembered.value = true
  isTutorialSkipped.value = false
  remoteMessage.value = 'Choose a Crogenitor photo, name, and password for this server.'
}

function cancelRemoteRegistration() {
  isRemoteRegistration.value = false
  isRemoteServerMenuOpen.value = false
  remotePassword.value = ''
  isTutorialSkipped.value = false
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
      isTutorialSkipped.value = false
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
    isTutorialSkipped.value = false
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
  isProfileMenuOpen.value = false
  if (profileName !== '__create__') await selectProfile()
}

function closeProfileMenu(event) {
  if (!profileSelect.value?.contains(event.target)) isProfileMenuOpen.value = false
  if (!detachedProfileSelect.value?.contains(event.target)) isDetachedProfileMenuOpen.value = false
  if (!remoteProfileSelect.value?.contains(event.target)) isRemoteProfileMenuOpen.value = false
  if (!remoteServerSelect.value?.contains(event.target)) isRemoteServerMenuOpen.value = false
}

function chooseDetachedProfile(profileName) {
  detachedProfile.value = profileName
  isDetachedProfileMenuOpen.value = false
}

async function createDetachedProfile() {
  isDetachedProfileMenuOpen.value = false
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
    await applyIdentity(profileName)
    const mission = await GetInterruptedMission(profileName)
    interruptedMission.value = mission?.isAvailable ? mission : null
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
  isProfileMenuOpen.value = false
}

function requestDetachedProfileDelete() {
  if (!detachedProfileRecord.value || isBusy.value || isDetachedLaunchBusy.value) return
  profilePendingDelete.value = detachedProfileRecord.value
  isDetachedProfileMenuOpen.value = false
}

function requestRemoteProfileDelete() {
  if (!selectedRemoteProfile.value || isRemoteBusy.value) return
  remoteProfilePendingDelete.value = selectedRemoteProfile.value
  remoteDeletionError.value = ''
  isRemoteProfileMenuOpen.value = false
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
      reportShareMessage.value = 'Full report copied. Paste it into the GitHub description, then attach the ZIP.'
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
  isTutorialSkipped.value = false
}

async function maybeAutoLaunch() {
  if (activePage.value !== 'play' || !isAutoLaunchEnabled.value || hasAutoLaunchAttempted.value || !isPlayEnabled.value) return
  if (statusRows.value.some(row => row.error)) return
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
    const mission = await GetInterruptedMission(profileName)
    if (detachedProfile.value !== profileName) return
    detachedInterruptedMission.value = mission?.isAvailable ? mission : null
    await launchDetachedProfile(profileName)
    await refreshRunningProfiles()
  }
  catch (error) { recordError(error) }
  finally { isDetachedLaunchBusy.value = false }
}

async function launchDetachedProfile(profileName) {
  await StartDetachedGameInstance(profileName)
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
  <main class="spinner-shell" :class="{ onboarding:isInstallationRequired, management:activePage === 'launcher' || activePage === 'remote' || activePage === 'config' }" @pointerdown.capture="handleLauncherInteraction" @keydown.capture="handleLauncherInteraction">
    <div class="backdrop-grid"></div>
    <div class="workspace" :class="{ 'navigation-hidden':isInstallationRequired }">
      <div v-if="!isInstallationRequired" class="workspace-navigation">
        <div class="workspace-wordmark" aria-label="Dark Spin"><span>DARK</span><strong>SPIN</strong></div>
        <nav class="page-tabs" aria-label="Launcher pages">
          <button :class="{ active:activePage === 'play' }" @click="showPage('play')">LAUNCH</button>
          <button :class="{ active:activePage === 'remote' }" @click="showPage('remote')">REMOTE</button>
          <button :class="{ active:activePage === 'launcher' }" @click="showPage('launcher')">DETACHED</button>
          <button :class="{ active:activePage === 'config' }" @click="showPage('config')">CONFIG</button>
        </nav>
        <button class="report-button" type="button" :disabled="isReportBusy" @click="openReportComposer">SEND REPORT</button>
      </div>

      <section v-if="isInstallationRequired" class="onboarding-frame install-frame">
      <article class="onboarding-card install-card">
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
            <label><input v-model="isStartMenuShortcut" type="checkbox"><span><strong>START MENU SHORTCUT</strong><small>Add Darkspinner to your personal Start menu.</small></span></label>
            <label><input v-model="isDesktopShortcut" type="checkbox"><span><strong>DESKTOP SHORTCUT</strong><small>Add Darkspinner to your personal desktop.</small></span></label>
            <label><input v-model="isSteamLaunch" type="checkbox"><span><strong>STEAM PLAY INTEGRATION</strong><small>Make Steam's Play button open Darkspinner using an additive compatibility DLL.</small></span></label>
          </div>
        </template>
        <p class="install-message">{{ installation.message }}</p>
        <div class="install-actions">
          <button v-if="installation.isSteamInstalled && !installation.isGameInstalled" class="onboarding-create" :disabled="isInstallationBusy" @click="openSteamInstall">OPEN STEAM INSTALL <span>&rsaquo;</span></button>
          <button v-if="installation.canRelocate" class="onboarding-create" :disabled="isInstallationBusy" @click="relocateLauncher">{{ isInstallationBusy ? 'RESTARTING...' : isIntegrationSelected ? 'INSTALL AND RESTART' : 'COPY AND RESTART' }} <span>&rsaquo;</span></button>
          <button class="onboarding-cancel" :disabled="isInstallationBusy" @click="refreshInstallation">{{ installation.isSteamInstalled ? 'REFRESH' : 'RETRY SCAN' }}</button>
        </div>
      </article>
    </section>

    <section v-else-if="activePage === 'remote'" class="command-frame launch-command-frame remote-command-frame">
      <div class="launch-main">
        <article class="bay launcher-bay">
          <div class="launch-pane">
            <div class="profile-core remote-profile-core">
              <div class="pilot-select-row">
                <button class="identity-create" type="button" :disabled="isRemoteBusy || !profileAvatars.length" @click="beginRemoteRegistration">+ NEW</button>
                <div v-if="remoteProfiles.length" ref="remoteProfileSelect" class="profile-select" :class="{ open:isRemoteProfileMenuOpen }">
                  <button class="profile-select-trigger" type="button" :disabled="isRemoteBusy" aria-label="Remote Crogenitor" :aria-expanded="isRemoteProfileMenuOpen" @click="isRemoteProfileMenuOpen = !isRemoteProfileMenuOpen">
                    <span>{{ selectedRemoteProfileLabel }}</span><i></i>
                  </button>
                  <div v-if="isRemoteProfileMenuOpen" class="profile-select-menu" role="listbox" aria-label="Remote Crogenitors">
                    <button v-for="profile in remoteProfiles" :key="`${profile.serverAddress}:${profile.loginName}`" class="profile-select-option" :class="{ selected:selectedRemoteProfile === profile }" type="button" role="option" :aria-selected="selectedRemoteProfile === profile" @click="selectRemoteProfile(profile)">
                      <img v-if="profile.avatarUrl" :src="profile.avatarUrl" alt=""><span><strong>{{ profile.displayName }}</strong><small>{{ profile.serverAddress }}</small></span>
                    </button>
                  </div>
                </div>
                <button v-if="remoteProfiles.length" class="identity-delete" type="button" :disabled="isRemoteBusy || !selectedRemoteProfile" @click="requestRemoteProfileDelete">DELETE</button>
              </div>
              <div v-if="selectedRemoteProfile" class="launch-profile-summary">
                <div class="selected-profile-avatar" :class="{ empty:!selectedRemoteProfileAvatar }">
                  <img v-if="selectedRemoteProfileAvatar" :src="selectedRemoteProfileAvatar" :alt="`${selectedRemoteProfileLabel} Crogenitor photo`">
                </div>
                <div class="profile-progress">
                  <span><small>CROGENITOR</small><strong>LEVEL {{ selectedRemoteProfile.crogenitorLevel }}</strong><em>{{ selectedRemoteProfile.cumulativeXp }} XP</em></span>
                  <span><small>PROGRESS</small><strong>{{ selectedRemoteCampaignLabel }}</strong><em>{{ selectedRemoteCampaignDetail }}</em></span>
                </div>
                <div class="remote-profile-server"><small>SERVER</small><strong>{{ selectedRemoteProfile.serverAddress }}</strong></div>
                <div v-if="!selectedRemoteProfile.isPasswordRemembered" class="remote-profile-password">
                  <label for="remote-profile-password">PASSWORD</label>
                  <input id="remote-profile-password" v-model="remotePassword" type="password" placeholder="ENTER PASSWORD" autocomplete="current-password" :disabled="isRemoteBusy" @keyup.enter="launchRemote">
                  <label class="management-toggle remote-remember"><input v-model="isRemotePasswordRemembered" type="checkbox"><span><strong>REMEMBER PASSWORD</strong></span></label>
                </div>
              </div>
              <div v-else class="remote-empty-state">
                <strong>NO REMOTE CROGENITOR</strong>
                <small>Create one or sign into an existing account using + New.</small>
              </div>
            </div>
          </div>
        </article>
      </div>
    </section>

    <section v-else-if="activePage === 'config'" class="management-frame config-frame">
      <div class="config-grid">
        <article class="management-card config-port-card">
          <div class="management-heading"><span>01</span><div><h2>CONFIGURATION</h2></div></div>
          <label class="management-toggle config-multiplayer"><input v-model="isConfiguredMultiplayerEnabled" type="checkbox" :disabled="isServerConfigurationBusy"><span><strong>ALLOW MULTIPLAYER CONNECTIONS</strong><small>Listen on LAN interfaces. Windows may request firewall access when enabled.</small></span></label>
          <label for="config-server-port">PORT</label>
          <input id="config-server-port" v-model.number="configuredServerPort" type="number" min="1" max="65534" inputmode="numeric" :disabled="isServerConfigurationBusy">
          <label class="config-locale" for="config-client-locale"><strong>LANGUAGE</strong></label>
          <select id="config-client-locale" v-model="configuredLocale" :disabled="isServerConfigurationBusy">
            <option v-for="locale in serverConfiguration.locales" :key="locale.code" :value="locale.code">{{ locale.label }} — {{ locale.code }}</option>
          </select>
          <label class="config-locale" for="config-snapshot-mode"><strong>SYNC SNAPSHOT</strong></label>
          <select id="config-snapshot-mode" v-model="configuredSnapshotMode" :disabled="isServerConfigurationBusy">
            <option value="off">OFF — no rolling capture</option>
            <option value="manual">MANUAL — capture for /ss dump</option>
            <option value="auto">AUTO — capture and detect ordering anomalies</option>
          </select>
          <p v-if="serverConfigurationMessage" class="management-message">{{ serverConfigurationMessage }}</p>
          <button class="config-save" type="button" :disabled="!isServerPortValid || !configuredLocale || isServerConfigurationBusy" @click="saveServerConfiguration">{{ isServerConfigurationBusy ? 'SAVING...' : 'SAVE CONFIGURATION' }}</button>
        </article>
        <article class="management-card shortcuts-card">
          <div class="management-heading"><span>02</span><div><h2>SHORTCUTS</h2><p>REPAIR OR REMOVE LAUNCH ENTRY POINTS</p></div></div>
          <div class="integration-row">
            <div><strong>START MENU</strong><small>Personal Darkspinner shortcut</small></div>
            <span :class="{ installed:integrationStatus.isStartMenuInstalled }">{{ integrationStatus.isStartMenuInstalled ? 'INSTALLED' : 'NOT INSTALLED' }}</span>
            <button :disabled="isManagementBusy || !integrationStatus.isManagementSupported" @click="repairIntegration('start-menu')">{{ integrationStatus.isStartMenuInstalled ? 'REPAIR' : 'INSTALL' }}</button>
            <button :disabled="isManagementBusy || !integrationStatus.isStartMenuInstalled" @click="removeIntegration('start-menu')">REMOVE</button>
          </div>
          <div class="integration-row">
            <div><strong>DESKTOP</strong><small>Personal Darkspinner shortcut</small></div>
            <span :class="{ installed:integrationStatus.isDesktopInstalled }">{{ integrationStatus.isDesktopInstalled ? 'INSTALLED' : 'NOT INSTALLED' }}</span>
            <button :disabled="isManagementBusy || !integrationStatus.isManagementSupported" @click="repairIntegration('desktop')">{{ integrationStatus.isDesktopInstalled ? 'REPAIR' : 'INSTALL' }}</button>
            <button :disabled="isManagementBusy || !integrationStatus.isDesktopInstalled" @click="removeIntegration('desktop')">REMOVE</button>
          </div>
          <div class="integration-row">
            <div><strong>STEAM PLAY</strong><small>Route Steam launch through Darkspinner</small></div>
            <span :class="{ installed:integrationStatus.isSteamLaunchInstalled }">{{ integrationStatus.isSteamLaunchInstalled ? 'INSTALLED' : 'NOT INSTALLED' }}</span>
            <button :disabled="isManagementBusy || !integrationStatus.isManagementSupported" @click="repairIntegration('steam')">{{ integrationStatus.isSteamLaunchInstalled ? 'REPAIR' : 'INSTALL' }}</button>
            <button :disabled="isManagementBusy || !integrationStatus.isSteamLaunchInstalled" @click="removeIntegration('steam')">REMOVE</button>
          </div>
          <p class="management-message">{{ integrationStatus.message }}</p>
          <div class="shortcut-danger">
            <p><strong>UNINSTALL DARKSPINNER</strong><small>Remove local runtime data and owned shortcuts while preserving the installed game.</small></p>
            <button class="danger-confirm" :disabled="isManagementBusy" @click="isUninstallConfirmOpen = true">UNINSTALL</button>
          </div>
        </article>
      </div>
    </section>

    <section v-else-if="activePage === 'play'" class="command-frame launch-command-frame">
      <div class="launch-main">
        <article class="bay launcher-bay">
          <div class="launch-pane">
            <div class="profile-core">
              <template v-if="profiles.length">
                <div class="pilot-select-row">
                  <button class="identity-create" type="button" :disabled="isBusy" @click="chooseProfile('__create__')">+ NEW</button>
                  <div ref="profileSelect" class="profile-select" :class="{ open:isProfileMenuOpen }">
                    <button class="profile-select-trigger" type="button" :disabled="isBusy" aria-label="Crogenitor" :aria-expanded="isProfileMenuOpen" @click="isProfileMenuOpen = !isProfileMenuOpen">
                      <span>{{ selectedProfileLabel }}</span><i></i>
                    </button>
                    <div v-if="isProfileMenuOpen" class="profile-select-menu" role="listbox" aria-label="Crogenitors">
                      <button v-for="profile in profiles" :key="profile.loginName" class="profile-select-option" :class="{ selected:profile.loginName === selectedProfile }" type="button" role="option" :aria-selected="profile.loginName === selectedProfile" @click="chooseProfile(profile.loginName)">
                        <img v-if="profile.avatarUrl" :src="profile.avatarUrl" alt=""><span><strong>{{ profile.displayName }}</strong><small v-if="profile.isTutorialCompletionPending">STARTER LOADOUT QUEUED</small><small v-else-if="profile.displayName !== profile.loginName">{{ profile.loginName }}</small></span>
                      </button>
                    </div>
                  </div>
                  <button class="identity-delete" type="button" :disabled="isBusy || !selectedProfileRecord" @click="requestDeleteProfile">DELETE</button>
                </div>
                <div class="launch-profile-summary">
                  <div class="selected-profile-avatar" :class="{ empty:!selectedProfileAvatar }">
                    <img v-if="selectedProfileAvatar" :src="selectedProfileAvatar" :alt="`${selectedProfileLabel} Crogenitor photo`">
                  </div>
                  <div v-if="selectedProfileRecord" class="profile-progress">
                    <span><small>CROGENITOR</small><strong>LEVEL {{ selectedProfileRecord.crogenitorLevel }}</strong><em>{{ selectedProfileRecord.cumulativeXp }} XP</em></span>
                    <span><small>PROGRESS</small><strong>{{ selectedCampaignLabel }}</strong><em>{{ selectedCampaignDetail }}</em></span>
                  </div>
                </div>
              </template>
            </div>
          </div>
        </article>
      </div>
    </section>

    <section v-else class="management-frame detached-frame">
      <div class="management-grid detached-grid">
        <article class="management-card detached-card">
          <div class="launch-pane">
            <div class="profile-core">
              <template v-if="profiles.length">
                <div class="pilot-select-row">
                  <button class="identity-create" type="button" :disabled="isBusy || isDetachedLaunchBusy || isMissionChoiceBusy" @click="createDetachedProfile">+ NEW</button>
                  <div ref="detachedProfileSelect" class="profile-select" :class="{ open:isDetachedProfileMenuOpen }">
                    <button class="profile-select-trigger" type="button" :disabled="isDetachedLaunchBusy || isMissionChoiceBusy" aria-label="Detached Crogenitor" :aria-expanded="isDetachedProfileMenuOpen" @click="isDetachedProfileMenuOpen = !isDetachedProfileMenuOpen">
                      <span>{{ detachedProfileLabel }}</span><i></i>
                    </button>
                    <div v-if="isDetachedProfileMenuOpen" class="profile-select-menu" role="listbox" aria-label="Detached Crogenitors">
                      <button v-for="profile in profiles" :key="profile.loginName" class="profile-select-option" :class="{ selected:profile.loginName === detachedProfile }" type="button" role="option" :aria-selected="profile.loginName === detachedProfile" @click="chooseDetachedProfile(profile.loginName)">
                        <img v-if="profile.avatarUrl" :src="profile.avatarUrl" alt=""><span><strong>{{ profile.displayName }}</strong><small v-if="profile.isTutorialCompletionPending">STARTER LOADOUT QUEUED</small><small v-else-if="profile.displayName !== profile.loginName">{{ profile.loginName }}</small></span>
                      </button>
                    </div>
                  </div>
                  <button class="identity-delete" type="button" :disabled="isBusy || isDetachedLaunchBusy || !detachedProfileRecord" @click="requestDetachedProfileDelete">DELETE</button>
                </div>
                <div class="launch-profile-summary">
                  <div class="selected-profile-avatar" :class="{ empty:!detachedProfileAvatar }">
                    <img v-if="detachedProfileAvatar" :src="detachedProfileAvatar" :alt="`${detachedProfileLabel} Crogenitor photo`">
                  </div>
                  <div v-if="detachedProfileRecord" class="profile-progress">
                    <span><small>CROGENITOR</small><strong>LEVEL {{ detachedProfileRecord.crogenitorLevel }}</strong><em>{{ detachedProfileRecord.cumulativeXp }} XP</em></span>
                    <span><small>PROGRESS</small><strong>{{ detachedCampaignLabel }}</strong><em>{{ detachedCampaignDetail }}</em></span>
                  </div>
                </div>
              </template>
            </div>
          </div>
        </article>
      </div>
    </section>

    </div>

    <footer :class="{ 'launch-footer':!isInstallationRequired }">
      <div v-if="!isInstallationRequired" class="launcher-dock">
        <div v-if="launcherFailure" class="launcher-failure" role="alert">
          <p>{{ launcherFailure }}</p>
          <button class="launcher-failure-copy" type="button" :class="{ copied:isLauncherFailureCopied }" :title="isLauncherFailureCopied ? 'Status copied' : 'Copy status'" :aria-label="isLauncherFailureCopied ? 'Status copied' : 'Copy status to clipboard'" @click="copyLauncherFailure">
            <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M5 4V2.75C5 1.78 5.78 1 6.75 1h5.5C13.22 1 14 1.78 14 2.75v7.5c0 .97-.78 1.75-1.75 1.75H11V6.75C11 5.23 9.77 4 8.25 4H5Zm-3 2.75C2 5.78 2.78 5 3.75 5h4.5C9.22 5 10 5.78 10 6.75v6.5c0 .97-.78 1.75-1.75 1.75h-4.5C2.78 15 2 14.22 2 13.25v-6.5Z"/></svg>
          </button>
        </div>
        <div class="launcher-progress-row">
          <button class="footer-build" type="button" title="View changelog" aria-label="View build changelog" aria-haspopup="dialog" @click="openChangelog">{{ status.version || '—' }}</button>
          <div class="launcher-progress" :class="{ indeterminate:isLauncherProgressIndeterminate }" :title="launcherProgressLabel">
            <i :style="isLauncherProgressIndeterminate ? {} : { width:`${launcherProgress}%` }"></i>
          </div>
          <div class="launcher-status-dots" aria-label="System readiness">
            <span v-for="row in statusRows" :key="row.label" tabindex="0" :class="{ ready:row.ready, error:!!row.error }" :data-tooltip="`${row.label}: ${row.error || row.detail}`"></span>
          </div>
        </div>
        <div class="launcher-control-row">
          <button class="launcher-action patch-action" type="button" :disabled="isPatchCancelRequested || status.state === 'cancelling' || (!isPatchBusy && status.state !== 'cancelled' && (!status.isGameReady || status.isStartupBlocked))" @click="requestPatch">{{ status.state === 'cancelling' || isPatchCancelRequested ? 'CANCELLING...' : isPatchBusy ? 'CANCEL' : 'PATCH' }}</button>
          <div class="launcher-control-center">
            <div class="launcher-progress-status" :title="launcherProgressLabel">{{ launcherProgressLabel }}</div>
            <div class="launcher-auto-options">
              <label><input v-model="isAutoPatchEnabled" type="checkbox"><span>AUTO PATCH</span></label>
              <label><input v-model="isAutoLaunchEnabled" type="checkbox"><span>AUTO PLAY</span></label>
            </div>
          </div>
          <div class="launcher-play-group">
            <button v-if="isDetachedKillVisible" class="launcher-action detach-action kill-detached-action" type="button" :disabled="isClientStopBusy" title="Close every detached or untracked game client" @click="stopActiveClient">{{ isClientStopBusy ? 'CLOSING...' : 'KILL ALL' }}</button>
			<button v-else-if="isAttachedKillVisible" class="launcher-action detach-action kill-detached-action" type="button" :disabled="isClientStopBusy" title="Close the active game client" @click="stopActiveClient">{{ isClientStopBusy ? 'CLOSING...' : 'KILL' }}</button>
            <button v-else-if="activeInterruptedMission" class="launcher-action detach-action mission-fresh" type="button" :disabled="!isHeaderPlayEnabled || isMissionChoiceBusy" title="Discard the interrupted deployment and start fresh" @click="startFresh">{{ isMissionChoiceBusy ? 'STARTING...' : 'START FRESH' }}</button>
            <button class="launcher-action play-action" type="button" :class="{ counting:isAutoLaunchCounting }" :disabled="activePage === 'config' || (!isHeaderPlayEnabled && !isAutoLaunchCounting)" :title="dockPlayTitle" @click="handlePlayButton">{{ dockPlayLabel }}</button>
          </div>
        </div>
      </div>
      <button v-else class="footer-build" type="button" title="View changelog" aria-label="View build changelog" aria-haspopup="dialog" @click="openChangelog">{{ status.version || '—' }}</button>
      <div v-if="isInstallationRequired && footerStatus" class="footer-status" :class="footerStatus.level" role="status" aria-live="polite" :title="footerStatus.message">
        <span>{{ footerStatus.label }}</span>
        <small>{{ footerStatus.message }}</small>
      </div>
    </footer>

    <section v-if="isCreatingProfile && activePage !== 'remote' && activePage !== 'config'" class="fullscreen-notice" role="dialog" aria-modal="true" aria-labelledby="profile-creation-title" @click.self="cancelProfileCreation">
      <article class="notice-card onboarding-card profile-creation-notice">
        <div class="onboarding-index">01</div>
        <p class="eyebrow">{{ isFirstRunOnboarding ? 'FIRST CONTACT' : 'LOCAL CROGENITOR REGISTRATION' }}</p>
        <h2 id="profile-creation-title">{{ isFirstRunOnboarding ? 'CREATE YOUR CROGENITOR' : 'ADD A CROGENITOR' }}</h2>
        <p class="onboarding-copy">Choose the name used for this local Crogenitor.</p>
        <label>CROGENITOR PHOTO</label>
        <div v-if="profileAvatars.length" class="avatar-picker" role="radiogroup" aria-label="Crogenitor photo">
          <button v-for="avatar in profileAvatars" :key="avatar.id" type="button" role="radio" :aria-checked="avatar.id === selectedAvatarId" :class="{ selected:avatar.id === selectedAvatarId }" @click="selectedAvatarId = avatar.id">
            <img :src="avatar.url" :alt="`Crogenitor photo ${avatar.id}`">
          </button>
        </div>
        <div v-else class="avatar-preparing">{{ status.avatarError ? 'CROGENITOR PHOTOS UNAVAILABLE' : 'RECOVERING CROGENITOR PHOTOS...' }}</div>
        <label for="onboarding-profile">CROGENITOR NAME</label>
        <input id="onboarding-profile" v-model="newProfileName" type="text" autofocus placeholder="ENTER NAME" :maxlength="maximumProfileNameLength" pattern="[A-Za-z0-9]*" autocomplete="off" spellcheck="false" :disabled="isProfileCreationBusy" @keyup.enter="createProfile">
        <p class="onboarding-rule">Use 1–20 letters or numbers.</p>
        <label class="tutorial-skip"><input v-model="isTutorialSkipped" type="checkbox"><span><strong>SKIP TUTORIAL</strong><small>Begin on the ship with tutorial progression, starter heroes, DNA, and reward gear already granted.</small></span></label>
        <p v-if="isTutorialSkipped && !status.isContentReady" class="onboarding-rule waiting">The Crogenitor will be created now. Starter heroes and rewards will finish automatically when content preparation completes.</p>
        <div class="onboarding-actions">
          <button v-if="!isFirstRunOnboarding" class="onboarding-cancel" :disabled="isProfileCreationBusy" @click="cancelProfileCreation">CANCEL</button>
          <button class="onboarding-create" :disabled="isProfileCreationBusy || !isProfileCreationReady" @click="createProfile">
            {{ isProfileCreationBusy ? 'CREATING...' : 'CREATE CROGENITOR' }} <span>&rsaquo;</span>
          </button>
        </div>
      </article>
    </section>

    <section v-if="isRemoteRegistration" class="fullscreen-notice" role="dialog" aria-modal="true" aria-labelledby="remote-registration-title" @click.self="cancelRemoteRegistration">
      <article class="notice-card onboarding-card profile-creation-notice remote-registration-notice">
        <p class="eyebrow">REMOTE CROGENITOR REGISTRATION</p>
        <h2 id="remote-registration-title">ADD A CROGENITOR</h2>
        <p class="onboarding-copy">Enter a server and credentials. If that account already exists, Darkspinner signs into it instead.</p>
        <label for="remote-server">SERVER</label>
        <div class="remote-registration-server">
          <button type="button" :disabled="isRemoteBusy" @click="scanRemoteServers">{{ isRemoteBusy ? 'SUGGESTING...' : 'SUGGEST' }}</button>
          <input id="remote-server" v-model="remoteServerAddress" type="text" placeholder="darkspin.local:42127" autocomplete="off" spellcheck="false" :disabled="isRemoteBusy">
        </div>
        <div v-if="remoteServers.length" ref="remoteServerSelect" class="profile-select remote-server-select" :class="{ open:isRemoteServerMenuOpen }">
          <button class="profile-select-trigger" type="button" :disabled="isRemoteBusy" aria-label="Detected Darkspinner instances" :aria-expanded="isRemoteServerMenuOpen" @click="isRemoteServerMenuOpen = !isRemoteServerMenuOpen">
            <span>{{ selectedRemoteServer?.address || remoteServerAddress }}</span><i></i>
          </button>
          <div v-if="isRemoteServerMenuOpen" class="profile-select-menu" role="listbox" aria-label="Detected Darkspinner instances">
            <button v-for="server in remoteServers" :key="server.address" class="profile-select-option remote-server-option" :class="{ selected:selectedRemoteServer === server }" type="button" role="option" :aria-selected="selectedRemoteServer === server" @click="selectRemoteServer(server)">
              <span><strong>{{ server.address }}</strong><small>{{ server.serverVersion || 'Darkspin' }}</small></span>
            </button>
          </div>
        </div>
        <label>CROGENITOR PHOTO</label>
        <div class="avatar-picker" role="radiogroup" aria-label="Remote Crogenitor photo">
          <button v-for="avatar in profileAvatars" :key="avatar.id" type="button" role="radio" :aria-checked="avatar.id === selectedAvatarId" :class="{ selected:avatar.id === selectedAvatarId }" @click="selectedAvatarId = avatar.id">
            <img :src="avatar.url" :alt="`Crogenitor photo ${avatar.id}`">
          </button>
        </div>
        <label for="remote-identity">CROGENITOR NAME</label>
        <input id="remote-identity" v-model="remoteIdentity" type="text" placeholder="ENTER NAME" maxlength="20" pattern="[A-Za-z0-9]*" autocomplete="username" spellcheck="false" :disabled="isRemoteBusy">
        <label for="remote-password">PASSWORD</label>
        <input id="remote-password" v-model="remotePassword" type="password" placeholder="ENTER PASSWORD" autocomplete="new-password" :disabled="isRemoteBusy" @keyup.enter="submitRemoteAccount">
        <label class="tutorial-skip"><input v-model="isTutorialSkipped" type="checkbox" :disabled="isRemoteBusy"><span><strong>SKIP TUTORIAL</strong><small>Begin on the ship with tutorial progression, starter heroes, DNA, and reward gear already granted.</small></span></label>
        <label class="management-toggle remote-remember"><input v-model="isRemotePasswordRemembered" type="checkbox"><span><strong>REMEMBER PASSWORD</strong><small>Store this password in darkspin/saves/remote.db on this computer.</small></span></label>
        <p class="onboarding-rule">{{ remoteMessage }}</p>
        <div class="onboarding-actions">
          <button class="onboarding-cancel" type="button" :disabled="isRemoteBusy" @click="cancelRemoteRegistration">CANCEL</button>
          <button class="onboarding-create" type="button" :disabled="!isRemoteFormReady || isRemoteBusy" @click="submitRemoteAccount">{{ isRemoteBusy ? 'WORKING...' : 'REGISTER OR SIGN IN' }} <span>&rsaquo;</span></button>
        </div>
      </article>
    </section>

    <section v-if="isRunningGameConfirmOpen" class="fullscreen-notice" role="dialog" aria-modal="true" aria-labelledby="running-game-title">
      <article class="notice-card">
        <p class="eyebrow">ACTIVE GAME SESSION</p>
        <h2 id="running-game-title">REPLACE RUNNING CLIENT?</h2>
        <p>The selected Crogenitor already owns a running client. Close only that Crogenitor's client before relaunching it, or keep the current session open.</p>
        <div class="notice-actions">
          <button class="onboarding-cancel" type="button" :disabled="isRunningGameConfirmBusy" @click="isRunningGameConfirmOpen = false">KEEP CURRENT CLIENT</button>
          <button class="danger-confirm" type="button" :disabled="isRunningGameConfirmBusy" @click="replaceRunningGame">{{ isRunningGameConfirmBusy ? 'CLOSING...' : 'CLOSE & START SELECTED' }}</button>
        </div>
      </article>
    </section>

    <section v-if="isMultiplayerConfirmOpen" class="fullscreen-notice" role="dialog" aria-modal="true" aria-labelledby="multiplayer-confirm-title">
      <article class="notice-card network-notice">
        <p class="eyebrow">WINDOWS NETWORK ACCESS</p>
        <h2 id="multiplayer-confirm-title">ENABLE LAN MULTIPLAYER?</h2>
        <p>After Darkspinner restarts, Windows may ask which networks can reach it. Enable <strong>Private networks</strong>, then choose <strong>Allow access</strong>. Public networks are not required for normal LAN play.</p>
        <p class="network-recovery">If the Windows prompt does not appear or was previously dismissed, use <strong>Open Firewall</strong> in Config.</p>
        <div class="notice-actions">
          <button class="onboarding-cancel" type="button" @click="cancelMultiplayerConfiguration">CANCEL</button>
          <button class="onboarding-create" type="button" @click="confirmMultiplayerConfiguration">ENABLE &amp; RESTART</button>
        </div>
      </article>
    </section>

    <section v-if="launcherNotice" class="fullscreen-notice" role="alertdialog" aria-modal="true" aria-labelledby="launcher-notice-title">
      <article class="notice-card danger-notice">
        <p class="eyebrow">{{ status.isStartupBlocked ? 'PRIVILEGE CHECK' : 'LAUNCHER ATTENTION REQUIRED' }}</p>
        <h2 id="launcher-notice-title">{{ status.isStartupBlocked ? 'STANDARD USER REQUIRED' : 'SETUP COULD NOT FINISH' }}</h2>
        <p>{{ launcherNotice }}</p>
        <div class="notice-actions">
          <button class="onboarding-cancel" type="button" @click="dismissLauncherNotice">{{ status.isStartupBlocked ? 'EXIT DARKSPINNER' : 'CLOSE' }}</button>
        </div>
      </article>
    </section>

    <section v-if="isReportComposerOpen" class="fullscreen-notice" role="dialog" aria-modal="true" aria-labelledby="report-composer-title" @click.self="closeReportComposer">
      <article class="notice-card report-composer">
        <div class="report-composer-header">
          <p class="eyebrow">LOCAL DIAGNOSTIC REPORT</p>
          <a class="report-manage-link" :href="myReportsURL" @click.prevent="openMyReports">MANAGE MY REPORTS ↗</a>
        </div>
        <h2 id="report-composer-title">WHAT HAPPENED?</h2>
        <p>Give the report a short title, then describe exactly what you were doing, what you expected, and what happened instead. More detail makes the captured logs easier to understand.</p>
        <form class="report-form" @submit.prevent="sendReport">
          <label for="report-title-input">TITLE</label>
          <input id="report-title-input" v-model="reportTitle" type="text" autocomplete="off" placeholder="Example: Revenant ability crashed the game" :disabled="isReportBusy">
          <label for="report-description-input">DESCRIPTION</label>
          <textarea id="report-description-input" v-model="reportDescription" placeholder="Describe everything that may be relevant. You can write as much as you need." :disabled="isReportBusy"></textarea>
          <div class="notice-actions">
            <button class="onboarding-cancel" type="button" :disabled="isReportBusy" @click="closeReportComposer">CANCEL</button>
            <button class="report-folder-button" type="submit" :disabled="!isReportReady">{{ isReportBusy ? 'PREPARING...' : 'CREATE REPORT' }}</button>
          </div>
        </form>
      </article>
    </section>

    <section v-if="profilePendingDelete" class="fullscreen-notice" role="dialog" aria-modal="true" aria-labelledby="delete-profile-title">
      <article class="notice-card danger-notice">
        <p class="eyebrow">IRREVERSIBLE ACTION</p>
        <h2 id="delete-profile-title">DELETE {{ profilePendingDelete.displayName }}?</h2>
        <p>This permanently removes the Crogenitor and all locally saved progress. This action cannot be undone.</p>
        <div class="notice-actions">
          <button class="onboarding-cancel" :disabled="isProfileDeletionBusy" @click="profilePendingDelete = null">KEEP CROGENITOR</button>
          <button class="danger-confirm" :disabled="isProfileDeletionBusy" @click="deleteProfile">{{ isProfileDeletionBusy ? 'DELETING...' : 'DELETE PERMANENTLY' }}</button>
        </div>
      </article>
    </section>

    <section v-if="remoteProfilePendingDelete" class="fullscreen-notice" role="dialog" aria-modal="true" aria-labelledby="remote-delete-profile-title" @click.self="cancelRemoteProfileDelete">
      <article class="notice-card danger-notice remote-delete-notice">
        <p class="eyebrow">REMOTE IRREVERSIBLE ACTION</p>
        <h2 id="remote-delete-profile-title">DELETE {{ remoteProfilePendingDelete.displayName }}?</h2>
        <p>This permanently removes the Crogenitor and all progress from <strong>{{ remoteProfilePendingDelete.serverAddress }}</strong>, then removes its cached connection from this launcher.</p>
        <div v-if="!remoteProfilePendingDelete.isPasswordRemembered" class="remote-delete-password">
          <label for="remote-delete-password">PASSWORD</label>
          <input id="remote-delete-password" v-model="remotePassword" type="password" autocomplete="current-password" placeholder="ENTER PASSWORD" :disabled="isRemoteDeletionBusy" @keyup.enter="deleteRemoteProfile">
        </div>
        <p v-if="remoteDeletionError" class="remote-delete-error">{{ remoteDeletionError }}</p>
        <div class="notice-actions">
          <button class="onboarding-cancel" type="button" :disabled="isRemoteDeletionBusy" @click="cancelRemoteProfileDelete">KEEP CROGENITOR</button>
          <button class="danger-confirm" type="button" :disabled="isRemoteDeletionBusy || (!remoteProfilePendingDelete.isPasswordRemembered && !remotePassword)" @click="deleteRemoteProfile">{{ isRemoteDeletionBusy ? 'DELETING...' : 'DELETE REMOTELY' }}</button>
        </div>
      </article>
    </section>

    <section v-if="isChangelogOpen" class="fullscreen-notice" role="dialog" aria-modal="true" aria-labelledby="changelog-title" @click.self="isChangelogOpen = false">
      <article class="notice-card changelog-notice">
        <p class="eyebrow">RELEASE HISTORY</p>
        <h2 id="changelog-title">DARK SPIN {{ status.version }}</h2>
        <p v-if="isChangelogLoading" class="changelog-state">LOADING CHANGELOG...</p>
        <p v-else-if="changelogError" class="changelog-state changelog-error">{{ changelogError }}</p>
        <pre v-else>{{ changelogText }}</pre>
        <div class="notice-actions">
          <button class="onboarding-cancel" type="button" @click="isChangelogOpen = false">CLOSE</button>
        </div>
      </article>
    </section>

    <section v-if="reportResult" class="fullscreen-notice" role="dialog" aria-modal="true" aria-labelledby="report-title" @click.self="reportResult = null">
      <article class="notice-card report-notice">
        <p class="eyebrow">DIAGNOSTIC ARCHIVE READY</p>
        <h2 id="report-title">REPORT CREATED</h2>
        <p><strong>{{ reportResult.name }}</strong> contains all available Darkspinner and protocol logs. Nothing was uploaded automatically; review the ZIP before sharing it.</p>
        <button class="report-path" type="button" title="Open report folder" @click="openReportFolder">{{ reportResult.directory }}</button>
        <p class="report-count">{{ reportResult.fileCount }} COMPLETE LOG FILES INCLUDED</p>
        <p>Sign in to GitHub to create an issue with your report details. Then open the bug folder and drag this ZIP into the issue before submitting. ZIP attachments can be up to 25 MB.</p>
        <p v-if="reportIssueDraft.isLong">Your description is too long for a browser link. Create GitHub issue will copy the full report for you to paste into the issue.</p>
        <p v-if="reportShareMessage" class="report-share-message" role="status">{{ reportShareMessage }}</p>
        <div class="notice-actions">
          <button class="onboarding-cancel" type="button" @click="reportResult = null">CLOSE</button>
          <button class="report-folder-button" type="button" @click="openReportIssue">1. CREATE GITHUB ISSUE</button>
          <button class="report-folder-button" type="button" @click="openReportFolder">2. OPEN BUG FOLDER</button>
        </div>
      </article>
    </section>

    <section v-if="isUninstallConfirmOpen" class="fullscreen-notice" role="dialog" aria-modal="true" aria-labelledby="uninstall-title">
      <article class="notice-card danger-notice">
        <p class="eyebrow">IRREVERSIBLE ACTION</p>
        <h2 id="uninstall-title">UNINSTALL DARKSPINNER?</h2>
        <p>This removes Darkspinner, all local Crogenitors and runtime data, its shortcuts, and its owned Steam launch integration. The original game installation remains intact.</p>
        <div class="notice-actions">
          <button class="onboarding-cancel" :disabled="isManagementBusy" @click="isUninstallConfirmOpen = false">CANCEL</button>
          <button class="danger-confirm" :disabled="isManagementBusy" @click="uninstallDarkspinner">{{ isManagementBusy ? 'UNINSTALLING...' : 'UNINSTALL PERMANENTLY' }}</button>
        </div>
      </article>
    </section>

  </main>
</template>
