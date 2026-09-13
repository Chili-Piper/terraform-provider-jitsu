---
page_title: "jitsu_workspace Resource - Jitsu"
description: |-
  Manages a Jitsu workspace.
---

# jitsu_workspace (Resource)

Manages a Jitsu workspace.

Reusing a deleted workspace's slug requires `database_url` and an authenticated owner of that workspace or an administrator. The provider releases the old slug and creates a new workspace; the deleted workspace and its data remain preserved.

## Example Usage

```hcl
resource "jitsu_workspace" "main" {
  name = "My Workspace"
  slug = "my-workspace"
}
```

## Schema

### Required

- `name` (String) - Workspace display name.
- `slug` (String) - Workspace slug.

### Read-Only

- `id` (String) - Workspace ID (assigned by Console).

## Import

Import using the workspace ID or slug:

```shell
terraform import jitsu_workspace.example <workspace_id_or_slug>
```
