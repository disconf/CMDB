import http from 'k6/http'
import { check } from 'k6'
import { Counter } from 'k6/metrics'

const agentCount = Number(__ENV.AGENT_COUNT || 1000)
const statuses = new Counter('status_codes')

export const options = {
  scenarios: {
    heartbeat: {
      executor: 'per-vu-iterations',
      vus: agentCount,
      iterations: 1,
      maxDuration: __ENV.MAX_DURATION || '2m',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<800'],
  },
}

const base = __ENV.BASE_URL || 'http://172.28.69.164:30080'
const token = __ENV.AGENT_SHARED_TOKEN

export default function () {
  const index = __VU
  const payload = JSON.stringify({
    agentId: `load-agent-${index}`,
    hostname: `load-agent-${index}`,
    ip: `10.250.${Math.floor(index / 250) % 250}.${(index % 250) + 1}`,
    status: 'online',
    version: 'load-test',
    os: 'Linux',
    architecture: 'x86_64',
    cpuCount: 8,
    memoryBytes: 17179869184,
    diskBytes: 536870912000,
  })
  const response = http.post(`${base}/api/v1/agent/report`, payload, {
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
  })
  statuses.add(1, { status: String(response.status) })
  check(response, { accepted: (item) => item.status === 200 || item.status === 202 })
}
