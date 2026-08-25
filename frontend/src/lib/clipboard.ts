import { Clipboard } from '@wailsio/runtime'
import { isDesktop } from './webTransport'

/**
 * The clipboard, wherever this frontend is running.
 *
 * Wails' Clipboard talks to the native window over the webview bridge, which
 * exists in the desktop app and nowhere else — in a served browser or the
 * Android shell every copy was posting a message into the void, which is why
 * "copy" appeared to do nothing on a phone. The browser has two clipboards of
 * its own instead: the async API, and — on a plain-http LAN address, where the
 * async one is not a secure context and simply is not there — the old
 * execCommand path, which still works as long as the call sits inside the
 * gesture that asked for it.
 */
export async function copyText(text: string): Promise<boolean> {
  if (!text) return false

  if (isDesktop) {
    try {
      await Clipboard.SetText(text)
      return true
    } catch {
      /* fall through to the browser's own clipboard */
    }
  }

  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    /* denied, or not a secure context */
  }

  return execCopy(text)
}

/**
 * The clipboard as text, or empty when this platform will not hand it over.
 *
 * Reading is the half browsers guard hardest: Android's WebView has no
 * clipboard-read permission to grant, so a paste button there returns nothing
 * and the caller says so. Pasting with the keyboard's own paste still works —
 * it types into the terminal like any other input.
 */
export async function readText(): Promise<string> {
  if (isDesktop) {
    try {
      return (await Clipboard.Text()) ?? ''
    } catch {
      /* fall through */
    }
  }
  try {
    if (navigator.clipboard?.readText) return await navigator.clipboard.readText()
  } catch {
    /* denied, or not a secure context */
  }
  return ''
}

/**
 * Copy through a throwaway textarea and execCommand.
 *
 * Deprecated for a decade and still the only thing that works on an insecure
 * origin. Focus is borrowed and given straight back, so the terminal does not
 * lose the caret to a copy.
 */
function execCopy(text: string): boolean {
  const active = document.activeElement as HTMLElement | null
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  // Off-screen would not be selectable on iOS; invisible and in place is.
  ta.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;opacity:0;padding:0;border:none'
  document.body.appendChild(ta)
  try {
    ta.focus()
    ta.select()
    ta.setSelectionRange(0, text.length)
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    ta.remove()
    active?.focus?.()
  }
}
