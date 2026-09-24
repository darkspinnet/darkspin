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
export const CloseDetachedGameInstances = () => call('CloseDetachedGameInstances')
export const CloseRunningGame = () => call('CloseRunningGame')
export const CloseRunningProfile = identity => call('CloseRunningProfile', identity)
export const CreateProfile = (identity, avatarID, isTutorialSkipped) => call('CreateProfile', identity, avatarID, isTutorialSkipped)
export const DeleteProfile = identity => call('DeleteProfile', identity)
export const DeleteRemoteProfile = (serverAddress, identity, password) => call('DeleteRemoteProfile', serverAddress, identity, password)
export const DiscardInterruptedMission = identity => call('DiscardInterruptedMission', identity)
export const GetInstallationStatus = () => call('GetInstallationStatus')
export const GetInterruptedMission = identity => call('GetInterruptedMission', identity)
export const GetLauncherIntegrationStatus = () => call('GetLauncherIntegrationStatus')
export const GetProfileAvatars = () => call('GetProfileAvatars')
export const GetProfiles = () => call('GetProfiles')
export const GetRemoteProfiles = () => call('GetRemoteProfiles')
export const GetServerConfiguration = () => call('GetServerConfiguration')
export const GetStatus = () => call('GetStatus')
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
export const SendReport = (title, description) => call('SendReport', title, description)
export const SetIdentity = identity => call('SetIdentity', identity)
export const SetServerPort = port => call('SetServerPort', port)
export const SetSkipCinematic = isSkipped => call('SetSkipCinematic', isSkipped)
export async function SetServerConfiguration(...args) {
  const configuration = await call('SetServerConfiguration', ...args)
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
