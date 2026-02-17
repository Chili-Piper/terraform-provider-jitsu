default: build

build:
	go build -o terraform-provider-jitsu

install: build
	@echo "Binary built. Use dev_overrides in ~/.tofurc or ~/.terraformrc pointing to $(CURDIR)"

test:
	go test ./... -v

testacc:
	TF_ACC=1 go test ./... -v -timeout 120s

.PHONY: build install test testacc
