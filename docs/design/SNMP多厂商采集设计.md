# SNMP 统一采集设计（多厂商网络设备 + 服务器带外/BMC）

## 1. 目标
一套 SNMP v2c 采集器即可：
- 识别并采集华为(Huawei VRP)、华三(H3C/Comware)、思科(Cisco IOS/IOS-XE/NX-OS)、锐捷(Ruijie RGOS)等常见网络设备
- 采集服务器带外管理口(BMC)：iLO(HPE)、iDRAC(Dell)、BMC/IPMI、华为 iBMC、超微等
- 入 CMDB：网络设备→network-device；带外→记录在对应服务器(bmc_ip)或按配置建资产

## 2. 数据采集内容
通用 OID（所有设备都有）：
- sysDescr 1.3.6.1.2.1.1.1.0
- sysObjectID 1.3.6.1.2.1.1.2.0
- sysName 1.3.6.1.2.1.1.5.0
- sysUpTime 1.3.6.1.2.1.1.3.0
- ifNumber 1.3.6.1.2.1.2.1.0

厂商/型号/序列号（ENTITY-MIB，多数企业设备支持，缺失则回退 sysDescr 解析）：
- 1.3.6.1.2.1.47.1.1.1.1.2  entPhysicalDescr
- 1.3.6.1.2.1.47.1.1.1.1.5  entPhysicalName
- 1.3.6.1.2.1.47.1.1.1.1.8  entPhysicalSoftwareRev
- 1.3.6.1.2.1.47.1.1.1.1.11 entPhysicalSerialNum
- 1.3.6.1.2.1.47.1.1.1.1.13 entPhysicalModelName
- 1.3.6.1.2.1.47.1.1.1.1.7  entPhysicalHardwareRev

厂商识别关键字（sysDescr/sysObjectID）：
- Huawei: VRP / HUAWEI / .1.3.6.1.4.1.2011
- H3C: Comware / H3C / .1.3.6.1.4.1.25506 / HPE Comware
- Cisco: IOS / IOS-XE / NX-OS / Catalyst / .1.3.6.1.4.1.9
- Ruijie: RGOS / Ruijie / .1.3.6.1.4.1.4881
- 服务器带外: iLO / iDRAC / iBMC / BMC / IPMI / Integrated Lights-Out / DRAC / Redfish
- 其它: 按 sysDescr 文本保留

## 3. 归类策略
- 默认网络设备 → network-device
- 检测到带外关键字且选了“服务器带外”/自动：
  - 优先尝试按 sysName/hostname 匹配已有服务器资产，更新其 bmc_ip；
  - 匹配不到时，可建 physical-server（带 bmc_ip 属性、tag=BMC），或只入“待认领”避免脏数据（可配置）。

## 4. 需要厂商适配的点
各厂商 ENTITY-MIB 索引可能从不同子索引取 chassis：先取所有 serial/model 非空值，优先 chassis 实体。带外设备型号一般 sysDescr 就有（如 iLO 5 / iDRAC9 / iBMC）。

## 5. 落地顺序
1. 通用采集升级：sysObjectID + ENTITY-MIB 厂商/型号/序列号 + 带外识别(已按此实现)
2. 每厂商用真实设备验证并微调（华为/华三/思科/锐捷各一台 + 服务器带外）
3. 带外资产匹配：按 hostname/IP 自动把 bmc_ip 回填到服务器；可“待认领”
4. 可选 LLDP(1.0.8802.1.1.2.1.4) 邻居→CMDB 链路关系→拓扑
