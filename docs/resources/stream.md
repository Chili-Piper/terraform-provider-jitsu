---
page_title: "jitsu_stream Resource - Jitsu"
description: |-
  Manages a Jitsu stream (event source).
---

# jitsu_stream (Resource)

Manages a Jitsu stream (event source). Keys are set via a two-step create (POST) then update (PUT).

## Example Usage

```hcl
resource "jitsu_stream" "website" {
  workspace_id = jitsu_workspace.main.id
  id           = "site-website"
  name         = "Website"

  public_keys = [{
    id        = "js.browser-key"
    plaintext = "js.browser-key"
  }]

  private_keys = [{
    id        = "s2s.server-key"
    plaintext = "s2s.server-key"
  }]
}
```

## Schema

### Required

- `workspace_id` (String) - Jitsu workspace ID. Changing this forces a new resource.
- `id` (String) - Stream ID. Changing this forces a new resource.
- `name` (String) - Display name of the stream.

### Optional

- `public_keys` (List of Object) - Public (browser) write keys. Each object has:
  - `id` (String, Required) - Key identifier.
  - `plaintext` (String, Required, Sensitive) - Plaintext key value. Write-only; API returns hashed value on read.
- `private_keys` (List of Object) - Private (server-to-server) write keys. Same schema as `public_keys`.

## Import

Import using `workspace_id/stream_id`:

```shell
terraform import jitsu_stream.example <workspace_id>/<stream_id>
```

~> **Note:** Keys are not available on import because the API returns hashed values. You will need to set them in your configuration after import.
