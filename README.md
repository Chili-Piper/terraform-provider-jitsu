# Terraform Provider: Jitsu

Terraform/OpenTofu provider for managing Jitsu Console configuration objects.

## Resources

- `jitsu_workspace`
- `jitsu_function`
- `jitsu_destination`
- `jitsu_stream`
- `jitsu_link`

## Requirements

- Go `1.24+` (for local build/development)
- Terraform or OpenTofu
- Running Jitsu Console API

## Installation

### Use from provider source

```hcl
terraform {
  required_providers {
    jitsu = {
      source = "chilipiper/jitsu"
    }
  }
}
```

### Local development install (dev override)

1. Build the provider:

```bash
make build
```

2. Configure `~/.terraformrc` or `~/.tofurc`:

```hcl
provider_installation {
  dev_overrides {
    "chilipiper/jitsu" = "/absolute/path/to/terraform-provider-jitsu"
  }
  direct {}
}
```

3. Initialize your Terraform/OpenTofu project:

```bash
terraform init
# or
tofu init
```

## Authentication

Provider uses bearer token authentication via:

- `auth_token` (or env `JITSU_AUTH_TOKEN`) — a user API key (format: `keyId:secret`)

`console_url` can be set via `JITSU_CONSOLE_URL`.

`database_url` is optional but strongly recommended. Jitsu uses soft-delete; `database_url` allows the provider to hard-delete stale rows during recreate flows.

## Usage

```hcl
terraform {
  required_providers {
    jitsu = {
      source = "chilipiper/jitsu"
    }
  }
}

provider "jitsu" {
  console_url  = "http://localhost:3300"
  auth_token   = "keyId:secret"  # or set JITSU_AUTH_TOKEN env var
  database_url = "postgres://reporting:plz_no_hack!@localhost:5432/reporting?sslmode=disable"
}

resource "jitsu_workspace" "main" {
  name = "Terraform Workspace"
  slug = "terraform-workspace"
}

resource "jitsu_function" "enrich" {
  workspace_id = jitsu_workspace.main.id
  id           = "enrich_event"
  name         = "Enrich Event"
  code         = <<-JS
    export default async function(event) {
      event.properties = event.properties || {};
      event.properties.source = "terraform";
      return event;
    }
  JS
}

resource "jitsu_stream" "site" {
  workspace_id = jitsu_workspace.main.id
  id           = "site-stream"
  name         = "Site Stream"

  public_keys = [{
    id        = "js.site"
    plaintext = "js.site"
  }]
}

resource "jitsu_destination" "clickhouse" {
  workspace_id     = jitsu_workspace.main.id
  id               = "dest-clickhouse"
  name             = "Local ClickHouse"
  destination_type = "clickhouse"
  protocol         = "http"
  hosts            = ["clickhouse:8123"]
  username         = "reporting"
  password         = ""
  database         = "default"
}

resource "jitsu_link" "site_to_clickhouse" {
  workspace_id = jitsu_workspace.main.id
  from_id      = jitsu_stream.site.id
  to_id        = jitsu_destination.clickhouse.id

  mode       = "batch"
  frequency  = 1
  batch_size = 10000
  functions  = [jitsu_function.enrich.id]
}
```

For a fuller local example, see `examples/main.tf`.

## Import formats

- `jitsu_workspace`: `workspace_id_or_slug`
- `jitsu_function`: `workspace_id/function_id`
- `jitsu_destination`: `workspace_id/destination_id`
- `jitsu_stream`: `workspace_id/stream_id`
- `jitsu_link`: `workspace_id/from_id/to_id`

Examples:

```bash
terraform import jitsu_workspace.main <workspace_id_or_slug>
terraform import jitsu_function.enrich <workspace_id>/<function_id>
terraform import jitsu_destination.clickhouse <workspace_id>/<destination_id>
terraform import jitsu_stream.site <workspace_id>/<stream_id>
terraform import jitsu_link.site_to_clickhouse <workspace_id>/<from_id>/<to_id>
```

## Testing

- Unit and resource tests:

```bash
make test
```

- Acceptance tests:

```bash
TF_ACC=1 go test ./internal/provider -v -timeout 300s
# or
make testacc
```

Common environment variables:

- `JITSU_CONSOLE_URL`
- `JITSU_AUTH_TOKEN`
- `JITSU_DATABASE_URL`

Local stack for testing is available at `workbench/docker-compose.yaml`.
