import http from 'k6/http';
import { check } from 'k6';
import { Trend } from 'k6/metrics';

import { encodeSummary } from './summary.js';

const TARGET = 'http://sut-lab:8080/work';
const SCENARIO = 'closed';
const SUMMARY_PATH = __ENV.SUMMARY_PATH || '/runs/cf001/closed-summary.json';

const workLatency = new Trend('work_latency', true);

export const options = {
  discardResponseBodies: true,
  scenarios: {
    closed: {
      executor: 'constant-vus',
      vus: 5,
      duration: '30s',
      gracefulStop: '1s',
    },
  },
};

export default function () {
  const response = http.get(TARGET, {
    headers: {
      'X-CollapseLab-Scenario': 'closed',
    },
    tags: {
      scenario: SCENARIO,
      endpoint: 'work',
    },
  });

  workLatency.add(response.timings.duration);

  check(response, {
    'work status is 204': (result) => result.status === 204,
  });
}

export function handleSummary(data) {
  return {
    [SUMMARY_PATH]: encodeSummary(data, SCENARIO),
  };
}
