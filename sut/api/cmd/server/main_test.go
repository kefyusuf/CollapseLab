package main

import (
	"testing"
	"time"
)

func TestBaseServiceTimeFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    time.Duration
		wantErr bool
	}{
		{name: "valid", value: "50ms", want: 50 * time.Millisecond},
		{name: "missing", value: "", wantErr: true},
		{name: "invalid", value: "fast", wantErr: true},
		{name: "zero", value: "0s", wantErr: true},
		{name: "negative", value: "-1ms", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := baseServiceTimeFromEnv(func(string) string { return tt.value })
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got duration %s", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("duration = %s, want %s", got, tt.want)
			}
		})
	}
}
