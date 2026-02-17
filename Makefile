default: build

build:
	go build -o terraform-provider-jitsu

install: build

test:
	go test ./... -v

testacc:
	TF_ACC=1 go test ./... -v -timeout 120s

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate

.PHONY: build install test testacc docs
