#!/usr/bin/env ruby
# frozen_string_literal: true
require "yaml"

spec = YAML.load_file(ARGV.fetch(0, "internal/apidocs/openapi.yaml"))
output = ARGV.fetch(1, "/tmp/task-manager-api.ts")

def ts_type(schema)
  return "unknown" unless schema.is_a?(Hash)
  return schema["$ref"].split("/").last if schema["$ref"]
  return schema["const"].inspect if schema.key?("const")
  case schema["type"]
  when "string" then "string"
  when "integer", "number" then "number"
  when "boolean" then "boolean"
  when "array" then "Array<#{ts_type(schema["items"] || {})}>"
  when "object"
    required = Array(schema["required"])
    fields = schema.fetch("properties", {}).map do |name, child|
      name + (required.include?(name) ? "" : "?") + ": " + ts_type(child)
    end
    "{ " + fields.join("; ") + " }"
  else "unknown"
  end
end

lines = ["/* Generated from internal/apidocs/openapi.yaml. Do not edit manually. */"]
lines << "export const API_VERSION = #{spec["x-api-version"].inspect} as const;"
lines << ""

(spec.dig("components", "schemas") || {}).sort.each do |name, schema|
  if schema["type"] == "object"
    lines << "export interface #{name} {"
    required = Array(schema["required"])
    schema.fetch("properties", {}).each do |prop, child|
      lines << "  #{prop}#{required.include?(prop) ? "" : "?"}: #{ts_type(child)};"
    end
    lines << "}"
  else
    lines << "export type #{name} = #{ts_type(schema)};"
  end
  lines << ""
end

operations = []
spec.fetch("paths", {}).each do |path, item|
  item.each do |method, op|
    next unless %w[get post put patch delete options head].include?(method)
    operations << [op.fetch("operationId"), method.upcase, path]
  end
end
lines << "export const operations = {"
operations.sort.each { |id, method, path| lines << "  #{id}: { method: #{method.inspect}, path: #{path.inspect} }," }
lines << "} as const;"
lines << "export type OperationId = keyof typeof operations;"
lines << ""
lines << "export class TaskManagerApiClient {"
lines << "  constructor(private readonly baseUrl: string, private readonly accessToken?: string) {}"
lines << "  async request<T>(operationId: OperationId, init: RequestInit = {}, pathParams: Record<string, string | number> = {}): Promise<T> {"
lines << "    const operation = operations[operationId];"
lines << "    let path = operation.path;"
lines << "    for (const [name, value] of Object.entries(pathParams)) path = path.replace('{' + name + '}', encodeURIComponent(String(value)));"
lines << "    const headers = new Headers(init.headers);"
lines << "    headers.set('Accept', 'application/json');"
lines << "    if (this.accessToken) headers.set('Authorization', 'Bearer ' + this.accessToken);"
lines << "    const response = await fetch(new URL(path, this.baseUrl), { ...init, method: operation.method, headers });"
lines << "    if (!response.ok) throw new Error('API request failed: ' + response.status);"
lines << "    return response.json() as Promise<T>;"
lines << "  }"
lines << "}"
File.write(output, lines.join("\n") + "\n")
puts "Generated TypeScript API client: #{output} (#{operations.length} operations)"
