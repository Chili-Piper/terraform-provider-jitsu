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
  clickhouse = {
    protocol = "https"
    hosts    = ["clickhouse.example.com:8443"]
    username = "default"
    password = "changeme"
    database = "analytics"
  }
}
```

## Schema

### Required

- `workspace_id` (String) - Jitsu workspace ID. Changing this forces a new resource.
- `id` (String) - Destination ID. Changing this forces a new resource.
- `name` (String) - Display name of the destination.
- `destination_type` (String) - Destination type (e.g., `clickhouse`, `postgres`).
- `clickhouse.hosts` (List of String) - List of host:port addresses, required inside the `clickhouse` object.

### Optional

- `clickhouse.protocol` (String) - Connection protocol. Defaults to `clickhouse-secure`.
- `clickhouse.username` (String) - Database username. Defaults to `default`.
- `clickhouse.password` (String, Sensitive) - Database password. Omission on create uses an empty password. API returns masked value; stored in state from user config.
- `clickhouse.database` (String) - Database name. Defaults to `default`.
- `clickhouse.cluster` (String) - ClickHouse cluster name. Defaults to an empty string (no cluster).

## Import

Import using `workspace_id/destination_id`:

```shell
terraform import jitsu_destination.example <workspace_id>/<destination_id>
```

~> **Note:** The password is not available on import because the API returns a masked value.

Removing `clickhouse.protocol`, `clickhouse.username`, or `clickhouse.database` restores the Console defaults (`clickhouse-secure`, `default`, and `default`). Removing a configured `clickhouse.cluster` clears the cluster; removing a configured `clickhouse.password` resets it to an empty password. An omitted password after import remains unmanaged and is preserved.
