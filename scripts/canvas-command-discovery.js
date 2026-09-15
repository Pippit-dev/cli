"use strict";

// These are views over the SDK contract. Only `schema` exports a complete schema.
const SCHEMA_VERSION = 2;
const PREFIX = "pippit-tool-cli canvas command";
const LIMITS = { list: 16 * 1024, describe: 32 * 1024 };

function category(name) {
  if (name.startsWith("xyq.")) return name.split(".")[1];
  if (name.includes("checkpoint")) return "checkpoint";
  if (name.includes("asset")) return "asset";
  return "canvas";
}

function summary(description = "") {
  const first = description.replace(/\s+/g, " ").split(/(?<=[.!?。])\s+/)[0];
  return first.length > 160 ? `${first.slice(0, 159)}…` : first;
}

function commandIndex(entry) {
  return { name: entry.name, category: category(entry.name), summary: summary(entry.description) };
}

function listCatalog(entries, selectedCategory) {
  const categories = [...new Set(entries.map((entry) => category(entry.name)))].sort((a, b) =>
    a.localeCompare(b, "en"),
  );
  if (selectedCategory && !categories.includes(selectedCategory)) {
    throw new Error(`未知分类：${selectedCategory}；可选：${categories.join(", ")}`);
  }
  return {
    schema_version: SCHEMA_VERSION,
    categories,
    commands: entries
      .filter((entry) => !selectedCategory || category(entry.name) === selectedCategory)
      .map(commandIndex),
    next: {
      describe: `${PREFIX} describe <command>`,
      guides: `${PREFIX} guide`,
      full_schema: `${PREFIX} schema [command]`,
    },
  };
}

function operationCatalog(entry) {
  const field =
    entry.name === "xyq.timeline.apply" ? "commands" : entry.name === "xyq.scene3d.apply" ? "operations" : undefined;
  if (!field) return undefined;
  const discriminator = field === "commands" ? "type" : "command";
  const variants = entry.input_schema.properties[field].items.oneOf;
  return { field, variants, discriminator };
}

function selectOperation(schema, catalog, name) {
  const selected = catalog?.variants.find((variant) => variant.properties?.[catalog.discriminator]?.const === name);
  if (!selected) throw new Error(`未知或不适用的 operation：${name}；请先 describe 此命令查看 operations`);
  const result = {
    ...schema,
    properties: { ...schema.properties, [catalog.field]: { ...schema.properties[catalog.field], items: selected } },
  };
  if (schema.examples)
    result.examples = schema.examples.filter((example) =>
      example[catalog.field]?.every((operation) => operation[catalog.discriminator] === name),
    );
  return result;
}

function selectNodeKind(schema, name) {
  const kinds = schema.properties?.nodeKind?.enum;
  if (!kinds?.includes(name)) throw new Error(`未知或不适用的 node-kind：${name}；请先 describe create_biz_node`);
  const matching = (schema.allOf || []).filter((branch) => branch.if?.properties?.nodeKind?.const === name);
  const remaining = (schema.allOf || []).filter((branch) => !branch.if?.properties?.nodeKind);
  let result = {
    ...schema,
    properties: { ...schema.properties, nodeKind: { ...schema.properties.nodeKind, const: name } },
  };
  delete result.properties.nodeKind.enum;
  delete result.allOf;
  if (schema.examples) result.examples = schema.examples.filter((example) => example.nodeKind === name);
  if (remaining.length) result.allOf = remaining;
  for (const { then: branch } of matching) {
    result = {
      ...result,
      ...branch,
      properties: { ...result.properties, ...branch.properties },
      required: [...new Set([...(result.required || []), ...(branch.required || [])])],
    };
  }
  return result;
}

function resolveSchemaPath(schema, schemaPath) {
  if (!schemaPath) return schema;
  let value = schema;
  for (const key of schemaPath.split(".")) {
    if (!key || !value || typeof value !== "object" || !Object.prototype.hasOwnProperty.call(value, key)) {
      throw new Error(`不存在的 schema 路径：${schemaPath}；使用 describe 输出中的 schema_path`);
    }
    value = value[key];
  }
  return value;
}

function childPath(parent, key) {
  return parent ? `${parent}.${key}` : key;
}

function branchSummary(branch, schemaPath) {
  if (!branch || typeof branch !== "object") return branch;
  const selectors = Object.fromEntries(
    Object.entries(branch.properties || branch.if?.properties || {})
      .filter(([, value]) => value.const !== undefined || value.enum)
      .map(([name, value]) => [name, value.const ?? value.enum]),
  );
  return {
    schema_path: schemaPath,
    ...(branch.type ? { type: branch.type } : {}),
    ...(branch.description ? { description: summary(branch.description) } : {}),
    ...(Object.keys(selectors).length ? { selectors } : {}),
  };
}

function schemaView(value, schemaPath = "", depth = 1) {
  if (!value || typeof value !== "object") return value;
  if (Array.isArray(value)) return value.map((branch, index) => branchSummary(branch, childPath(schemaPath, index)));
  const output = {};
  for (const [key, child] of Object.entries(value)) {
    const path = childPath(schemaPath, key);
    if (key === "properties" && depth >= 0) {
      output.properties = Object.fromEntries(
        Object.entries(child).map(([name, property]) => [name, schemaView(property, childPath(path, name), depth - 1)]),
      );
    } else if (["oneOf", "anyOf", "allOf"].includes(key)) {
      output[key] = child.map((branch, index) => branchSummary(branch, childPath(path, index)));
    } else if (
      ["items", "additionalProperties", "if", "then", "else", "not"].includes(key) &&
      typeof child === "object"
    ) {
      output[key] = depth > 0 ? schemaView(child, path, depth - 1) : { schema_path: path, expand: true };
    } else if (key === "properties" || key === "$defs" || key === "definitions" || key === "patternProperties") {
      output[key] = { schema_path: path, expand: true };
    } else if (JSON.stringify(child).length > 2048) {
      output[key] = { schema_path: path, expand: true };
    } else {
      output[key] = child;
    }
  }
  return output;
}

function describeEntry(entry, options, entries) {
  const operations = operationCatalog(entry);
  let schema = entry.input_schema;
  if (options.operation) schema = selectOperation(schema, operations, options.operation);
  if (options.nodeKind) schema = selectNodeKind(schema, options.nodeKind);
  const schemaPath = options.schemaPath || "";
  const result = {
    schema_version: SCHEMA_VERSION,
    name: entry.name,
    category: category(entry.name),
    description: entry.description,
    schema_view: "summary",
    schema_note:
      "Display only: nested schemas and conditional branches are indexed by schema_path. Use --path to expand a field; use schema to export the complete validation contract.",
    ...(options.operation ? { operation: options.operation } : {}),
    ...(options.nodeKind ? { node_kind: options.nodeKind } : {}),
    ...(schemaPath ? { schema_path: schemaPath } : {}),
    input_schema: schemaView(resolveSchemaPath(schema, schemaPath), schemaPath),
    schema_export: `${PREFIX} schema ${entry.name}`,
  };
  if (options.operation && !schemaPath) {
    const path = `properties.${operations.field}.items`;
    result.input_schema.properties[operations.field].items = schemaView(resolveSchemaPath(schema, path), path);
  }
  if (!schemaPath && !options.operation && operations) {
    result.operations = operations.variants.map((variant) => ({
      name: variant.properties[operations.discriminator].const,
      summary: summary(variant.description || variant.properties[operations.discriminator].const.replace(/_/g, " ")),
    }));
    result.describe_operation = `${PREFIX} describe ${entry.name} --operation <name>`;
  }
  if (!schemaPath && !options.nodeKind && schema.properties?.nodeKind?.enum) {
    result.node_kinds = schema.properties.nodeKind.enum;
    result.describe_node_kind = `${PREFIX} describe ${entry.name} --node-kind <kind>`;
  }
  if (!schemaPath && (entry.name === "apply_mutations" || entry.name === "invoke_command")) {
    result.related_commands = entries.filter((item) => item.name !== entry.name).map((item) => item.name);
    result.describe_related = `${PREFIX} describe <command>`;
  }
  result.guide = `${PREFIX} guide`;
  return result;
}

function assertOutputBudget(value, action) {
  const limit = LIMITS[action];
  if (limit && Buffer.byteLength(`${JSON.stringify(value, null, 2)}\n`) > limit) {
    throw new Error(`${action} 输出超过 ${limit / 1024} KiB 预算；请使用更具体的筛选或 schema 显式导出`);
  }
  return value;
}

module.exports = { assertOutputBudget, describeEntry, listCatalog, SCHEMA_VERSION };
