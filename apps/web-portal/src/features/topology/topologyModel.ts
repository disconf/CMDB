export const healthLabel=(status:string)=>({healthy:'健康',warning:'关注',critical:'严重'}[status]||status)
export const impactCount=(impact:{affected:unknown[]}|undefined)=>impact?.affected.length??0
