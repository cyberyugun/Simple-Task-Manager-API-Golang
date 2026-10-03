#!/usr/bin/env ruby
# frozen_string_literal: true
require "yaml"

spec = YAML.load_file(ARGV.fetch(0, "internal/apidocs/openapi.yaml"))
contract = YAML.load_file(ARGV.fetch(1, "tests/contracts/consumer-v1.yaml"))
failures = []

def resolve_ref(spec, node)
  while node.is_a?(Hash) && node["$ref"]
    node = node["$ref"].delete_prefix("#/").split("/").reduce(spec) { |current, key| current.fetch(key) }
  end
  node
end

def operation_by_id(spec, id)
  spec.fetch("paths", {}).each do |path, item|
    item.each do |method, op|
      next unless %w[get post put patch delete options head].include?(method)
      return [path, method, op] if op["operationId"] == id
    end
  end
  nil
end

def field_exists?(spec, schema, path)
  node = resolve_ref(spec, schema)
  path.split(".").each do |segment|
    node = resolve_ref(spec, node)
    return false unless node.is_a?(Hash) && node["type"] == "object"
    props = node.fetch("properties", {})
    return false unless props.key?(segment)
    node = props[segment]
  end
  true
end

failures << "consumer contract api_version must match OpenAPI x-api-version" unless contract["api_version"] == spec["x-api-version"]

Array(contract["expectations"]).each do |expectation|
  id = expectation.fetch("operation_id")
  found = operation_by_id(spec, id)
  unless found
    failures << "#{id}: operation missing from OpenAPI"
    next
  end

  _, _, operation = found
  status = expectation.fetch("success_status").to_s
  response = operation.fetch("responses", {})[status]
  unless response
    failures << "#{id}: expected response #{status} is missing"
    next
  end
  response = resolve_ref(spec, response)
  schema = response.dig("content", "application/json", "schema")
  unless schema
    failures << "#{id}: response #{status} has no application/json schema"
    next
  end

  Array(expectation["required_response_fields"]).each do |field|
    failures << "#{id}: consumer-required response field #{field} is missing" unless field_exists?(spec, schema, field)
  end
end

if failures.any?
  warn "Consumer contract FAILED:"
  failures.each { |failure| warn "- #{failure}" }
  exit 1
end
puts "Consumer contract PASSED for #{Array(contract["expectations"]).length} expectations."
