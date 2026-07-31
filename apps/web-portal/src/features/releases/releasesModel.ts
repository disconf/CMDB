export const releaseStatusLabel=(s:string)=>({running:'发布中',success:'成功',failed:'失败','rolling-back':'回滚中'}[s]||s)
export const stageProgress=(stages:{status:string}[])=>stages.length?Math.round(stages.filter(s=>s.status==='success').length/stages.length*100):0
