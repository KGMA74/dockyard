interface Props {
  onChangePassword: () => void
  onLogout: () => void
}

export default function Sidebar({ onChangePassword, onLogout }: Props) {
  return (
    <aside className="w-56 shrink-0 h-screen sticky top-0 border-r border-zinc-800/80 bg-zinc-950 flex flex-col">
      <div className="h-14 flex items-center gap-2.5 px-4 border-b border-zinc-800/80">
        <svg className="w-5 h-5 text-blue-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
            d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10" />
        </svg>
        <span className="font-semibold text-zinc-100 text-sm tracking-tight">Dockyard</span>
      </div>

      <nav className="flex-1 px-3 py-4 space-y-1">
        <div className="flex items-center gap-2.5 px-3 py-2 rounded-lg bg-zinc-900 text-zinc-100 text-sm font-medium">
          <svg className="w-4 h-4 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
              d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10" />
          </svg>
          Images
        </div>
      </nav>

      <div className="px-3 py-4 border-t border-zinc-800/80 space-y-1">
        <button
          onClick={onChangePassword}
          className="w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-zinc-500 hover:text-zinc-200 hover:bg-zinc-900 text-sm transition-colors"
        >
          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
              d="M15.75 5.25a3 3 0 013 3m3 0a6 6 0 01-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1121.75 8.25z" />
          </svg>
          Change password
        </button>
        <button
          onClick={onLogout}
          className="w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-zinc-500 hover:text-zinc-200 hover:bg-zinc-900 text-sm transition-colors"
        >
          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
              d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15M12 9l-3 3m0 0l3 3m-3-3h12.75" />
          </svg>
          Sign out
        </button>
      </div>
    </aside>
  )
}
