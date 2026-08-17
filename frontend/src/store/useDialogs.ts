import { create } from 'zustand'
import type { Agent, Project, Server, Skill, Workspace } from '../lib/types'

export type Dialog =
  | { kind: 'server'; server?: Server }
  | { kind: 'project'; project?: Project }
  | { kind: 'workspace'; workspace?: Workspace; projectId?: string }
  | { kind: 'agent'; agent?: Agent; workspaceId?: string; presetCommand?: string }
  | { kind: 'settings' }
  | { kind: 'skill'; skill?: Skill }
  | { kind: 'skillHistory'; skill: Skill }
  /** Asks what to put in a new terminal pane. */
  | { kind: 'split' }
  | null

interface DialogState {
  dialog: Dialog
  open: (d: NonNullable<Dialog>) => void
  close: () => void
}

export const useDialogs = create<DialogState>((set) => ({
  dialog: null,
  open: (dialog) => set({ dialog }),
  close: () => set({ dialog: null }),
}))
