import { useId, useState } from 'react'
import { Icon } from './Icon'

/** Editable list of short strings (genres, labels, collections). */
export function ChipsInput({
  value,
  onChange,
  suggestions = [],
  placeholder,
}: {
  value: string[]
  onChange: (v: string[]) => void
  suggestions?: string[]
  placeholder?: string
}) {
  const [text, setText] = useState('')
  const listId = useId()

  const add = (raw: string) => {
    const v = raw.trim()
    if (v && !value.some((x) => x.toLowerCase() === v.toLowerCase())) onChange([...value, v])
    setText('')
  }

  return (
    <div className="chips-input">
      {value.map((v) => (
        <span key={v} className="chip removable">
          {v}
          <button type="button" onClick={() => onChange(value.filter((x) => x !== v))} aria-label={`Remove ${v}`}>
            <Icon name="x" size={12} />
          </button>
        </span>
      ))}
      <input
        list={listId}
        value={text}
        placeholder={value.length ? '' : placeholder}
        onChange={(e) => {
          // Picking a datalist suggestion fires change with the full value.
          const v = e.target.value
          if (v.endsWith(',')) add(v.slice(0, -1))
          else if (suggestions.includes(v)) add(v)
          else setText(v)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            add(text)
          } else if (e.key === 'Backspace' && !text && value.length) {
            onChange(value.slice(0, -1))
          }
        }}
        onBlur={() => text && add(text)}
      />
      <datalist id={listId}>
        {suggestions
          .filter((s) => !value.includes(s))
          .map((s) => (
            <option key={s} value={s} />
          ))}
      </datalist>
    </div>
  )
}
