export const statusLabel=(s:string)=>({awaiting_approval:'待审批',pending:'待执行',running:'执行中',success:'成功',failed:'失败',cancelled:'已取消'}[s]||s)
export const progressTone=(s:string)=>s==='failed'?'danger':s==='success'?'success':'active'
