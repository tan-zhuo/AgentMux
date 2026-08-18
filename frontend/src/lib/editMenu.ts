import { Clipboard } from '@wailsio/runtime'
import { ClipboardPaste, Copy, Scissors, TextCursorInput } from 'lucide-react'
import { useContextMenu } from '../store/useContextMenu'
import { t } from '../store/useI18n'

/**
 * The editing menu for text fields.
 *
 * Suppressing the webview's menu everywhere would otherwise leave inputs with
 * no way to paste by mouse, which people reasonably expect from a text box.
 */
export async function showEditMenu(e: MouseEvent, target: HTMLElement) {
  const field = (
    target.tagName === 'INPUT' || target.tagName === 'TEXTAREA'
      ? target
      : target.closest('input, textarea')
  ) as HTMLInputElement | HTMLTextAreaElement | null
  if (!field) return

  const start = field.selectionStart ?? 0
  const end = field.selectionEnd ?? 0
  const selected = field.value.slice(start, end)
  const readOnly = field.readOnly || field.disabled

  // Replaces the selection the way typing would, so undo history and React's
  // onChange both see a normal edit.
  const insert = (text: string) => {
    field.focus()
    field.setRangeText(text, field.selectionStart ?? 0, field.selectionEnd ?? 0, 'end')
    field.dispatchEvent(new Event('input', { bubbles: true }))
  }

  useContextMenu.getState().show(e.clientX, e.clientY, [
    {
      label: t('edit.cut'),
      icon: Scissors,
      hint: 'Ctrl+X',
      disabled: !selected || readOnly,
      onSelect: async () => {
        await Clipboard.SetText(selected)
        insert('')
      },
    },
    {
      label: t('edit.copy'),
      icon: Copy,
      hint: 'Ctrl+C',
      disabled: !selected,
      onSelect: () => void Clipboard.SetText(selected),
    },
    {
      label: t('edit.paste'),
      icon: ClipboardPaste,
      hint: 'Ctrl+V',
      disabled: readOnly,
      onSelect: async () => {
        const text = await Clipboard.Text()
        if (text) insert(text)
      },
    },
    {},
    {
      label: t('edit.selectAll'),
      icon: TextCursorInput,
      hint: 'Ctrl+A',
      onSelect: () => {
        field.focus()
        field.select()
      },
    },
  ])
}
