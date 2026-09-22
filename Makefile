.PHONY: test config-check

test:
	go test ./...

config-check:
	go test ./internal/cf001 -run TestCanonicalConfigLoads -v
