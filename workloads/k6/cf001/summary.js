function metric(data, name) {
  if (!data || !data.metrics) {
    return null;
  }

  return data.metrics[name] || null;
}

function metricValues(data, name) {
  const selected = metric(data, name);
  return selected && selected.values ? selected.values : null;
}

function counterEvidence(data, name) {
  const selected = metric(data, name);
  const count =
    selected &&
    selected.values &&
    typeof selected.values.count === 'number'
      ? selected.values.count
      : 0;

  return {
    present: selected !== null,
    count,
  };
}

export function encodeSummary(data, scenario) {
  return JSON.stringify(
    {
      schema_version: 1,
      scenario,
      signals: {
        dropped_iterations: counterEvidence(data, 'dropped_iterations'),
        http_req_failed: metricValues(data, 'http_req_failed'),
        checks: metricValues(data, 'checks'),
        work_latency: metricValues(data, 'work_latency'),
      },
      k6: data,
    },
    null,
    2,
  );
}
