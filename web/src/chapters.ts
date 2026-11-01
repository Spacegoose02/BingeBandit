import { useEffect, useState } from 'react'
import { api } from './api'
import type { Chapter } from './types'

/** The chapter marks of a file, or [] while loading or when it has none. */
export function useChapters(fileId: number | undefined): Chapter[] {
  const [chapters, setChapters] = useState<Chapter[]>([])
  useEffect(() => {
    setChapters([])
    if (!fileId) return
    let stale = false
    api.get<{ chapters: Chapter[] }>(`/api/files/${fileId}/chapters`).then(
      (r) => !stale && setChapters(r.chapters ?? []),
      () => undefined,
    )
    return () => {
      stale = true
    }
  }, [fileId])
  return chapters
}

/** The index of the chapter playing at t, or -1. */
export function chapterAt(chapters: Chapter[], t: number): number {
  for (let i = chapters.length - 1; i >= 0; i--) {
    if (t >= chapters[i].start) return i
  }
  return -1
}

/** Where "next chapter" goes from t, or null in the last chapter. */
export function nextChapterStart(chapters: Chapter[], t: number): number | null {
  const next = chapters.find((c) => c.start > t + 0.5)
  return next ? next.start : null
}

/**
 * Where "previous chapter" goes from t: the start of the current chapter,
 * or the one before when that start was less than 3 seconds ago (as a CD
 * player's back button does).
 */
export function prevChapterStart(chapters: Chapter[], t: number): number {
  const i = chapterAt(chapters, t)
  if (i < 0) return 0
  if (t - chapters[i].start > 3 || i === 0) return chapters[i].start
  return chapters[i - 1].start
}

/** The chapter's title, or "Chapter N" for one without a name. */
export function chapterName(chapters: Chapter[], i: number): string {
  return chapters[i]?.title || `Chapter ${i + 1}`
}
