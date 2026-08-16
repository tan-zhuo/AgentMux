import * as monaco from 'monaco-editor'
import editorWorker from 'monaco-editor/editor/editor.worker?worker'
import cssWorker from 'monaco-editor/language/css/css.worker?worker'
import htmlWorker from 'monaco-editor/language/html/html.worker?worker'
import jsonWorker from 'monaco-editor/language/json/json.worker?worker'
import tsWorker from 'monaco-editor/language/typescript/ts.worker?worker'
import type { Theme } from './themes'

// Monaco loads its language services in workers. They are bundled rather than
// pulled from a CDN because the app runs offline inside a webview with no
// network origin to fetch them from.
self.MonacoEnvironment = {
  getWorker(_id: string, label: string) {
    switch (label) {
      case 'json':
        return new jsonWorker()
      case 'css':
      case 'scss':
      case 'less':
        return new cssWorker()
      case 'html':
      case 'handlebars':
      case 'razor':
        return new htmlWorker()
      case 'typescript':
      case 'javascript':
        return new tsWorker()
      default:
        return new editorWorker()
    }
  },
}

export { monaco }

/** Filenames that carry their language in the whole name, not the extension. */
const byFilename: Record<string, string> = {
  dockerfile: 'dockerfile',
  makefile: 'makefile',
  'docker-compose.yml': 'yaml',
  'docker-compose.yaml': 'yaml',
  '.gitignore': 'plaintext',
  '.env': 'shell',
  'go.mod': 'plaintext',
  'go.sum': 'plaintext',
}

const byExtension: Record<string, string> = {
  go: 'go', rs: 'rust', py: 'python', rb: 'ruby', php: 'php', java: 'java',
  kt: 'kotlin', swift: 'swift', c: 'c', h: 'c', cc: 'cpp', cpp: 'cpp', hpp: 'cpp',
  cs: 'csharp', ts: 'typescript', tsx: 'typescript', js: 'javascript', jsx: 'javascript',
  mjs: 'javascript', cjs: 'javascript', json: 'json', jsonc: 'json', yml: 'yaml',
  yaml: 'yaml', toml: 'ini', ini: 'ini', conf: 'ini', cfg: 'ini', xml: 'xml',
  html: 'html', htm: 'html', css: 'css', scss: 'scss', less: 'less',
  md: 'markdown', markdown: 'markdown', sh: 'shell', bash: 'shell', zsh: 'shell',
  fish: 'shell', ps1: 'powershell', sql: 'sql', lua: 'lua', pl: 'perl', r: 'r',
  dart: 'dart', scala: 'scala', clj: 'clojure', ex: 'elixir', exs: 'elixir',
  erl: 'plaintext', hs: 'plaintext', vue: 'html', svelte: 'html', graphql: 'graphql',
  proto: 'plaintext', tf: 'hcl', hcl: 'hcl', bat: 'bat', cmd: 'bat', log: 'plaintext',
}

/** Best guess at a file's language from its name. */
export function languageFor(path: string): string {
  const name = (path.split('/').pop() ?? '').toLowerCase()
  if (byFilename[name]) return byFilename[name]
  if (name.startsWith('dockerfile')) return 'dockerfile'
  const ext = name.includes('.') ? name.split('.').pop()! : ''
  return byExtension[ext] ?? 'plaintext'
}

/**
 * Builds a Monaco theme from the app's own tokens, so the editor belongs to the
 * window it sits in rather than looking like a component someone dropped in.
 */
export function defineTheme(theme: Theme): string {
  const id = `agentmux-${theme.id}`
  const c = theme.colors
  const t = theme.terminal
  const base: monaco.editor.BuiltinTheme = theme.mode === 'light' ? 'vs' : 'vs-dark'

  monaco.editor.defineTheme(id, {
    base,
    inherit: true,
    rules: [
      { token: 'comment', foreground: strip(c['ink-500']), fontStyle: 'italic' },
      { token: 'string', foreground: strip(t.green) },
      { token: 'number', foreground: strip(t.magenta) },
      { token: 'keyword', foreground: strip(t.blue) },
      { token: 'type', foreground: strip(t.cyan) },
      { token: 'function', foreground: strip(t.yellow) },
      { token: 'variable', foreground: strip(c['ink-100']) },
      { token: 'delimiter', foreground: strip(c['ink-300']) },
    ],
    colors: {
      'editor.background': c['ink-950'],
      'editor.foreground': c['ink-100'],
      'editorLineNumber.foreground': c['ink-600'],
      'editorLineNumber.activeForeground': c['ink-300'],
      'editorCursor.foreground': c.accent,
      'editor.selectionBackground': `${c.accent}33`,
      'editor.inactiveSelectionBackground': `${c.accent}1f`,
      'editor.lineHighlightBackground': `${c['ink-800']}80`,
      'editorIndentGuide.background1': c['ink-800'],
      'editorIndentGuide.activeBackground1': c['ink-700'],
      'editorWidget.background': c['ink-850'],
      'editorWidget.border': c['ink-700'],
      'editorSuggestWidget.background': c['ink-850'],
      'editorSuggestWidget.border': c['ink-700'],
      'editorSuggestWidget.selectedBackground': `${c.accent}26`,
      'editorHoverWidget.background': c['ink-850'],
      'editorHoverWidget.border': c['ink-700'],
      'editorGutter.background': c['ink-950'],
      'scrollbarSlider.background': `${c['ink-600']}99`,
      'scrollbarSlider.hoverBackground': c['ink-600'],
      'scrollbarSlider.activeBackground': c['ink-500'],
      'minimap.background': c['ink-950'],
      'editorError.foreground': c.danger,
      'editorWarning.foreground': c.warn,
    },
  })
  return id
}

/** Monaco wants six-digit hex without alpha in rule tokens. */
function strip(hex: string): string {
  return hex.replace('#', '').slice(0, 6)
}
