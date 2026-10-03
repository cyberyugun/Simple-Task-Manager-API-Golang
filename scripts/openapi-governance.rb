#!/usr/bin/env ruby
# frozen_string_literal: true
require "yaml"

spec_path = ARGV.fetch(0, "internal/apidocs/openapi.yaml")
main_path = ARGV.fetch(1, "cmd/api/main.go")
spec = YAML.load_file(spec_path)
failures = []
failures << "OpenAPI version must be 3.1.0" unless spec["openapi"] == "3.1.0"
failures << "x-api-version must be v1" unless spec["x-api-version"] == "v1"
failures << "x-stability must be stable" unless spec["x-stability"] == "stable"
failures << "info.version must be semantic version" unless spec.dig("info", "version").to_s.match?(/\A\d+\.\d+\.\d+\z/)

methods = %w[get post put patch delete options head]
operation_ids = {}
spec.fetch("paths", {}).each do |path, item|
  item.each do |method, operation|
    next unless methods.include?(method)
    next unless operation.is_a?(Hash)

    id = operation["operationId"].to_s
    failures << "#{method.upcase} #{path}: missing operationId" if id.empty?
    failures << "duplicate operationId #{id}" if operation_ids.key?(id)
    operation_ids[id] = "#{method.upcase} #{path}"
    failures << "#{id}: missing summary" if operation["summary"].to_s.strip.empty?
    failures << "#{id}: missing tag" if Array(operation["tags"]).empty?

    responses = operation.fetch("responses", {})
    failures << "#{id}: missing 2xx response" unless responses.keys.any? { |code| code.to_s.match?(/\A2\d\d\z/) }

    protected_path = path.start_with?("/api/tasks") || %w[
      /api/auth/change-password
      /api/auth/logout-all
      /api/auth/sessions
      /api/auth/sessions/{id}
      /api/auth/email-verification/request
    ].include?(path)
    if protected_path
      secured = Array(operation["security"]).any? { |s| s.is_a?(Hash) && s.key?("bearerAuth") }
      failures << "#{id}: protected operation must declare bearerAuth" unless secured
    end

    if operation["deprecated"] == true
      failures << "#{id}: deprecated operation requires x-sunset-date" if operation["x-sunset-date"].to_s.empty?
      failures << "#{id}: deprecated operation requires x-replacement" if operation["x-replacement"].to_s.empty?
    end
  end
end

def resolve_ref(spec, ref)
  return nil unless ref.to_s.start_with?("#/")
  ref.delete_prefix("#/").split("/").reduce(spec) { |node, key| node.is_a?(Hash) ? node[key] : nil }
end

def walk_refs(node, spec, failures, trail = "$")
  case node
  when Hash
    failures << "#{trail}: unresolved reference #{node["$ref"]}" if node["$ref"] && resolve_ref(spec, node["$ref"]).nil?
    node.each { |key, value| walk_refs(value, spec, failures, "#{trail}.#{key}") }
  when Array
    node.each_with_index { |value, index| walk_refs(value, spec, failures, "#{trail}[#{index}]") }
  end
end
walk_refs(spec, spec, failures)

registered = File.read(main_path).scan(/mux\.Handle(?:Func)?\("([^"]+)"/).flatten
registered.reject! { |path| ["/docs", "/docs/", "/openapi.yaml"].include?(path) }
registered.each do |route|
  represented = route.end_with?("/") ?
    spec.fetch("paths", {}).keys.any? { |path| path.start_with?(route) } :
    spec.fetch("paths", {}).key?(route)
  failures << "registered route #{route} is missing from OpenAPI" unless represented
end

spec.fetch("paths", {}).keys.each do |path|
  represented = registered.any? { |route| route == path || (route.end_with?("/") && path.start_with?(route)) }
  failures << "OpenAPI path #{path} has no registered route" unless represented
end

if failures.any?
  warn "OpenAPI governance FAILED:"
  failures.each { |failure| warn "- #{failure}" }
  exit 1
end
puts "OpenAPI governance PASSED: #{operation_ids.length} operations, #{spec.fetch("paths", {}).length} paths."
