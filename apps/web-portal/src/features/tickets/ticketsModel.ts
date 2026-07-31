export const priorityLabel=(v:string)=>({critical:'紧急',high:'高',medium:'中',low:'低'}[v]||v)
export const statusLabel=(v:string)=>({pending:'待审批',processing:'处理中',approved:'已通过',rejected:'已驳回'}[v]||v)
