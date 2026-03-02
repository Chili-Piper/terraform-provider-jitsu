terraform {
  required_providers {
    jitsu = {
      source = "chilipiper/jitsu"
    }
  }
}

provider "jitsu" {
  console_url  = "http://localhost:3300"
  auth_token   = "keyId:secret" # or set JITSU_AUTH_TOKEN env var
  database_url = "postgres://reporting:plz_no_hack!@localhost:5432/reporting?sslmode=disable"
}

resource "jitsu_workspace" "main" {
  name = "Jitsu workspace"
  slug = "jitsuworkspace"
}

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

resource "jitsu_stream" "chilichat" {
  workspace_id = jitsu_workspace.main.id
  id           = "site-chilichat"
  name         = "chilichat.app"

  public_keys = [{
    id        = "js.chilichat-browser-key"
    plaintext = "js.chilichat-browser-key"
  }]

  private_keys = [{
    id        = "s2s.chilichat-s2s-key"
    plaintext = "s2s.chilichat-s2s-key"
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

resource "jitsu_link" "chilichat_to_clickhouse" {
  workspace_id = jitsu_workspace.main.id
  from_id      = jitsu_stream.chilichat.id
  to_id        = jitsu_destination.clickhouse.id

  mode                = "batch"
  data_layout         = "segment-single-table"
  primary_key         = "tenant_id,timestamp,message_id"
  frequency           = 1
  batch_size          = 10000
  deduplicate         = true
  deduplicate_window  = 31
  schema_freeze       = false
  timestamp_column    = "timestamp"
  keep_original_names = false
  functions           = [jitsu_function.inject_tenant_id.id]
}
