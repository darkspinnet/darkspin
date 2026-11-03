import * as desktop from '../wailsjs/go/main/App'
import * as wailsRuntime from '../wailsjs/runtime/runtime'

const tokenKey = 'darkspinner.browserToken'
const fragment = new URLSearchParams(window.location.hash.slice(1))
if (fragment.has('token')) {
  sessionStorage.setItem(tokenKey, fragment.get('token'))
  history.replaceState(null, '', `${window.location.pathname}${window.location.search}`)
}

function isWails() {
  return !!window.go?.main?.App
}

async function call(method, ...args) {
  if (isWails()) return desktop[method](...args)
  const token = sessionStorage.getItem(tokenKey) || ''
  const response = await fetch(`./api/call/${method}`, {
    method:'POST',
    headers:{ 'Content-Type':'application/json', 'X-Darkspinner-Token':token },
    body:JSON.stringify({ arguments:args }),
  })
  if (!response.ok) throw new Error(`Darkspinner returned ${response.status}`)
  const payload = await response.json()
  if (payload.error) throw new Error(payload.error)
  return payload.result
}

export const Authorize = identity => call('Authorize', identity)
export const Cancel = () => call('Cancel')
export const CancelPatch = () => call('CancelPatch')
export const Check = () => call('Check')
export const CheckRemoteServers = addresses => call('CheckRemoteServers', addresses)
export const CloseDetachedGameInstances = () => call('CloseDetachedGameInstances')
export const CloseRunningGame = () => call('CloseRunningGame')
export const CloseRunningProfile = identity => call('CloseRunningProfile', identity)
export const CreateProfile = (identity, avatarID, isTutorialSkipped) => call('CreateProfile', identity, avatarID, isTutorialSkipped)
export const DeleteProfile = identity => call('DeleteProfile', identity)
export const DeleteModProfile = identity => isWails()
  ? window.go.main.App.DeleteModProfile(identity)
  : call('DeleteModProfile', identity)
export const DeleteRemoteConnection = (serverAddress, identity) => call('DeleteRemoteConnection', serverAddress, identity)
export const DeleteRemoteProfile = (serverAddress, identity, password) => call('DeleteRemoteProfile', serverAddress, identity, password)
export const ResetProfileDisplay = (identity, serverAddress = '') => call('ResetProfileDisplay', identity, serverAddress)
export const ResetModProfileDisplay = identity => isWails()
  ? window.go.main.App.ResetModProfileDisplay(identity)
  : call('ResetModProfileDisplay', identity)
export const DiscardInterruptedMission = identity => call('DiscardInterruptedMission', identity)
export const GetInstallationStatus = () => call('GetInstallationStatus')
export const GetInterruptedMission = identity => call('GetInterruptedMission', identity)
export const GetLauncherIntegrationStatus = () => call('GetLauncherIntegrationStatus')
export const GetProfileAvatars = () => call('GetProfileAvatars')
export const GetProfiles = () => call('GetProfiles')
export const GetModConfiguration = () => call('GetModConfiguration')
export const GetModPortrait = id => call('GetModPortrait', id)
export const SetModSporePath = path => call('SetModSporePath', path)
export const DetectModSporePath = () => call('DetectModSporePath')
export const BrowseModSporeDirectory = path => call('BrowseModSporeDirectory', path)
export const SaveModProfile = request => call('SaveModProfile', request)
export const LaunchModProfile = identity => call('LaunchModProfile', identity)
export const GetRemoteProfiles = () => call('GetRemoteProfiles')
// Preserve selection/sign-out order across component unmounts and HTTP RPC arrival.
let controlPanelQueue = Promise.resolve()
function controlPanelCall(method, ...args) {
  const operation = controlPanelQueue.then(() => isWails() ? window.go.main.App[method](...args) : call(method, ...args))
  controlPanelQueue = operation.then(() => {}, () => {})
  return operation
}
export const OpenRemoteControlPanel = (...args) => controlPanelCall('OpenRemoteControlPanel', ...args)
export const OpenLocalControlPanel = (...args) => controlPanelCall('OpenLocalControlPanel', ...args)
export const GetRemoteControlPanel = (...args) => controlPanelCall('GetRemoteControlPanel', ...args)
export const GetRemoteControlActivity = (...args) => controlPanelCall('GetRemoteControlActivity', ...args)
export const GetMyControlActivity = (...args) => controlPanelCall('GetMyControlActivity', ...args)
export const GetUserControlActivity = (...args) => controlPanelCall('GetUserControlActivity', ...args)
export const GetUserControlLoginHistory = (...args) => controlPanelCall('GetUserControlLoginHistory', ...args)
export const CloseRemoteControlPanel = () => controlPanelCall('CloseRemoteControlPanel')
export const ListRemoteControlUsers = (...args) => controlPanelCall('ListRemoteControlUsers', ...args)
export const ListRemoteControlDiagnostics = (...args) => controlPanelCall('ListRemoteControlDiagnostics', ...args)
export const DownloadRemoteControlDiagnostic = (...args) => controlPanelCall('DownloadRemoteControlDiagnostic', ...args)
export const DeleteRemoteControlDiagnostic = (...args) => controlPanelCall('DeleteRemoteControlDiagnostic', ...args)
export const ListRemoteControlHistory = (...args) => controlPanelCall('ListRemoteControlHistory', ...args)
export const FilterRemoteControlHistory = (...args) => controlPanelCall('FilterRemoteControlHistory', ...args)
export const SetRemoteControlUserAccess = (...args) => controlPanelCall('SetRemoteControlUserAccess', ...args)
export const ModerateRemoteControlUser = (...args) => controlPanelCall('ModerateRemoteControlUser', ...args)
export const TimeoutRemoteControlUser = (...args) => controlPanelCall('TimeoutRemoteControlUser', ...args)
export const RenameRemoteControlAccount = (...args) => controlPanelCall('RenameRemoteControlAccount', ...args)
export const ChangeRemoteControlPassword = (...args) => controlPanelCall('ChangeRemoteControlPassword', ...args)
export const SetRemoteControlPassword = (...args) => controlPanelCall('SetRemoteControlPassword', ...args)
export const DeleteRemoteControlAccount = (...args) => controlPanelCall('DeleteRemoteControlAccount', ...args)
export const GetRemoteControlIdentity = (...args) => controlPanelCall('GetRemoteControlIdentity', ...args)
export const SetRemoteControlIdentity = (...args) => controlPanelCall('SetRemoteControlIdentity', ...args)
export const GetRemoteControlAnnouncements = (...args) => controlPanelCall('GetRemoteControlAnnouncements', ...args)
export const SendRemoteControlAnnouncement = (...args) => controlPanelCall('SendRemoteControlAnnouncement', ...args)
export const GetRemoteControlHealth = (...args) => controlPanelCall('GetRemoteControlHealth', ...args)
export const GetRemoteControlUpdate = (...args) => controlPanelCall('GetRemoteControlUpdate', ...args)
export const CheckRemoteControlUpdate = (...args) => controlPanelCall('CheckRemoteControlUpdate', ...args)
export const ListRemoteControlSessions = (...args) => controlPanelCall('ListRemoteControlSessions', ...args)
export const RevokeRemoteControlSession = (...args) => controlPanelCall('RevokeRemoteControlSession', ...args)
export const RevokeOtherRemoteControlSessions = (...args) => controlPanelCall('RevokeOtherRemoteControlSessions', ...args)
export const GetRemoteControlMaintenance = (...args) => controlPanelCall('GetRemoteControlMaintenance', ...args)
export const StartRemoteControlMaintenance = (...args) => controlPanelCall('StartRemoteControlMaintenance', ...args)
export const CancelRemoteControlMaintenance = (...args) => controlPanelCall('CancelRemoteControlMaintenance', ...args)
export const GetServerConfiguration = () => call('GetServerConfiguration')
export const GetServerRulesConfiguration = () => call('GetServerRulesConfiguration')
export const SetServerRulesConfiguration = request => call('SetServerRulesConfiguration', request)
export const GetStatus = () => call('GetStatus')
export const GetLauncherAutomationConfiguration = () => isWails()
  ? window.go.main.App.GetLauncherAutomationConfiguration()
  : call('GetLauncherAutomationConfiguration')
export const SetAutoPatch = isEnabled => isWails()
  ? window.go.main.App.SetAutoPatch(isEnabled)
  : call('SetAutoPatch', isEnabled)
export const SetAutoPlay = isEnabled => isWails()
  ? window.go.main.App.SetAutoPlay(isEnabled)
  : call('SetAutoPlay', isEnabled)
export const GetLauncherLanguageConfiguration = () => isWails()
  ? window.go.main.App.GetLauncherLanguageConfiguration()
  : call('GetLauncherLanguageConfiguration')
export const SetLauncherLocale = locale => isWails()
  ? window.go.main.App.SetLauncherLocale(locale)
  : call('SetLauncherLocale', locale)
export const SetLocalServicesRequired = isRequired => isWails()
  ? window.go.main.App.SetLocalServicesRequired(isRequired)
  : call('SetLocalServicesRequired', isRequired)
export const GetSquadSnapshot = profileLoginNames => call('GetSquadSnapshot', profileLoginNames)
export const GetLuaConsole = () => call('GetLuaConsole')
export const RunLuaScript = path => call('RunLuaScript', path)
export const KillLuaSession = () => call('KillLuaSession')
export const StopLuaScript = () => call('StopLuaScript')
export const GetWineRunnerConfiguration = () => call('GetWineRunnerConfiguration')
export const HasDetachedGameInstances = () => call('HasDetachedGameInstances')
export const IsProfileRunning = identity => call('IsProfileRunning', identity)
export const LaunchRemoteProfile = (...args) => call('LaunchRemoteProfile', ...args)
export const LoginRemoteProfile = (...args) => call('LoginRemoteProfile', ...args)
export const OpenFirewallSettings = () => call('OpenFirewallSettings')
export const OpenReportFolder = () => call('OpenReportFolder')
export const OpenSteamDemoInstall = () => call('OpenSteamDemoInstall')
export const Patch = () => call('Patch')
export const Play = () => call('Play')
export const RefreshInstallationStatus = () => call('RefreshInstallationStatus')
export const RefreshRemoteProfiles = () => call('RefreshRemoteProfiles')
export const RegisterRemoteProfile = (...args) => call('RegisterRemoteProfile', ...args)
export const RelocateToGameRoot = request => call('RelocateToGameRoot', request)
export const RemoveLauncherIntegration = kind => call('RemoveLauncherIntegration', kind)
export const RepairLauncherIntegration = kind => call('RepairLauncherIntegration', kind)
export const RestartLauncher = () => call('RestartLauncher')
export const ScanRemoteServers = address => call('ScanRemoteServers', address)
export const ScanWineRunners = () => call('ScanWineRunners')
export const SendReport = title => call('SendReport', title)
export const CompactSnapshots = () => call('CompactSnapshots')
export const SetIdentity = identity => call('SetIdentity', identity)
export const SetServerPort = port => call('SetServerPort', port)
export const SetSkipCinematic = isSkipped => call('SetSkipCinematic', isSkipped)
export const SetSkipIntro = isSkipped => call('SetSkipIntro', isSkipped)
export const SetWineRunner = path => call('SetWineRunner', path)
export async function SetServerConfiguration(...args) {
  const configuration = await call('SetServerSetup', ...args)
  if (!isWails() && configuration?.port && String(configuration.port) !== window.location.port) {
    const address = new URL(window.location.href)
    address.port = String(configuration.port)
    address.pathname = '/launcher/'
    address.hash = `token=${sessionStorage.getItem(tokenKey) || ''}`
    window.location.assign(address)
  }
  return configuration
}
export const StartDetachedGameInstance = identity => call('StartDetachedGameInstance', identity)
export const SignOut = () => call('SignOut')
export const UninstallDarkspinner = () => call('UninstallDarkspinner')

export function BrowserOpenURL(address) {
  if (isWails()) return wailsRuntime.BrowserOpenURL(address)
  window.open(address, '_blank', 'noopener,noreferrer')
}

export async function ClipboardSetText(contents) {
  if (isWails()) return wailsRuntime.ClipboardSetText(contents)
  return navigator.clipboard.writeText(contents)
}

export function EventsOn(eventName, callback) {
  if (isWails()) return wailsRuntime.EventsOn(eventName, callback)
  if (eventName !== 'darkspinner:status') return () => {}
  let previousStatus = ''
  let isRefreshing = false
  const refresh = async () => {
    if (isRefreshing) return
    isRefreshing = true
    try {
      const status = await GetStatus()
      const serializedStatus = JSON.stringify(status)
      if (serializedStatus !== previousStatus) {
        previousStatus = serializedStatus
        callback(status)
      }
    } catch {
      // The shared listener briefly changes owners while game services start.
    } finally {
      isRefreshing = false
    }
  }
  void refresh()
  const timer = window.setInterval(() => void refresh(), 250)
  return () => {
    window.clearInterval(timer)
  }
}

export async function Quit() {
  if (isWails()) return wailsRuntime.Quit()
  await call('Quit')
  window.close()
}
