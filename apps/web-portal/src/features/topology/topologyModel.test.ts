import {describe,expect,it} from 'vitest'
import {healthLabel,impactCount} from './topologyModel'
describe('topology model',()=>{it('labels health',()=>expect(healthLabel('critical')).toBe('严重'));it('counts affected nodes',()=>expect(impactCount({affected:[{},{}]})).toBe(2))})
