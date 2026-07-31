import { describe,expect,it } from 'vitest'
import { buildAssetQuery,statusLabel,typeLabel } from './cmdbModel'

describe('cmdbModel',()=>{
  it('builds a compact asset query',()=>{
    expect(buildAssetQuery({search:'prod',type:'physical-server',status:'',projectGroup:'核心系统组',page:2,pageSize:20})).toBe('q=prod&type=physical-server&projectGroup=%E6%A0%B8%E5%BF%83%E7%B3%BB%E7%BB%9F%E7%BB%84&page=2&pageSize=20')
  })
  it('maps domain values to readable labels',()=>{
    expect(statusLabel('online')).toBe('在线');expect(statusLabel('warning')).toBe('异常');expect(typeLabel('k8s-node')).toBe('K8s 节点')
  })
})
