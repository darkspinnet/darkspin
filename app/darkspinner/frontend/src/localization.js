import { ref } from 'vue'
import english from '../../../locale/en-us.json'
import german from '../../../locale/de-de.json'
import french from '../../../locale/fr-fr.json'
import polish from '../../../locale/pl-pl.json'
import russian from '../../../locale/ru-ru.json'

const catalogs = [english, german, french, polish, russian]
const catalogsByLocale = Object.fromEntries(catalogs.map(catalog => [catalog.locale, catalog.messages]))
const launcherLocale = ref('en-us')

function escapePattern(text) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// Backend progress and existing computed labels arrive as formatted English.
// Match only cataloged message shapes, without modifying the underlying state.
function messagePatterns(messages) {
  const patterns = []
  for (const [source, translation] of Object.entries(messages)) {
    if (!translation || !/\{\d+\}/.test(source)) continue
    const slots = []
    let pattern = ''
    let offset = 0
    for (const match of source.matchAll(/\{(\d+)\}/g)) {
      pattern += escapePattern(source.slice(offset, match.index)) + '(.*?)'
      slots.push(match[1])
      offset = match.index + match[0].length
    }
    pattern += escapePattern(source.slice(offset))
    patterns.push({ pattern:new RegExp(`^${pattern}$`, 's'), slots, translation, weight:source.replace(/\{\d+\}/g, '').length })
  }
  return patterns.sort((left, right) => right.weight - left.weight)
}

const patternsByLocale = Object.fromEntries(catalogs.map(catalog => [catalog.locale, messagePatterns(catalog.messages)]))

export function setLauncherLocale(locale) {
  const requested = String(locale || '').toLowerCase().replaceAll('_', '-')
  launcherLocale.value = catalogsByLocale[requested] ? requested
    : catalogs.find(catalog => catalog.locale.split('-')[0] === requested.split('-')[0])?.locale || 'en-us'
  document.documentElement.lang = launcherLocale.value
}

function interpolate(text, parameters) {
  return text.replace(/\{(\d+)\}/g, (slot, index) => parameters[index] ?? slot)
}

export function translate(source, parameters) {
  if (typeof source !== 'string' || !source) return source
  const locale = launcherLocale.value
  const messages = catalogsByLocale[locale]
  const key = source.trim().replace(/\s+/g, ' ')
  const translation = messages[key]
  if (parameters) return interpolate(translation || english.messages[key] || source, parameters)
  let translated = translation
  if (!translated && locale !== 'en-us') {
    for (const entry of patternsByLocale[locale]) {
      const matches = entry.pattern.exec(source.trim())
      if (!matches) continue
      const captures = Object.fromEntries(entry.slots.map((slot, index) => [slot, matches[index + 1]]))
      translated = interpolate(entry.translation, captures)
      break
    }
  }
  if (!translated) return source
  const leading = source.match(/^\s*/)[0]
  const trailing = source.match(/\s*$/)[0]
  return leading + translated + trailing
}

// The saved game locale replaces this early-startup preference when loaded.
setLauncherLocale(navigator.language)
