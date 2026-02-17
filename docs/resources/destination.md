---
page_title: "jitsu_destination Resource - Jitsu"
description: |-
  Manages a Jitsu destination (e.g., ClickHouse, PostgreSQL).
---

# jitsu_destination (Resource)

Manages a Jitsu destination (e.g., ClickHouse, PostgreSQL).

## Example Usage

```hcl
resource "jitsu_destination" "clickhouse" {
  workspace_id     = jitsu_workspace.main.id
  id               = "dest-clickhouse"
  name             = "ClickHouse"
  destination_type = "clickhouse"
  protocol         = "https"
  hosts            = ["clickhouse.example.com:8443"]
  username         = "default"
  password         = "changeme"
  database         = "analytics"
}
```

## Schema

### Required

- `workspace_id` (String) - Jitsu workspace ID. Changing this forces a new resource.
- `id` (String) - Destination ID. Changing this forces a new resource.
- `name` (String) - Display name of the destination.
- `destination_type` (String) - Destination type (e.g., `clickhouse`, `postgres`).
- `hosts` (List of String) - List of host:port addresses.

### Optional

- `protocol` (String) - Connection protocol (e.g., `http`, `https`, `tcp`).
- `username` (String) - Database username.
- `password` (String, Sensitive) - Database password. API returns masked value; stored in state from user config.
- `database` (String) - Database name.

## Import

Import using `workspace_id/destination_id`:

```shell
terraform import jitsu_destination.example <workspace_id>/<destination_id>
```

~> **Note:** The password is not available on import because the API returns a masked value.
