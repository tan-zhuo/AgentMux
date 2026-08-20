import clsx from 'clsx'
import { Command, PanelLeft, PanelRight, Settings } from 'lucide-react'
import { useAppStore } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { useT } from '../store/useI18n'

/**
 * The compact layout's navigation: a thumb-height bar along the bottom edge,
 * because that is where a hand actually is on a phone. It replaces the status
 * bar there — the desktop's top-corner toggles technically exist on a phone
 * too, but a control nobody can find or reach does not count as one.
 */
export function MobileNav() {
  const sidebarOpen = useAppStore((s) => s.sidebarOpen)
  const rightOpen = useAppStore((s) => s.rightOpen)
  const toggleSidebar = useAppStore((s) => s.toggleSidebar)
  const toggleRight = useAppStore((s) => s.toggleRight)
  const setPaletteOpen = useAppStore((s) => s.setPaletteOpen)
  const openDialog = useDialogs((s) => s.open)
  const t = useT()

  return (
    <nav
      className="flex shrink-0 items-stretch border-t hairline bg-ink-900"
      style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
    >
      <NavButton
        icon={<PanelLeft size={18} />}
        label={t('nav.tree')}
        active={sidebarOpen}
        onClick={toggleSidebar}
      />
      <NavButton
        icon={<Command size={18} />}
        label={t('nav.palette')}
        onClick={() => setPaletteOpen(true)}
      />
      <NavButton
        icon={<PanelRight size={18} />}
        label={t('nav.panel')}
        active={rightOpen}
        onClick={toggleRight}
      />
      <NavButton
        icon={<Settings size={18} />}
        label={t('nav.settings')}
        onClick={() => openDialog({ kind: 'settings' })}
      />
    </nav>
  )
}

function NavButton({
  icon,
  label,
  active,
  onClick,
}: {
  icon: React.ReactNode
  label: string
  active?: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={clsx(
        'flex h-12 flex-1 flex-col items-center justify-center gap-0.5 text-[10px]',
        active ? 'text-accent' : 'text-ink-400',
      )}
    >
      {icon}
      {label}
    </button>
  )
}
