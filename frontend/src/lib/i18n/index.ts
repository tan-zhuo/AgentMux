import { en, type MsgKey, type Messages } from './en'
import { ja } from './ja'
import { zh } from './zh'
import { zhHant } from './zh-Hant'

export type { MsgKey, Messages }

export type Lang = 'en' | 'ja' | 'zh' | 'zh-Hant'

/** Offered in the language's own words — nobody looks for "Japanese". */
export const LANGUAGES: { id: Lang; native: string; english: string }[] = [
  { id: 'en', native: 'English', english: 'English' },
  { id: 'ja', native: '日本語', english: 'Japanese' },
  { id: 'zh', native: '简体中文', english: 'Chinese (Simplified)' },
  { id: 'zh-Hant', native: '繁體中文', english: 'Chinese (Traditional)' },
]

const CATALOGS: Record<Lang, Messages> = { en, ja, zh, 'zh-Hant': zhHant }

/** The locale used for dates, numbers and collation in each language. */
export const LOCALES: Record<Lang, string> = {
  en: 'en-US',
  ja: 'ja-JP',
  zh: 'zh-CN',
  'zh-Hant': 'zh-TW',
}

export type Params = Record<string, string | number>
export type TFunc = (key: MsgKey, params?: Params) => string

/**
 * Looks a message up and fills its `{placeholders}`.
 *
 * A missing translation falls through to English rather than showing the key:
 * a catalog can lag behind the code, and an English label is still usable where
 * `agent.kill.confirm` is not.
 */
export function makeT(lang: Lang): TFunc {
  const catalog = CATALOGS[lang] ?? en
  return (key, params) => {
    const raw = catalog[key] ?? en[key] ?? (key as string)
    if (!params) return raw
    return raw.replace(/\{(\w+)\}/g, (whole, name: string) =>
      name in params ? String(params[name]) : whole,
    )
  }
}

/**
 * Splits a message around one placeholder, for the cases where the value needs
 * its own markup — a monospaced host name inside a sentence, say. Call `t`
 * without params so the placeholder survives to be split on.
 */
export function tAround(raw: string, name: string): [string, string] {
  const token = `{${name}}`
  const at = raw.indexOf(token)
  if (at < 0) return [raw, '']
  return [raw.slice(0, at), raw.slice(at + token.length)]
}

/**
 * The same idea for a sentence with more than one such value: the message comes
 * back as alternating text and `{placeholder}` names, which a component renders
 * with whatever markup each value needs.
 */
export function tParts(raw: string): Array<{ text: string; slot: boolean }> {
  return raw
    .split(/\{(\w+)\}/g)
    .map((piece, i) => ({ text: piece, slot: i % 2 === 1 }))
    .filter((p) => p.text !== '')
}

/** The best match for the OS language, used until the user chooses one. */
export function detectLang(): Lang {
  for (const tag of navigator.languages ?? [navigator.language]) {
    const base = (tag ?? '').toLowerCase()
    if (base.startsWith('ja')) return 'ja'
    if (base.startsWith('zh')) {
      // Traditional-script regions and explicit -hant tags; the mainland and
      // Singapore fall through to the simplified catalog.
      const hant = base.includes('hant') || ['zh-tw', 'zh-hk', 'zh-mo'].some((p) => base.startsWith(p))
      return hant ? 'zh-Hant' : 'zh'
    }
    if (base.startsWith('en')) return 'en'
  }
  return 'en'
}

/** The empty string means "whatever this computer is set to". */
export const SYSTEM_TZ = ''

export function systemTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

/**
 * Every zone the runtime knows, when it will tell us. WebKitGTK is the one that
 * will not, so a short list of the zones this app is actually used from stands
 * in — plus whatever this computer is set to, which is never missing from it.
 */
export function timeZones(): string[] {
  const supported = (Intl as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf
  if (supported) {
    try {
      const list = supported('timeZone')
      if (list.length) return list
    } catch {
      /* fall through to the short list */
    }
  }
  const fallback = [
    'UTC',
    'Asia/Shanghai',
    'Asia/Hong_Kong',
    'Asia/Taipei',
    'Asia/Tokyo',
    'Asia/Seoul',
    'Asia/Singapore',
    'Asia/Kolkata',
    'Asia/Dubai',
    'Europe/London',
    'Europe/Paris',
    'Europe/Berlin',
    'Europe/Moscow',
    'America/New_York',
    'America/Chicago',
    'America/Denver',
    'America/Los_Angeles',
    'America/Sao_Paulo',
    'Australia/Sydney',
    'Pacific/Auckland',
  ]
  const here = systemTimeZone()
  return fallback.includes(here) ? fallback : [here, ...fallback].sort()
}

export interface Fmt {
  /** Date and time, e.g. for a memory's creation stamp. */
  dateTime: (epochSeconds: number) => string
  /** Clock time only, for things that happened today. */
  time: (epochSeconds: number) => string
  /** Calendar date only. */
  date: (epochSeconds: number) => string
  /** Numbers, grouped the way the language groups them. */
  number: (value: number) => string
  /** The zone these are rendered in, resolved — never the empty string. */
  zone: string
  /** e.g. `UTC+09:00`, for showing next to the zone name. */
  offsetLabel: (tz?: string) => string
}

/**
 * Formatters bound to one language and zone. Built once per change rather than
 * per render: `Intl.DateTimeFormat` is expensive enough to matter in a list.
 */
export function makeFmt(lang: Lang, tz: string): Fmt {
  const locale = LOCALES[lang] ?? 'en-US'
  const zone = tz || systemTimeZone()
  const opts = (o: Intl.DateTimeFormatOptions) => ({ ...o, timeZone: zone })
  const safe = (o: Intl.DateTimeFormatOptions) => {
    try {
      return new Intl.DateTimeFormat(locale, opts(o))
    } catch {
      // An unknown zone (a stale setting, a renamed zone) must not take the
      // whole view down with it.
      return new Intl.DateTimeFormat(locale, o)
    }
  }
  const dateTime = safe({ dateStyle: 'medium', timeStyle: 'medium' })
  const time = safe({ timeStyle: 'medium' })
  const date = safe({ dateStyle: 'medium' })
  const number = new Intl.NumberFormat(locale)
  return {
    dateTime: (s) => dateTime.format(s * 1000),
    time: (s) => time.format(s * 1000),
    date: (s) => date.format(s * 1000),
    number: (v) => number.format(v),
    zone,
    offsetLabel: (which) => offsetLabel(which || zone),
  }
}

/** `UTC+08:00` for a zone, read out of the runtime rather than tabulated. */
export function offsetLabel(tz: string): string {
  try {
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone: tz,
      timeZoneName: 'longOffset',
    }).formatToParts(new Date())
    const name = parts.find((p) => p.type === 'timeZoneName')?.value ?? ''
    return name === 'GMT' ? 'UTC+00:00' : name.replace('GMT', 'UTC')
  } catch {
    return ''
  }
}
