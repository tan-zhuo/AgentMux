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
      className="material-bar flex shrink-0 items-stretch border-t hairline"
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
        'transition-colors duration-[var(--duration-quick)] active:text-ink-100',
        active ? 'text-accent' : 'text-ink-400',
      )}
    >
      {/* The glyph sits in a tile that tints when selected — iOS 26's tab bar
          marks the active tab with a lifted pill, not just a recolour. */}
      <span
        className={clsx(
          'flex h-6 w-10 items-center justify-center rounded-capsule',
          'transition-[background-color,transform] duration-[var(--duration-base)] ease-[var(--ease-spring)]',
          active ? 'bg-accent/15' : 'bg-transparent',
        )}
      >
        {icon}
      </span>
      {label}
    </button>
  )
}
