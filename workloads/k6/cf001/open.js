import http from 'k6/http';
import { check } from 'k6';
import { Trend } from 'k6/metrics';

import { encodeSummary } from './summary.js';

const TARGET = 'http://sut-lab:8080/work';
const SCENARIO = 'open';
const SUMMARY_PATH = __ENV.SUMMARY_PATH || '/runs/cf001/open-summary.json';

const workLatency = new Trend('work_latency', true);

export const options = {
  discardResponseBodies: true,
  scenarios: {
    open: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '30s',
      preAllocatedVUs: 100,
      maxVUs: 100,
      gracefulStop: '1s',
    },
  },
};

export default function () {
  const response = http.get(TARGET, {
    headers: {
      'X-CollapseLab-Scenario': 'open',
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
