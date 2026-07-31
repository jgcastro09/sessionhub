import { useState } from 'react'
import { pair } from '../api'

export function PairingGate({ onPaired }: { onPaired: () => void }) {
  const [code, setCode] = useState('')
  const [error, setError] = useState<string>()
  const [submitting, setSubmitting] = useState(false)

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    setSubmitting(true)
    setError(undefined)
    try {
      await pair(code)
      onPaired()
    } catch {
      setError('Código inválido. Confira o código exibido na aba Settings do SessionHub.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="pairing-screen">
      <form className="pairing-card" onSubmit={submit}>
        <h1>Parear este dispositivo</h1>
        <p>Digite o código de 6 dígitos exibido em Settings no SessionHub.</p>
        <input
          className="pairing-input"
          inputMode="numeric"
          pattern="[0-9]*"
          maxLength={6}
          autoFocus
          value={code}
          onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))}
          placeholder="000000"
        />
        {error && <div className="error-banner">{error}</div>}
        <button className="pairing-button" type="submit" disabled={code.length !== 6 || submitting}>
          {submitting ? 'Pareando...' : 'Parear'}
        </button>
      </form>
    </div>
  )
}
