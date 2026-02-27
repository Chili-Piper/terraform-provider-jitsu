---
page_title: "Jitsu Provider"
description: |-
  The Jitsu provider manages configuration objects in a Jitsu data ingestion platform instance.
---

# Jitsu Provider

The Jitsu provider allows you to manage [Jitsu](https://jitsu.com/) configuration objects — workspaces, streams, destinations, functions, and links — via the Jitsu Console API and direct PostgreSQL access.

## Important: Soft-Delete Behavior

Jitsu uses soft-delete for most operations. When recreating resources with the same ID, the provider must hard-delete soft-deleted rows from the database. This requires `database_url` to be configured. Without it, recreating resources with the same ID will fail due to unique constraint errors.

## Example Usage

```hcl
provider "jitsu" {
  console_url  = "https://jitsu.example.com"
  auth_token   = "keyId:secret"
  database_url = "postgres://user:pass@host:5432/dbname?sslmode=require"
}
```

## Authentication

The provider authenticates using bearer token authentication (`Authorization: Bearer <token>`) against the Jitsu Console API. The token must be a user API key (format: `keyId:secret`). All configuration values can be set via environment variables.

## Schema

### Optional

- `console_url` (String) - Jitsu Console URL. Can also be set via `JITSU_CONSOLE_URL` env var.
- `auth_token` (String, Sensitive) - Bearer token for Jitsu Console API authentication. Must be a user API key (format: `keyId:secret`). Can also be set via `JITSU_AUTH_TOKEN` env var.
- `database_url` (String, Sensitive) - PostgreSQL connection string for Console's database. Required to handle destroy+recreate (Jitsu uses soft-delete; this allows the provider to hard-delete stale rows). Can also be set via `JITSU_DATABASE_URL` env var.
