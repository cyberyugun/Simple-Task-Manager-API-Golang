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

def request(base, method, path, body: nil, token: nil)
  uri = base + path
  klass = Net::HTTP.const_get(method.capitalize)
  req = klass.new(uri)
  req["Content-Type"] = "application/json"
  req["Authorization"] = "Bearer " + token if token
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

res = request(base, "post", "/api/tasks", body: {title: "Contract task", description: "OpenAPI validation"}, token: token)
created = validate_response!(spec, res, "/api/tasks", "post", 201)
id = created.fetch("data").fetch("id")

[
  ["get", "/api/tasks/#{id}", "/api/tasks/{id}", 200],
  ["patch", "/api/tasks/#{id}/complete", "/api/tasks/{id}/complete", 200],
  ["delete", "/api/tasks/#{id}", "/api/tasks/{id}", 200]
].each do |method, real_path, contract_path, status|
  res = request(base, method, real_path, token: token)
  validate_response!(spec, res, contract_path, method, status)
end

puts "Live OpenAPI contract PASSED."
