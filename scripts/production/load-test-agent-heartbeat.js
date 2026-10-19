import http from 'k6/http'
import { check } from 'k6'
export const options = { scenarios: { heartbeat: { executor: 'constant-arrival-rate', rate: 100, timeUnit: '1s', duration: '2m', preAllocatedVUs: 200, maxVUs: 800 } }, thresholds: { http_req_failed: ['rate<0.01'] } }
const BASE=__ENV.BASE_URL||'http://172.28.69.164:30080'
const TOKEN=__ENV.AGENT_SHARED_TOKEN
export default function(){ const response=http.post(`${BASE}/api/v1/agent/report`,JSON.stringify({agentId:`load-${__VU}`,hostname:`load-${__VU}`,ip:`10.255.${Math.floor(__VU/250)%250}.${(__VU%250)+1}`,status:'online',version:'load-test'}),{headers:{Authorization:`Bearer ${TOKEN}`,'Content-Type':'application/json'}}); check(response,{accepted:r=>r.status===200||r.status===202}) }
