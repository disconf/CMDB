import { describe, expect, it } from 'vitest'
import { metricDirection, metricTone, severityLabel, trendPoints } from './monitorModel'

describe('monitor model', () => {
  it('labels severity', () => expect(severityLabel('critical')).toBe('严重'))

  it('calculates higher-is-worse metric tone', () => {
    expect(metricTone(92, 80, 90)).toBe('critical')
    expect(metricTone(85, 80, 90)).toBe('warning')
  })

  it('calculates lower-is-worse metric tone', () => {
    expect(metricDirection('redis-hit-rate')).toBe('lower')
    expect(metricTone(75, 90, 80, 'lower')).toBe('critical')
    expect(metricTone(85, 90, 80, 'lower')).toBe('warning')
    expect(metricTone(98, 90, 80, 'lower')).toBe('healthy')
  })

  it('builds safe trend points for flat and empty series', () => {
    expect(trendPoints([])).toBe('')
    expect(trendPoints([5, 5])).not.toContain('NaN')
  })
})
