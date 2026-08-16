import { create } from 'zustand'

export type ConfirmTone = 'danger' | 'warning' | 'info'

export interface ConfirmRequest {
  /** Short sentence naming the action, not a question. */
  title: string
  /** What will happen, in plain words. */
  message: string
  /** Consequences worth spelling out before someone commits. */
  points?: string[]
  /** Reassurance about what is *not* affected. */
  reassurance?: string
  confirmLabel?: string
  cancelLabel?: string
  tone?: ConfirmTone
  /**
   * When set, the confirm button stays disabled until the user types this
   * string. Reserved for the few actions that destroy remote state.
   */
  requireText?: string
}

interface ConfirmState {
  request: (ConfirmRequest & { id: number }) | null
  resolve: ((ok: boolean) => void) | null
  ask: (req: ConfirmRequest) => Promise<boolean>
  settle: (ok: boolean) => void
}

let seq = 0

export const useConfirm = create<ConfirmState>((set, get) => ({
  request: null,
  resolve: null,

  ask(req) {
    // A second prompt while one is open would strand the first promise, so the
    // pending one is answered "no" before the new question replaces it.
    const pending = get().resolve
    if (pending) pending(false)

    return new Promise<boolean>((resolve) => {
      set({ request: { ...req, id: ++seq }, resolve })
    })
  },

  settle(ok) {
    const resolve = get().resolve
    set({ request: null, resolve: null })
    resolve?.(ok)
  },
}))

/** Promise-based confirmation, so call sites read like the native one they replace. */
export function confirmAction(req: ConfirmRequest): Promise<boolean> {
  return useConfirm.getState().ask(req)
}
