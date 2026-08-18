import clsx from 'clsx'
import { RotateCcw, Save } from 'lucide-react'
import type * as Monaco from 'monaco-editor'
import { useCallback, useEffect, useRef, useState } from 'react'
import { errText, files as filesApi } from '../lib/api'
import type { FileContent } from '../lib/types'
import { useAppStore, type Tab } from '../store/useAppStore'
import { confirmAction } from '../store/useConfirm'
import { t as tr, useT } from '../store/useI18n'
import { useTheme } from '../store/useTheme'
import { Button, Empty } from './ui'

/** The lazily-loaded editor module. Monaco is four megabytes of JavaScript; it
 *  is fetched the first time a file is opened rather than at startup, so the
 *  app still comes up in a blink for people who never edit anything. */
type EditorModule = typeof import('../lib/monaco')

/**
 * A remote file open in the VS Code editor core.
 *
 * The file is read over the same SFTP connection the browser uses, edited
 * locally, and written back atomically. Saving checks the modification time
 * first: an agent working in the same directory is a normal thing here, and
 * silently overwriting its change would be the worst kind of data loss —
 * the invisible kind.
 */
export function EditorPane({ tab, active }: { tab: Tab; active: boolean }) {
  const hostRef = useRef<HTMLDivElement | null>(null)
  const editorRef = useRef<Monaco.editor.IStandaloneCodeEditor | null>(null)
  const modelRef = useRef<Monaco.editor.ITextModel | null>(null)
  /** The last version known to be on the server; the baseline for "dirty". */
  const savedRef = useRef<FileContent | null>(null)
  /** Monaco's version id at the last save. Comparing ids is how you ask "has
   *  this changed" in constant time — comparing the document text on every
   *  keystroke would mean copying the whole file out of the model to answer a
   *  yes/no question, which on a large file is felt as lag while typing. */
  const savedVersionRef = useRef(0)

  const theme = useTheme((s) => s.theme)
  const toast = useAppStore((s) => s.toast)
  const setTabState = useAppStore((s) => s.setTabState)
  const t = useT()

  const [mod, setMod] = useState<EditorModule | null>(null)
  const [file, setFile] = useState<FileContent | null>(null)
  const [error, setError] = useState('')
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [lines, setLines] = useState(0)

  const path = tab.command ?? ''
  const name = baseName(path)

  const save = useCallback(async () => {
    const model = modelRef.current
    const saved = savedRef.current
    if (!model || !saved) return
    setSaving(true)
    try {
      const written = await filesApi.write(
        tab.serverId,
        saved.path,
        model.getValue(),
        saved.modTime,
        saved.crlf,
      )
      savedRef.current = written
      savedVersionRef.current = model.getAlternativeVersionId()
      // The status line follows the file on disk, which just changed size.
      setFile((prev) =>
        prev ? { ...prev, size: written.size, modTime: written.modTime, mode: written.mode } : prev,
      )
      setDirty(false)
      toast('ok', t('editor.saved', { name: baseName(saved.path) }))
    } catch (e) {
      const msg = errText(e)
      // A conflict is not a failure to write, it is a decision to make.
      if (!msg.includes('changed on the server')) {
        toast('error', msg)
        return
      }
      const overwrite = await confirmAction({
        title: t('editor.conflict.title', { name: baseName(saved.path) }),
        message: t('editor.conflict.message'),
        points: [t('editor.conflict.save'), t('editor.conflict.reload')],
        tone: 'warning',
        confirmLabel: t('editor.conflict.overwrite'),
        cancelLabel: t('editor.conflict.keepEditing'),
      })
      if (!overwrite) return
      try {
        // modTime 0 skips the check on the way back down.
        const forced = await filesApi.write(tab.serverId, saved.path, model.getValue(), 0, saved.crlf)
        savedRef.current = forced
        savedVersionRef.current = model.getAlternativeVersionId()
        setFile((prev) =>
          prev ? { ...prev, size: forced.size, modTime: forced.modTime, mode: forced.mode } : prev,
        )
        setDirty(false)
        toast('warn', t('editor.overwrote', { name: baseName(saved.path) }))
      } catch (e2) {
        toast('error', errText(e2))
      }
    } finally {
      setSaving(false)
    }
  }, [tab.serverId, toast, t])

  // The save closure changes as state does, so the keybinding reads it from a
  // ref rather than being torn down and rebuilt on every render.
  const saveRef = useRef(save)
  saveRef.current = save

  useEffect(() => {
    let cancelled = false
    import('../lib/monaco')
      .then((m) => !cancelled && setMod(m))
      .catch((e) => !cancelled && setError(errText(e)))
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    filesApi
      .read(tab.serverId, path)
      .then((f) => !cancelled && setFile(f))
      .catch((e) => !cancelled && setError(errText(e)))
    return () => {
      cancelled = true
    }
  }, [tab.serverId, path])

  // Create the editor once both the module and the host element exist.
  useEffect(() => {
    if (!mod || !hostRef.current || editorRef.current) return
    const { monaco } = mod
    const editor = monaco.editor.create(hostRef.current, {
      value: '',
      language: 'plaintext',
      theme: mod.defineTheme(theme),
      automaticLayout: true,
      fontFamily:
        "'JetBrains Mono', 'Cascadia Code', ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      fontSize: 12.5,
      lineHeight: 1.55,
      minimap: { enabled: true, renderCharacters: false },
      scrollBeyondLastLine: false,
      renderWhitespace: 'selection',
      smoothScrolling: true,
      tabSize: 2,
      detectIndentation: true,
      bracketPairColorization: { enabled: true },
      padding: { top: 8, bottom: 8 },
      scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10 },
    })
    editorRef.current = editor

    // Bound on the editor, not the document, so Ctrl+S only saves the file you
    // are looking at and only while the caret is in it.
    editor.addAction({
      id: 'agentmux.save',
      // Read once, when the editor is built: Monaco keeps its own copy of the
      // action, and a language change re-labels it on the next open.
      label: tr('editor.saveAction'),
      keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS],
      run: () => void saveRef.current(),
    })

    return () => {
      editor.dispose()
      modelRef.current?.dispose()
      editorRef.current = null
      modelRef.current = null
    }
    // theme is read once here; a later change goes through setTheme below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mod])

  // Attach the file once it and the editor are both ready.
  useEffect(() => {
    if (!mod || !file || !editorRef.current || modelRef.current) return
    const { monaco } = mod
    savedRef.current = file
    const model = monaco.editor.createModel(file.content, mod.languageFor(file.path))
    modelRef.current = model
    editorRef.current.setModel(model)
    savedVersionRef.current = model.getAlternativeVersionId()
    setLines(model.getLineCount())
    const sub = model.onDidChangeContent(() => {
      // Undoing back to the saved state restores the saved version id, so this
      // correctly reports "not dirty" again rather than staying stuck on.
      setDirty(model.getAlternativeVersionId() !== savedVersionRef.current)
      setLines(model.getLineCount())
    })
    return () => sub.dispose()
  }, [mod, file])

  useEffect(() => {
    if (mod && editorRef.current) mod.monaco.editor.setTheme(mod.defineTheme(theme))
  }, [mod, theme])

  // Background tabs stay in the DOM at full size, just hidden, so Monaco's
  // resize observer never fires for them. Measuring once on the way to the
  // front covers the case where the window was resized while this tab was
  // behind another one.
  useEffect(() => {
    if (!active || !editorRef.current) return
    editorRef.current.layout()
    editorRef.current.focus()
  }, [active, mod, file])

  // The tab title carries the dirty mark, so an unsaved file is visible even
  // when its tab is in the background — and the flag lets closeTab ask before
  // throwing the edits away.
  useEffect(() => {
    setTabState(tab.id, { title: dirty ? `${name} •` : name, dirty })
  }, [dirty, name, tab.id, setTabState])

  async function reload() {
    if (dirty) {
      const ok = await confirmAction({
        title: t('editor.discard.title', { name }),
        message: t('editor.discard.message'),
        tone: 'warning',
        confirmLabel: t('editor.discard.confirm'),
      })
      if (!ok) return
    }
    try {
      const fresh = await filesApi.read(tab.serverId, path)
      savedRef.current = fresh
      modelRef.current?.setValue(fresh.content)
      savedVersionRef.current = modelRef.current?.getAlternativeVersionId() ?? 0
      setFile(fresh)
      setDirty(false)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  if (error) {
    return (
      <div className="flex h-full w-full flex-col bg-ink-950">
        <Empty title={t('editor.cannotOpen', { name })} hint={error} />
      </div>
    )
  }

  const ready = !!mod && !!file

  return (
    <div className="flex h-full w-full flex-col bg-ink-950">
      <div className="flex h-9 shrink-0 items-center gap-2 border-b hairline bg-ink-900 px-2.5">
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-300" title={path}>
          {path}
          {dirty && <span className="ml-1.5 text-warn">●</span>}
        </span>
        {ready && (
          <span className="hidden shrink-0 text-[10.5px] text-ink-600 lg:block">
            {mod.languageFor(path)} · {t('editor.lines', { n: lines })} · {file.mode}
            {file.crlf && ' · CRLF'}
          </span>
        )}
        <Button size="sm" onClick={() => void reload()} title={t('editor.reload.title')}>
          <RotateCcw size={11} /> {t('editor.reload')}
        </Button>
        <Button
          size="sm"
          variant="primary"
          disabled={!dirty || saving}
          onClick={() => void save()}
          title={t('editor.save.title')}
        >
          <Save size={11} /> {saving ? t('common.saving') : t('common.save')}
        </Button>
      </div>

      <div className="relative min-h-0 flex-1">
        <div ref={hostRef} className={clsx('h-full w-full', !ready && 'invisible')} />
        {!ready && (
          <div className="absolute inset-0 flex items-center justify-center text-[11px] text-ink-500">
            {t('editor.opening', { name })}
          </div>
        )}
      </div>
    </div>
  )
}

function baseName(p: string): string {
  return p.split('/').filter(Boolean).pop() ?? p
}
