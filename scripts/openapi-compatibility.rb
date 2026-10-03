#!/usr/bin/env ruby
# frozen_string_literal: true
require "yaml"

baseline = YAML.unsafe_load_file(ARGV.fetch(0))
current = YAML.unsafe_load_file(ARGV.fetch(1))
failures = []
methods = %w[get post put patch delete options head]

def schema_type(schema)
  return nil unless schema.is_a?(Hash)
  return "ref:" + schema["$ref"].to_s.split("/").last if schema["$ref"]
  [schema["type"], schema["format"]].compact.join(":")
end

baseline.fetch("paths", {}).each do |path, old_item|
  new_item = current.fetch("paths", {})[path]
  unless new_item
    failures << "removed path #{path}"
    next
  end
  old_item.each do |method, old_op|
    next unless methods.include?(method)
    new_op = new_item[method]
    unless new_op
      failures << "removed operation #{method.upcase} #{path}"
      next
    end
    failures << "#{method.upcase} #{path}: operationId changed" if old_op["operationId"] != new_op["operationId"]

    old_op.fetch("responses", {}).each_key do |code|
      failures << "#{old_op["operationId"]}: removed response #{code}" unless new_op.fetch("responses", {}).key?(code)
    end

    if Array(old_op["security"]).empty? && !Array(new_op["security"]).empty?
      failures << "#{old_op["operationId"]}: authentication became required"
    end

    old_params = Array(old_item["parameters"]) + Array(old_op["parameters"])
    new_params = Array(new_item["parameters"]) + Array(new_op["parameters"])
    old_keys = old_params.map { |p| [p["in"], p["name"]] }
    new_keys = new_params.map { |p| [p["in"], p["name"]] }
    (old_keys - new_keys).each { |key| failures << "#{old_op["operationId"]}: removed parameter #{key.join(":")}" }
    new_params.each do |p|
      key = [p["in"], p["name"]]
      failures << "#{old_op["operationId"]}: added required parameter #{key.join(":")}" if !old_keys.include?(key) && p["required"] == true
    end
    if old_op.dig("requestBody", "required") != true && new_op.dig("requestBody", "required") == true
      failures << "#{old_op["operationId"]}: request body became required"
    end
  end
end

old_schemas = baseline.dig("components", "schemas") || {}
new_schemas = current.dig("components", "schemas") || {}
old_schemas.each do |name, old_schema|
  new_schema = new_schemas[name]
  unless new_schema
    failures << "removed schema #{name}"
    next
  end
  failures << "schema #{name}: type changed" if schema_type(old_schema) != schema_type(new_schema)

  old_props = old_schema.fetch("properties", {})
  new_props = new_schema.fetch("properties", {})
  old_props.each do |prop, old_prop|
    new_prop = new_props[prop]
    unless new_prop
      failures << "schema #{name}: removed property #{prop}"
      next
    end
    failures << "schema #{name}.#{prop}: type changed" if schema_type(old_prop) != schema_type(new_prop)
    removed_enum = Array(old_prop["enum"]) - Array(new_prop["enum"])
    failures << "schema #{name}.#{prop}: removed enum values #{removed_enum.join(", ")}" if removed_enum.any?
  end

  added_required = Array(new_schema["required"]) - Array(old_schema["required"])
  added_required.each do |prop|
    failures << "schema #{name}: newly required property #{prop}" unless old_props.key?(prop)
  end
end

if failures.any?
  warn "OpenAPI backward compatibility FAILED:"
  failures.each { |failure| warn "- #{failure}" }
  exit 1
end
puts "OpenAPI backward compatibility PASSED."
