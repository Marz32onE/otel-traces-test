import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.4/index.js';

const baseUrl = __ENV.API_BASE_URL || 'http://otel-traces-test-api:8088';
const sendTraceparent = (__ENV.K6_SEND_TRACEPARENT || 'true').toLowerCase() === 'true';
const duration = __ENV.K6_DURATION || '5m';
const jetstreamRate = Number(__ENV.K6_JETSTREAM_RATE || 80);
const coreRate = Number(__ENV.K6_CORE_RATE || 60);
const mongoRate = Number(__ENV.K6_MONGO_RATE || 60);
const totalRate = jetstreamRate + coreRate + mongoRate;

export const options = {
  discardResponseBodies: true,
  scenarios: {
    mixed: {
      executor: 'constant-arrival-rate',
      rate: totalRate,
      timeUnit: '1s',
      duration,
      preAllocatedVUs: 50,
      maxVUs: 150,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{endpoint:jetstream}': ['p(95)<5000'],
    'http_req_duration{endpoint:core}': ['p(95)<5000'],
    'http_req_duration{endpoint:mongo}': ['p(95)<8000'],
  },
};

function headers() {
  const h = { 'Content-Type': 'application/json' };
  if (sendTraceparent) {
    const vu = exec.vu.idInTest;
    const iter = exec.scenario.iterationInTest;
    const traceId = vu.toString(16).padStart(32, '0').slice(-32);
    const spanId = iter.toString(16).padStart(16, '0').slice(-16);
    h.traceparent = `00-${traceId}-${spanId}-01`;
  }
  return h;
}

function post(path, endpoint) {
  const body = JSON.stringify({
    text: `load-${endpoint}-${exec.vu.idInTest}-${exec.scenario.iterationInTest}`,
  });
  const res = http.post(`${baseUrl}${path}`, body, {
    headers: headers(),
    tags: { endpoint },
  });
  check(res, { 'status is 200': (r) => r.status === 200 });
}

export default function () {
  const roll = Math.random() * totalRate;
  if (roll < jetstreamRate) {
    post('/api/message', 'jetstream');
  } else if (roll < jetstreamRate + coreRate) {
    post('/api/message-core', 'core');
  } else {
    post('/api/message-mongo', 'mongo');
  }
}

export function handleSummary(data) {
  const path = __ENV.K6_SUMMARY_PATH;
  const out = { stdout: textSummary(data, { indent: ' ', enableColors: false }) };
  if (path) {
    out[path] = JSON.stringify(data, null, 2);
  }
  return out;
}
