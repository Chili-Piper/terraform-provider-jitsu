default: build

build:
	go build -o terraform-provider-jitsu

install: build
	@echo "Binary built. Use dev_overrides in ~/.tofurc or ~/.terraformrc pointing to $(CURDIR)"

test:
	go test ./... -v

testacc: export JITSU_CONSOLE_URL ?= http://localhost:3300
testacc: export JITSU_AUTH_TOKEN ?= tfacc:workbench-only-key
testacc: export JITSU_DATABASE_URL ?= postgres://reporting:plz_no_hack!@localhost:5432/reporting?sslmode=disable
testacc:
	TF_ACC=1 go test ./... -v -count=1 -timeout 600s

workbench-up:
	docker compose -f workbench/docker-compose.yaml up --detach --wait
	docker compose -f workbench/docker-compose.yaml exec -T postgres psql -v ON_ERROR_STOP=1 -U reporting -d reporting < workbench/seed.sql

workbench-down:
	docker compose -f workbench/docker-compose.yaml down --volumes

.PHONY: build install test testacc workbench-up workbench-down
