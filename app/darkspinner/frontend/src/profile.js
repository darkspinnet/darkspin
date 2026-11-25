const campaignPlanets = [
  "Zelem's Nexus", "Zelem's Nexus", 'Nocturna', 'Nocturna',
  'Verdanth', 'Verdanth', "Zelem's Nexus", "Zelem's Nexus",
  'Cryos', 'Cryos', 'Verdanth', 'Verdanth',
  'Infinity', 'Infinity', 'Cryos', 'Cryos',
  'Nocturna', 'Nocturna', 'Infinity', 'Infinity',
  'Scaldron', 'Scaldron', 'Scaldron', 'Scaldron',
  'Cryos', 'Cryos', "Zelem's Nexus", "Zelem's Nexus",
  'Infinity', 'Infinity', 'Verdanth', 'Verdanth',
  'Scaldron', 'Scaldron', 'Cryos', 'Cryos',
  'Nocturna', 'Nocturna', 'Infinity', 'Infinity',
  "Zelem's Nexus", "Zelem's Nexus", 'Nocturna', 'Nocturna',
  'Verdanth', 'Verdanth', 'Scaldron', 'Scaldron',
  'Nocturna', 'Infinity', 'Cryos', 'Verdanth',
  'Scaldron', "Zelem's Nexus", 'Nocturna', 'Infinity',
  "Zelem's Nexus", 'Verdanth', 'Scaldron', 'Cryos',
  'Verdanth', 'Cryos', "Zelem's Nexus", 'Nocturna',
  'Infinity', 'Scaldron', 'Verdanth', "Zelem's Nexus",
  'Cryos', 'Nocturna', 'Infinity', 'Scaldron',
]

export function campaignLevelFor(profile) {
  if (!profile?.isTutorialCompleted) return 0
  return Math.min(campaignPlanets.length, Math.max(1, profile.highestCampaignUnlocked || 1))
}

export function profileCreateDate(profile) {
  if (!profile?.createDt) return ''
  const createDT = new Date(profile.createDt)
  if (Number.isNaN(createDT.getTime())) return ''
  return createDT.toISOString().slice(0, 10)
}

export function profileLastPlayedDate(profile) {
  const connectionDT = profile?.lastConnectionDt || profile?.lastConnectedDt
  if (!connectionDT) return 'NEVER'
  const lastPlayedDT = new Date(connectionDT)
  if (Number.isNaN(lastPlayedDT.getTime())) return 'NEVER'
  return lastPlayedDT.toISOString().slice(0, 10)
}

export function profileLastPlayedTime(profile) {
  const timestamp = Date.parse(profile?.lastConnectionDt || profile?.lastConnectedDt || '')
  return Number.isFinite(timestamp) ? Math.max(0, timestamp) : 0
}

export function campaignLabelFor(chainLevel) {
  if (!chainLevel) return 'TUTORIAL'
  const level = Math.min(campaignPlanets.length, chainLevel)
  const planet = Math.floor((level - 1) / 4) + 1
  const mission = ((level - 1) % 4) + 1
  return `${planet}-${mission}`
}

export function campaignDetailFor(chainLevel) {
  return chainLevel ? campaignPlanets[Math.min(campaignPlanets.length, chainLevel) - 1] : 'CRYOS TRAINING'
}
