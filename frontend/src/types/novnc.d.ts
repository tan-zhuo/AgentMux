/**
 * The slice of noVNC this app uses.
 *
 * noVNC ships no types of its own, and a blanket `any` for a module that draws
 * somebody's screen and takes their keystrokes is worth avoiding: these are the
 * members DesktopPane touches, taken from the library's API document.
 */
declare module '@novnc/novnc' {
  interface RFBOptions {
    shared?: boolean
    credentials?: { username?: string; password?: string; target?: string }
    repeaterID?: string
    wsProtocols?: string[]
  }

  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, url: string | WebSocket, options?: RFBOptions)
    /** Scale the remote screen to the element rather than clipping it. */
    scaleViewport: boolean
    /** Let a screen larger than the element be panned instead of shrunk. */
    clipViewport: boolean
    /** CSS behind the remote screen. */
    background: string
    viewOnly: boolean
    focusOnClick: boolean
    disconnect(): void
    sendCredentials(credentials: { username?: string; password?: string; target?: string }): void
    sendCtrlAltDel(): void
    focus(): void
    blur(): void
  }
}
