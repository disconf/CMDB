import http from 'k6/http'
import { check, sleep } from 'k6'
import { Counter } from 'k6/metrics'

const target = Number(__ENV.TARGET_VUS || 200)
const ramp = __ENV.RAMP_DURATION || '30s'
const hold = __ENV.HOLD_DURATION || '2m'
const statuses = new Counter('status_codes')

export const options = {
  scenarios: {
    assets: {
      executor: 'ramping-vus',
      stages: [
        { duration: ramp, target: Math.max(1, Math.floor(target / 4)) },
        { duration: hold, target },
        { duration: ramp, target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<800'],
  },
}

const BASE=__ENV.BASE_URL||'http://172.28.69.164:30080'
const TOKEN=__ENV.ACCESS_TOKEN

export default function(){
  const response=http.get(`${BASE}/api/v1/cmdb/assets?pageSize=50`,{headers:{Authorization:`Bearer ${TOKEN}`}})
  statuses.add(1, { status: String(response.status) })
  check(response,{ ok:r=>r.status===200 })
  sleep(1)
}
