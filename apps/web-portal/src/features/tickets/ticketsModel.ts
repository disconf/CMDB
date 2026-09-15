export const priorityLabel=(v:string)=>({critical:'紧急',high:'高',medium:'中',low:'低'}[v]||v)
export const statusLabel=(v:string)=>({pending:'待审批',processing:'处理中',approved:'已通过',completed:'已完成',rejected:'已驳回',cancelled:'已取消'}[v]||v)
export const ticketTypeLabel=(v:string)=>({incident:'故障工单',change:'变更工单',resource:'资源申请',release:'发布工单',access:'权限申请',inspection:'巡检工单'}[v]||v)