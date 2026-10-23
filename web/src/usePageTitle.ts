import { useEffect } from 'react'

const SUFFIX = 'CineFund'

/** Sets the browser tab / history title for the current page. */
export function usePageTitle(title?: string | null) {
  useEffect(() => {
    document.title = title ? `${title} · ${SUFFIX}` : `${SUFFIX} — Fund independent films. Watch them come to life.`
  }, [title])
}
