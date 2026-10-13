import http from 'k6/http'
import { check, sleep } from 'k6'
export const options = { scenarios: { assets: { executor: 'ramping-vus', stages: [{duration:'30s',target:50},{duration:'2m',target:200},{duration:'30s',target:0}] } }, thresholds: { http_req_failed: ['rate<0.01'], http_req_duration: ['p(95)<800'] } }
const BASE=__ENV.BASE_URL||'http://172.28.69.164:30080'
const TOKEN=__ENV.ACCESS_TOKEN
export default function(){ const response=http.get(`${BASE}/api/v1/cmdb/assets?pageSize=50`,{headers:{Authorization:`Bearer ${TOKEN}`}}); check(response,{ok:r=>r.status===200}); sleep(1) }
