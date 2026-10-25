import { useRef, useState } from 'react'
import { api } from '../api'
import { Icon } from './Icon'

/** A profile picture, or the name's first letter when there is none. */
export function Avatar({ name, src, className = '' }: { name: string; src?: string; className?: string }) {
  const [broken, setBroken] = useState(false)
  if (src && !broken) return <img className={`avatar avatar-img ${className}`} src={src} alt="" onError={() => setBroken(true)} />
  return <span className={`avatar ${className}`}>{name.slice(0, 1).toUpperCase()}</span>
}

const AVATAR_SIZE = 512

/** Crops a picture to its centre square and shrinks it, so uploads stay small. */
async function squareJPEG(file: File): Promise<Blob> {
  const bitmap = await createImageBitmap(file).catch(() => {
    throw new Error('That file is not a picture this browser can open')
  })
  const side = Math.min(bitmap.width, bitmap.height)
  const out = Math.min(side, AVATAR_SIZE)
  const canvas = document.createElement('canvas')
  canvas.width = out
  canvas.height = out
  canvas.getContext('2d')!.drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, out, out)
  bitmap.close()
  return new Promise((resolve, reject) =>
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('Could not read the picture'))), 'image/jpeg', 0.9),
  )
}

/** Upload or remove a household member's picture. */
export function AvatarPicker({
  memberId,
  name,
  src,
  onChange,
}: {
  memberId: number
  name: string
  src?: string
  /** Called with the new picture's URL, or undefined once it's removed. */
  onChange: (src?: string) => void
}) {
  const input = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const upload = async (file: File) => {
    setBusy(true)
    setError('')
    try {
      const body = new FormData()
      body.append('file', await squareJPEG(file), 'avatar.jpg')
      const res = await api.put<{ avatar: string }>(`/api/household/${memberId}/avatar`, body)
      onChange(res.avatar)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const remove = async () => {
    setError('')
    await api.del(`/api/household/${memberId}/avatar`).then(() => onChange(undefined), (e) => setError(e.message))
  }

  return (
    <div className="avatar-picker">
      <Avatar name={name} src={src} className="big" />
      <div className="actions">
        <button type="button" className="btn btn-glass btn-sm" disabled={busy} onClick={() => input.current?.click()}>
          <Icon name="image" size={16} /> {busy ? 'Uploading…' : src ? 'Change picture' : 'Upload picture'}
        </button>
        {src && (
          <button type="button" className="btn btn-glass btn-sm" onClick={remove}>
            Remove
          </button>
        )}
      </div>
      <input
        ref={input}
        type="file"
        accept="image/*"
        hidden
        onChange={(e) => {
          const f = e.target.files?.[0]
          e.target.value = ''
          if (f) upload(f)
        }}
      />
      {error && <div className="form-error">{error}</div>}
    </div>
  )
}
