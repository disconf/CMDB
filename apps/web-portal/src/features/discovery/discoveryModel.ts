export const agentStatusLabel=(status:string)=>status==='online'?'在线':status==='pending'?'待连接':'离线'
export const taskProgress=(task:{discovered:number;imported:number})=>task.discovered?Math.round(task.imported/task.discovered*100):0
