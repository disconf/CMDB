export const categoryLabel=(v:string)=>({network:'网络诊断',host:'主机诊断',kubernetes:'Kubernetes',logs:'日志检索',observability:'可观测性',database:'数据库',topology:'拓扑分析'}[v]||v)
export const resultTone=(v:string)=>v==='failed'?'danger':v==='warning'?'warning':'success'
