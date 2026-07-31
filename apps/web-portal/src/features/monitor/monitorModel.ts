export const severityLabel = (value: string) =>
  ({ critical: '严重', warning: '警告', info: '提示' })[value] || value

export const metricTone = (
  value: number,
  warning: number,
  critical: number,
  direction: 'higher' | 'lower' = 'higher',
) => {
  if (direction === 'lower') {
    if (value <= critical) return 'critical'
    if (value <= warning) return 'warning'
    return 'healthy'
  }
  if (value >= critical) return 'critical'
  if (value >= warning) return 'warning'
  return 'healthy'
}

export const metricDirection = (id: string): 'higher' | 'lower' =>
  id === 'redis-hit-rate' ? 'lower' : 'higher'

export const trendPoints = (trend: number[]) => {
  if (!trend.length) return ''
  const max = Math.max(...trend)
  const min = Math.min(...trend)
  const range = Math.max(max - min, 1)
  const step = trend.length > 1 ? 200 / (trend.length - 1) : 0
  return trend.map((value, index) => `${index * step},${50 - ((value - min) / range) * 42}`).join(' ')
}
