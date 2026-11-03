import { translate } from './localization'

export function formatControlTime(timestamp, isDaily = false) {
  if (!timestamp || (typeof timestamp === 'string' && timestamp.startsWith('0001-'))) return translate('UNKNOWN')
  const instant = new Date(timestamp)
  if (!Number.isFinite(instant.getTime())) return translate('UNKNOWN')
  const pad = part => String(part).padStart(2, '0')
  const day = `${String(instant.getFullYear()).padStart(4, '0')}-${pad(instant.getMonth() + 1)}-${pad(instant.getDate())}`
  if (isDaily) return day
  return `${day} ${pad(instant.getHours())}:${pad(instant.getMinutes())}:${pad(instant.getSeconds())}`
}
