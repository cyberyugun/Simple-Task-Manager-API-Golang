#!/usr/bin/env ruby
# frozen_string_literal: true
require "json"
require "net/http"
require "uri"
require "yaml"

base = URI(ARGV.fetch(0, "http://127.0.0.1:8080"))
spec_path = ARGV.fetch(1, "internal/apidocs/openapi.yaml")
spec = YAML.unsafe_load_file(spec_path)

def resolve_ref(spec, ref)
  ref.delete_prefix("#/").split("/").reduce(spec) { |node, key| node.fetch(key) }
end

def validate_schema!(value, schema, spec, trail = "$")
  schema = resolve_ref(spec, schema["$ref"]) if schema.is_a?(Hash) && schema["$ref"]
  return unless schema.is_a?(Hash)
  raise "#{trail}: const mismatch" if schema.key?("const") && value != schema["const"]
  case schema["type"]
  when "object"
    raise "#{trail}: expected object" unless value.is_a?(Hash)
    Array(schema["required"]).each { |key| raise "#{trail}: missing #{key}" unless value.key?(key) }
    schema.fetch("properties", {}).each { |key, child| validate_schema!(value[key], child, spec, "#{trail}.#{key}") if value.key?(key) }
  when "array"
    raise "#{trail}: expected array" unless value.is_a?(Array)
    value.each_with_index { |item, i| validate_schema!(item, schema["items"] || {}, spec, "#{trail}[#{i}]") }
  when "string" then raise "#{trail}: expected string" unless value.is_a?(String)
  when "integer" then raise "#{trail}: expected integer" unless value.is_a?(Integer)
  when "number" then raise "#{trail}: expected number" unless value.is_a?(Numeric)
  when "boolean" then raise "#{trail}: expected boolean" unless value == true || value == false
  end
end

def request(base, method, path, body: nil, token: nil, headers: {})
  uri = base + path
  klass = Net::HTTP.const_get(method.capitalize)
  req = klass.new(uri)
  req["Content-Type"] = "application/json"
  req["Authorization"] = "Bearer " + token if token
  headers.each { |key, value| req[key] = value }
  req.body = JSON.generate(body) if body
  Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == "https") { |http| http.request(req) }
end

def response_schema(spec, path, method, status)
  response = spec.fetch("paths").fetch(path).fetch(method.downcase).fetch("responses").fetch(status.to_s)
  response = resolve_ref(spec, response["$ref"]) if response["$ref"]
  response.dig("content", "application/json", "schema")
end

def validate_response!(spec, response, path, method, status)
  raise "#{method} #{path}: expected #{status}, got #{response.code} #{response.body}" unless response.code.to_i == status
  raise "#{method} #{path}: missing X-API-Version" unless response["X-API-Version"] == "v1"
  data = JSON.parse(response.body)
  schema = response_schema(spec, path, method, status)
  validate_schema!(data, schema, spec) if schema
  data
end

served = request(base, "get", "/openapi.yaml")
raise "served OpenAPI differs from repository contract" unless served.body == File.read(spec_path)
raise "OpenAPI endpoint missing X-API-Version" unless served["X-API-Version"] == "v1"

%w[/health /ready /version].each do |path|
  res = request(base, "get", path)
  validate_response!(spec, res, path, "get", 200)
end

email = "contract-#{Time.now.to_i}-#{Process.pid}@example.com"
res = request(base, "post", "/api/auth/register", body: {name: "Contract User", email: email, password: "contract-password-123"})
registered = validate_response!(spec, res, "/api/auth/register", "post", 201)
token = registered.fetch("data").fetch("access_token")

res = request(base, "get", "/api/tasks", token: token)
validate_response!(spec, res, "/api/tasks", "get", 200)

idempotency_headers = {"X-Idempotency-Key" => "contract-create-#{Time.now.to_i}-#{Process.pid}"}
task_body = {title: "Contract task", description: "OpenAPI validation"}
res = request(base, "post", "/api/tasks", body: task_body, token: token, headers: idempotency_headers)
created = validate_response!(spec, res, "/api/tasks", "post", 201)
id = created.fetch("data").fetch("id")

replayed = request(base, "post", "/api/tasks", body: task_body, token: token, headers: idempotency_headers)
replayed_body = validate_response!(spec, replayed, "/api/tasks", "post", 201)
raise "idempotent replay did not advertise replay" unless replayed["Idempotent-Replayed"] == "true"
raise "idempotent replay created a different task" unless replayed_body.fetch("data").fetch("id") == id

conflict = request(
  base, "post", "/api/tasks",
  body: {title: "Different request"},
  token: token,
  headers: idempotency_headers
)
raise "idempotency hash conflict should return 409, got #{conflict.code}" unless conflict.code.to_i == 409

[
  ["get", "/api/tasks/#{id}", "/api/tasks/{id}", 200],
  ["patch", "/api/tasks/#{id}/complete", "/api/tasks/{id}/complete", 200],
  ["delete", "/api/tasks/#{id}", "/api/tasks/{id}", 200]
].each do |method, real_path, contract_path, status|
  res = request(base, method, real_path, token: token)
  validate_response!(spec, res, contract_path, method, status)
end

member_email = "member-#{Time.now.to_i}-#{Process.pid}@example.com"
res = request(base, "post", "/api/auth/register", body: {name: "Workspace Member", email: member_email, password: "contract-password-123"})
member_registered = validate_response!(spec, res, "/api/auth/register", "post", 201)
member_token = member_registered.fetch("data").fetch("access_token")

outsider_email = "outsider-#{Time.now.to_i}-#{Process.pid}@example.com"
res = request(base, "post", "/api/auth/register", body: {name: "Workspace Outsider", email: outsider_email, password: "contract-password-123"})
outsider_registered = validate_response!(spec, res, "/api/auth/register", "post", 201)
outsider_token = outsider_registered.fetch("data").fetch("access_token")

res = request(base, "post", "/api/workspaces", body: {name: "Contract Workspace"}, token: token)
workspace = validate_response!(spec, res, "/api/workspaces", "post", 201)
workspace_id = workspace.fetch("data").fetch("id")

res = request(base, "post", "/api/workspaces/#{workspace_id}/members", body: {email: member_email, role: "member"}, token: token)
validate_response!(spec, res, "/api/workspaces/{id}/members", "post", 201)

res = request(base, "get", "/api/workspaces/#{workspace_id}", token: member_token)
validate_response!(spec, res, "/api/workspaces/{id}", "get", 200)

res = request(base, "get", "/api/workspaces/#{workspace_id}", token: outsider_token)
raise "cross-tenant workspace access should be hidden with 404, got #{res.code}" unless res.code.to_i == 404

res = request(
  base,
  "post",
  "/api/tasks",
  body: {title: "Shared contract task", description: "tenant isolation"},
  token: member_token,
  headers: {"X-Workspace-ID" => workspace_id.to_s}
)
shared_task = validate_response!(spec, res, "/api/tasks", "post", 201)
raise "shared task workspace mismatch" unless shared_task.fetch("data").fetch("workspace_id") == workspace_id

res = request(base, "get", "/api/tasks", token: token)
personal_page = validate_response!(spec, res, "/api/tasks", "get", 200)
raise "shared task leaked into owner's personal workspace" unless personal_page.fetch("data").fetch("items").empty?

res = request(base, "get", "/api/tasks", token: token, headers: {"X-Workspace-ID" => workspace_id.to_s})
shared_page = validate_response!(spec, res, "/api/tasks", "get", 200)
raise "owner cannot see shared workspace task" unless shared_page.fetch("data").fetch("items").any? { |item| item["id"] == shared_task.fetch("data").fetch("id") }

res = request(base, "get", "/api/workspaces/#{workspace_id}/audit", token: member_token)
raise "member audit access should be forbidden, got #{res.code}" unless res.code.to_i == 403

res = request(base, "get", "/api/workspaces/#{workspace_id}/audit", token: token)
validate_response!(spec, res, "/api/workspaces/{id}/audit", "get", 200)

res = request(
  base,
  "post",
  "/api/workspaces/#{workspace_id}/webhooks",
  body: {url: "https://webhook.example.invalid/task-hook", event_types: ["task.created", "task.updated"]},
  token: token
)
webhook = validate_response!(spec, res, "/api/workspaces/{id}/webhooks", "post", 201)
subscription_id = webhook.fetch("data").fetch("id")
raise "webhook signing secret missing from creation response" if webhook.fetch("data").fetch("signing_secret", "").empty?

res = request(base, "get", "/api/workspaces/#{workspace_id}/webhooks", token: token)
listed = validate_response!(spec, res, "/api/workspaces/{id}/webhooks", "get", 200)
listed_item = listed.fetch("data").find { |item| item["id"] == subscription_id }
raise "created webhook missing from list" unless listed_item
raise "webhook signing secret leaked in list" if listed_item.key?("signing_secret")

res = request(base, "delete", "/api/workspaces/#{workspace_id}/webhooks/#{subscription_id}", token: token)
validate_response!(spec, res, "/api/workspaces/{id}/webhooks/{subscription_id}", "delete", 200)

puts "Live OpenAPI contract PASSED, including RBAC, idempotency, and webhook management."
