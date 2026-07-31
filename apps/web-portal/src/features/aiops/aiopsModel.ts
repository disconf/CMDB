export const confidenceTone=(v:number)=>v>=90?'high':v>=75?'medium':'low'
export const riskLabel=(v:string)=>({low:'低风险',medium:'中风险',high:'高风险'}[v]||v)
