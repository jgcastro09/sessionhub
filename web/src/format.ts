export function formatCost(costMicrosUsd: number): string {
  return `$${(costMicrosUsd / 1_000_000).toFixed(2)}`
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

export function formatRelativeTime(iso: string | undefined): string {
  if (!iso) return '-'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return '-'
  const diffMs = Date.now() - date.getTime()
  const diffSec = Math.round(diffMs / 1000)
  const abs = Math.abs(diffSec)
  const future = diffSec < 0

  const units: [number, string][] = [
    [60, 's'],
    [60, 'min'],
    [24, 'h'],
    [7, 'd'],
    [4.345, 'sem'],
    [12, 'mês'],
    [Number.POSITIVE_INFINITY, 'ano'],
  ]
  let value = abs
  let unit = 's'
  for (const [factor, label] of units) {
    unit = label
    if (value < factor) break
    value /= factor
  }
  const rounded = Math.max(1, Math.round(value))
  return future ? `em ${rounded}${unit}` : `há ${rounded}${unit}`
}
