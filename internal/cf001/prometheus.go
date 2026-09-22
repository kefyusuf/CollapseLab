package cf001

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

func ParsePrometheusMatrix(data []byte) ([]PromSeries, error) {
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Values [][]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w", err)
	}
	if envelope.Status != "success" {
		return nil, fmt.Errorf("prometheus response status must be success")
	}
	if envelope.Data.ResultType != "matrix" {
		return nil, fmt.Errorf("prometheus resultType must be matrix")
	}

	series := make([]PromSeries, 0, len(envelope.Data.Result))
	for seriesIndex, rawSeries := range envelope.Data.Result {
		current := PromSeries{
			Labels:  cloneLabels(rawSeries.Metric),
			Samples: make([]MetricPoint, 0, len(rawSeries.Values)),
		}

		var previous time.Time
		for sampleIndex, rawSample := range rawSeries.Values {
			if len(rawSample) != 2 {
				return nil, fmt.Errorf("prometheus series %d sample %d must have timestamp and value", seriesIndex, sampleIndex)
			}

			var timestamp float64
			if err := json.Unmarshal(rawSample[0], &timestamp); err != nil {
				return nil, fmt.Errorf("decode prometheus timestamp: %w", err)
			}
			if math.IsNaN(timestamp) || math.IsInf(timestamp, 0) {
				return nil, fmt.Errorf("prometheus timestamp must be finite")
			}

			var valueText string
			if err := json.Unmarshal(rawSample[1], &valueText); err != nil {
				return nil, fmt.Errorf("decode prometheus sample value: %w", err)
			}
			value, err := strconv.ParseFloat(valueText, 64)
			if err != nil {
				return nil, fmt.Errorf("parse prometheus sample value %q: %w", valueText, err)
			}
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("prometheus sample value must be non-finite")
			}

			at := unixFloatTime(timestamp)
			if !previous.IsZero() && !at.After(previous) {
				return nil, fmt.Errorf("prometheus samples must be strictly increasing")
			}
			previous = at
			current.Samples = append(current.Samples, MetricPoint{At: at, Value: value})
		}

		series = append(series, current)
	}

	return series, nil
}

func unixFloatTime(timestamp float64) time.Time {
	seconds, fraction := math.Modf(timestamp)
	nanoseconds := int64(math.Round(fraction * float64(time.Second)))
	return time.Unix(int64(seconds), nanoseconds).UTC()
}

func cloneLabels(labels map[string]string) map[string]string {
	result := make(map[string]string, len(labels))
	for key, value := range labels {
		result[key] = value
	}
	return result
}
