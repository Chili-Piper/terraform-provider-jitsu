---
page_title: "jitsu_function Resource - Jitsu"
description: |-
  Manages a Jitsu function (JavaScript transformation).
---

# jitsu_function (Resource)

Manages a Jitsu function. The ID must be a valid JavaScript identifier (use underscores, not hyphens).

## Example Usage

```hcl
resource "jitsu_function" "inject_tenant_id" {
  workspace_id = jitsu_workspace.main.id
  id           = "inject_tenant_id"
  name         = "Inject Tenant ID"
  code         = <<-JS
    export default async function(event, ctx) {
      event.properties = event.properties || {};
      event.properties.tenant_id = ctx.source.name || "unknown";
      return event;
    }
  JS
}
```

## Schema

### Required

- `workspace_id` (String) - Jitsu workspace ID. Changing this forces a new resource.
- `id` (String) - Function ID. Must be a valid JS identifier (no hyphens). Changing this forces a new resource.
- `name` (String) - Display name of the function.
- `code` (String) - JavaScript function code.

## Import

Import using `workspace_id/function_id`:

```shell
terraform import jitsu_function.example <workspace_id>/<function_id>
```
