.PHONY: test config-check compose-check lab-up lab-down lab-smoke

test:
	go test ./...

config-check:
	go test ./internal/cf001 -run TestCanonicalConfigLoads -v

compose-check:
	docker compose config --quiet

lab-up:
	docker compose up -d --build sut prometheus

lab-down:
	docker compose down --remove-orphans -v

lab-smoke: lab-up
	@i=0; until curl --fail --silent --connect-timeout 1 --max-time 2 http://127.0.0.1:18080/healthz >/dev/null; do \
		i=$$((i+1)); [ $$i -ge 30 ] && { echo "SUT readiness timeout" >&2; exit 1; }; sleep 1; \
	done
	@i=0; until curl --fail --silent --connect-timeout 1 --max-time 2 http://127.0.0.1:19090/-/ready >/dev/null; do \
		i=$$((i+1)); [ $$i -ge 30 ] && { echo "Prometheus readiness timeout" >&2; exit 1; }; sleep 1; \
	done
	@curl --fail --silent --connect-timeout 1 --max-time 2 http://127.0.0.1:18080/metrics | grep collapselab_requests_total >/dev/null
