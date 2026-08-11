import { createHash } from 'node:crypto';

const PLAN_SCHEMA = 'pippit-canvas-plan/0.1';
const SNAPSHOT_SCHEMA = 'xyq-libtv-snapshot/0.1';
const SUPPORTED_NODE_TYPES = new Set(['group', 'video', 'audio', 'video-clip']);

function canonicalize(value) {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(
    Object.keys(value)
      .sort()
      .map((key) => [key, canonicalize(value[key])]),
  );
}

function sha256Json(value) {
  return createHash('sha256').update(JSON.stringify(canonicalize(value))).digest('hex');
}

function nonEmptyString(value) {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined;
}

function finiteNumber(value, field) {
  const number = Number(value);
  if (!Number.isFinite(number)) throw new Error(`${field} must be a finite number`);
  return number;
}

function positiveNumber(value, field) {
  const number = finiteNumber(value, field);
  if (number <= 0) throw new Error(`${field} must be greater than zero`);
  return number;
}

function compact(value) {
  return Object.fromEntries(Object.entries(value).filter(([, child]) => child !== undefined));
}

function detailDataByNodeId(snapshot) {
  const result = new Map();
  for (const item of snapshot.nodeDetails ?? []) {
    const sourceNodeId = nonEmptyString(item?.sourceNodeId);
    if (!sourceNodeId || result.has(sourceNodeId)) continue;
    const data = item?.detail?.data;
    result.set(sourceNodeId, data && typeof data === 'object' && !Array.isArray(data) ? data : {});
  }
  return result;
}

function mediaManifestByNodeId(mediaManifest) {
  const result = new Map();
  for (const item of mediaManifest?.uploads ?? mediaManifest?.media ?? []) {
    const sourceNodeId = nonEmptyString(item?.sourceNodeId ?? item?.source_node_id);
    if (!sourceNodeId || result.has(sourceNodeId)) continue;
    result.set(sourceNodeId, {
      fileName: nonEmptyString(item?.fileName ?? item?.file_name ?? item?.path),
      url: normalizeHTTPSURL(item?.url),
    });
  }
  return result;
}

function assetReferenceByNodeId(snapshot) {
  const result = new Map();
  for (const item of snapshot.assetReferences ?? []) {
    const sourceNodeId = nonEmptyString(item?.sourceNodeId);
    const url = normalizeHTTPSURL(item?.url);
    if (sourceNodeId && url && !result.has(sourceNodeId)) result.set(sourceNodeId, url);
  }
  return result;
}

function normalizeHTTPSURL(value) {
  const raw = Array.isArray(value) ? value.find((item) => nonEmptyString(item)) : value;
  const text = nonEmptyString(raw);
  if (!text) return undefined;
  let url;
  try {
    url = new URL(text);
  } catch {
    throw new Error(`invalid media URL: ${text.slice(0, 120)}`);
  }
  if (url.protocol !== 'https:') throw new Error(`media URL must use HTTPS: ${url.protocol}`);
  return url.toString();
}

function fileNameFromURL(value) {
  if (!value) return undefined;
  const pathname = new URL(value).pathname;
  const encodedName = pathname.slice(pathname.lastIndexOf('/') + 1);
  if (!encodedName) return undefined;
  try {
    return decodeURIComponent(encodedName);
  } catch {
    return encodedName;
  }
}

function safeFileName(value) {
  const text = nonEmptyString(value);
  if (!text) return undefined;
  const normalized = text.replaceAll('\\', '/');
  return normalized.slice(normalized.lastIndexOf('/') + 1) || undefined;
}

function mediaMetadata(data) {
  const item = data?.resourceMeta?.items?.[0] ?? {};
  const duration = Number(item.durationSec);
  return compact({
    byte_size: Number(item.byteSize) > 0 ? Number(item.byteSize) : undefined,
    duration_ms: Number.isFinite(duration) && duration > 0 ? Math.round(duration * 1000) : undefined,
    extension: nonEmptyString(item.extension)?.toLowerCase(),
    height: Number(item.height) > 0 ? Number(item.height) : undefined,
    mime_type: nonEmptyString(item.mimeType),
    width: Number(item.width) > 0 ? Number(item.width) : undefined,
  });
}

function fallbackFileName(node, metadata) {
  const extension = metadata.extension ?? (node.type === 'audio' ? 'audio' : 'video');
  const stem = (nonEmptyString(node.name) ?? node.id)
    .replaceAll(/[\\/:*?"<>|]/g, '_')
    .slice(0, 120);
  return `${stem}.${extension}`;
}

function logicalNodeId(sourceNodeId) {
  return `node:${sourceNodeId}`;
}

function logicalGroupId(sourceNodeId) {
  return `group:${sourceNodeId}`;
}

function logicalMediaId(sourceNodeId) {
  return `media:${sourceNodeId}`;
}

function logicalEdgeId(sourceEdgeId) {
  return `edge:${sourceEdgeId}`;
}

function validateSnapshot(snapshot) {
  if (snapshot?.protocolVersion !== SNAPSHOT_SCHEMA) {
    throw new Error(`unsupported LibTV snapshot schema: ${snapshot?.protocolVersion ?? '<missing>'}`);
  }
  const projectId = nonEmptyString(snapshot?.project?.projectUuid);
  if (!projectId) throw new Error('snapshot project.projectUuid is required');
  const nodes = snapshot?.project?.nodes;
  if (!Array.isArray(nodes) || nodes.length === 0) throw new Error('snapshot project.nodes must not be empty');
  if (!Array.isArray(snapshot?.project?.edges)) throw new Error('snapshot project.edges must be an array');

  const nodeById = new Map();
  nodes.forEach((node, index) => {
    const id = nonEmptyString(node?.id);
    if (!id) throw new Error(`snapshot node ${index} has no id`);
    if (nodeById.has(id)) throw new Error(`duplicate snapshot node id: ${id}`);
    if (!SUPPORTED_NODE_TYPES.has(node?.type)) throw new Error(`unsupported LibTV node type: ${node?.type ?? '<missing>'}`);
    finiteNumber(node?.position?.x, `node ${id} position.x`);
    finiteNumber(node?.position?.y, `node ${id} position.y`);
    positiveNumber(node?.width, `node ${id} width`);
    positiveNumber(node?.height, `node ${id} height`);
    nodeById.set(id, node);
  });

  for (const node of nodes) {
    const parentId = nonEmptyString(node?.parentId);
    if (parentId && nodeById.get(parentId)?.type !== 'group') {
      throw new Error(`node ${node.id} references a missing or non-group parent ${parentId}`);
    }
  }

  const edgeIds = new Set();
  snapshot.project.edges.forEach((edge, index) => {
    const id = nonEmptyString(edge?.id);
    if (!id) throw new Error(`snapshot edge ${index} has no id`);
    if (edgeIds.has(id)) throw new Error(`duplicate snapshot edge id: ${id}`);
    edgeIds.add(id);
    if (!nodeById.has(edge?.source) || !nodeById.has(edge?.target)) {
      throw new Error(`edge ${id} references a missing node`);
    }
    if (nodeById.get(edge.source).type === 'group' || nodeById.get(edge.target).type === 'group') {
      throw new Error(`edge ${id} must connect business nodes, not groups`);
    }
  });
  return { projectId, nodes, nodeById };
}

function videoCompositeInputs(data, nodeById) {
  const preferred = Array.isArray(data?.params?.videoList)
    ? data.params.videoList.map((item) => item?.nodeId)
    : [];
  const fallback = Array.isArray(data?.clipTimelineData?.videoSourceNodeIds)
    ? data.clipTimelineData.videoSourceNodeIds
    : [];
  const chosen = preferred.some(Boolean) ? preferred : fallback;
  const unique = [];
  const seen = new Set();
  for (const sourceId of chosen) {
    if (!nonEmptyString(sourceId) || seen.has(sourceId) || nodeById.get(sourceId)?.type !== 'video') continue;
    seen.add(sourceId);
    unique.push(logicalNodeId(sourceId));
  }
  return unique;
}

function sourceFingerprint(snapshot) {
  return `sha256:${sha256Json({
    protocolVersion: snapshot.protocolVersion,
    project: snapshot.project,
    nodeDetails: snapshot.nodeDetails ?? [],
    assetReferences: snapshot.assetReferences ?? [],
  })}`;
}

function canvasTitle(snapshot, projectId, override) {
  const title = nonEmptyString(override) ?? nonEmptyString(snapshot.project.name) ?? `LibTV · ${projectId}`;
  if (Array.from(title).length > 50) {
    throw new Error('canvas title must not exceed 50 characters; provide a shorter --title');
  }
  return title;
}

function convertSnapshotToCanvasPlan(snapshot, options = {}) {
  const { projectId, nodes: sourceNodes, nodeById } = validateSnapshot(snapshot);
  const details = detailDataByNodeId(snapshot);
  const manifest = mediaManifestByNodeId(options.mediaManifest);
  const assetReferences = assetReferenceByNodeId(snapshot);
  const requiredMedia = [];
  const nodes = [];
  const groups = [];
  const degradations = [];

  sourceNodes.forEach((sourceNode, order) => {
    const sourceNodeId = sourceNode.id;
    const position = {
      x: finiteNumber(sourceNode.position.x, `node ${sourceNodeId} position.x`),
      y: finiteNumber(sourceNode.position.y, `node ${sourceNodeId} position.y`),
    };
    const size = {
      width: positiveNumber(sourceNode.width, `node ${sourceNodeId} width`),
      height: positiveNumber(sourceNode.height, `node ${sourceNodeId} height`),
    };
    if (sourceNode.type === 'group') {
      const children = sourceNodes
        .filter((child) => child.parentId === sourceNodeId)
        .map((child) => child.type === 'group' ? logicalGroupId(child.id) : logicalNodeId(child.id));
      groups.push({
        logical_id: logicalGroupId(sourceNodeId),
        source_node_id: sourceNodeId,
        title: nonEmptyString(sourceNode.name) ?? 'LibTV group',
        position,
        size,
        order,
        child_logical_ids: children,
      });
      return;
    }

    const detail = details.get(sourceNodeId) ?? {};
    const base = compact({
      logical_id: logicalNodeId(sourceNodeId),
      source_node_id: sourceNodeId,
      title: nonEmptyString(sourceNode.name) ?? sourceNodeId,
      position,
      size,
      parent_group_logical_id: sourceNode.parentId ? logicalGroupId(sourceNode.parentId) : undefined,
      order,
    });
    if (sourceNode.type === 'video-clip') {
      const inputNodeLogicalIds = videoCompositeInputs(detail, nodeById);
      nodes.push({
        ...base,
        kind: 'video-composite',
        target_type: 'biz/video',
        variant: 'video-composite',
        input_node_logical_ids: inputNodeLogicalIds,
      });
      degradations.push({
        code: 'libtv.video_clip.empty_placeholder',
        source_node_id: sourceNodeId,
        message: 'LibTV video-clip has no portable generated result and will become an empty video-composite placeholder.',
        input_node_logical_ids: inputNodeLogicalIds,
      });
      return;
    }

    const mediaType = sourceNode.type;
    const manifestItem = manifest.get(sourceNodeId) ?? {};
    const url = manifestItem.url ?? assetReferences.get(sourceNodeId) ?? normalizeHTTPSURL(detail?.url);
    const metadata = mediaMetadata(detail);
    const fileName = safeFileName(manifestItem.fileName) ?? safeFileName(fileNameFromURL(url)) ?? fallbackFileName(sourceNode, metadata);
    const mediaLogicalId = logicalMediaId(sourceNodeId);
    requiredMedia.push(compact({
      logical_id: mediaLogicalId,
      source_node_id: sourceNodeId,
      file_name: fileName,
      media_type: mediaType,
      url,
      metadata,
    }));
    nodes.push({
      ...base,
      kind: mediaType,
      target_type: mediaType === 'audio' ? 'biz/audio' : 'biz/video',
      media_logical_id: mediaLogicalId,
    });
  });

  const edges = snapshot.project.edges.map((edge) => ({
    logical_id: logicalEdgeId(edge.id),
    source_edge_id: edge.id,
    type: 'reference',
    source_node_logical_id: logicalNodeId(edge.source),
    target_node_logical_id: logicalNodeId(edge.target),
    source_handle: 'right',
    target_handle: 'left',
  }));

  return {
    schema: PLAN_SCHEMA,
    title: canvasTitle(snapshot, projectId, options.title),
    source: {
      provider: 'libtv',
      project_id: projectId,
      fingerprint: sourceFingerprint(snapshot),
    },
    required_media: requiredMedia,
    nodes,
    groups,
    edges,
    degradations,
  };
}

export {
  PLAN_SCHEMA,
  SNAPSHOT_SCHEMA,
  convertSnapshotToCanvasPlan,
  sha256Json,
};
