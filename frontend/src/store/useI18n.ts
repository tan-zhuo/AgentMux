import { create } from 'zustand'
import { tree } from '../lib/api'
import {
  SYSTEM_TZ,
  detectLang,
  makeFmt,
  makeT,
  type Fmt,
  type Lang,
  type Params,
  type MsgKey,
  type TFunc,
} from '../lib/i18n'

const LANG_KEY = 'language'
const TZ_KEY = 'timezone'

interface I18nState {
  lang: Lang
  /** Empty means "follow this computer" — the resolved zone lives on `fmt`. */
  tz: string
  t: TFunc
  fmt: Fmt
  init: () => Promise<void>
  setLang: (lang: Lang) => void
  setTz: (tz: string) => void
}

const initialLang = detectLang()

export const useI18n = create<I18nState>((set, get) => ({
  lang: initialLang,
  tz: SYSTEM_TZ,
  t: makeT(initialLang),
  fmt: makeFmt(initialLang, SYSTEM_TZ),

  async init() {
    // No stored choice means the OS language, which is already applied — the
    // app is usable in the right language before the store has answered.
    let lang = get().lang
    let tz = SYSTEM_TZ
    try {
      const stored = await tree.getSetting(LANG_KEY, '')
      if (stored === 'en' || stored === 'ja' || stored === 'zh' || stored === 'zh-Hant') lang = stored
      tz = await tree.getSetting(TZ_KEY, SYSTEM_TZ)
    } catch {
      /* first run, or the store is unavailable — the defaults are fine */
    }
    document.documentElement.lang = lang
    set({ lang, tz, t: makeT(lang), fmt: makeFmt(lang, tz) })
  },

  setLang(lang) {
    document.documentElement.lang = lang
    set({ lang, t: makeT(lang), fmt: makeFmt(lang, get().tz) })
    void tree.setSetting(LANG_KEY, lang).catch(() => {})
  },

  setTz(tz) {
    set({ tz, fmt: makeFmt(get().lang, tz) })
    void tree.setSetting(TZ_KEY, tz).catch(() => {})
  },
}))

/** Subscribes a component to the language, so it re-renders when it changes. */
export const useT = () => useI18n((s) => s.t)

/** Subscribes a component to the language and zone used for timestamps. */
export const useFmt = () => useI18n((s) => s.fmt)

/**
 * The same lookup for code that is not a component — stores, event handlers,
 * anything that raises a toast from outside the render tree.
 */
export const t = (key: MsgKey, params?: Params) => useI18n.getState().t(key, params)

/** Formatters for the same callers. */
export const fmt = () => useI18n.getState().fmt
