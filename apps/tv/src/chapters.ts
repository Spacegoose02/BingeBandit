import { useEffect, useState } from 'react'
import { api } from './api'

/** A chapter mark in a file; seconds from the start of the file. */
export interface Chapter {
  start: number
  end: number
  title?: string
}

/** The chapter marks of a file, or [] while loading or when it has none. */
export function useChapters(fileId?: number): Chapter[] {
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

/** The chapter's title, or "Chapter N" for one without a name. */
export function chapterName(chapters: Chapter[], i: number): string {
  return chapters[i]?.title || `Chapter ${i + 1}`
}
