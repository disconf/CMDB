export const userStatusLabel=(v:string)=>({active:'正常',disabled:'已停用',locked:'已锁定'}[v]||v)
export const auditTone=(v:string)=>v==='failed'?'danger':'success'
