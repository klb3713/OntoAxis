/**
 * 领域类型 —— 前端边界使用的稳定视图模型。
 *
 * 这些类型对应 `proto/ontoaxis/v1` 的领域消息，字段采用 lowerCamelCase。
 * Stage 1 先由本文件承担 typed client 的契约基线；后续 `oa-contracts` change
 * 生成 OpenAPI/TS 客户端后，将其替换为生成类型，本文件的边界适配保持不变。
 */

export type Timestamps = {
  createdAt: string;
  updatedAt: string;
};

export type UserProfile = {
  id: string;
  email: string;
  displayName: string;
  organizationId: string;
  tenantId: string;
  roles: string[];
  locale?: string;
};

export type Project = Timestamps & {
  id: string;
  organizationId: string;
  workspaceId: string;
  name: string;
  displayName: string;
  description: string;
  ownerId: string;
};

export type ProjectInput = {
  name: string;
  displayName: string;
  description?: string;
};

export type PropertyDefinition = {
  id: string;
  objectTypeId: string;
  name: string;
  displayName: string;
  description: string;
  propertyType: string;
  required: boolean;
  unique: boolean;
};

export type ObjectType = Timestamps & {
  id: string;
  name: string;
  displayName: string;
  description: string;
  primaryKeyProperty: string;
  properties: PropertyDefinition[];
};

export type LinkType = Timestamps & {
  id: string;
  name: string;
  displayName: string;
  sourceTypeId: string;
  targetTypeId: string;
};

export type ObjectRecord = Timestamps & {
  id: string;
  objectTypeId: string;
  properties: Record<string, unknown>;
  version: number;
};

export type Dataset = Timestamps & {
  id: string;
  name: string;
  displayName: string;
  description: string;
  ownerId: string;
  format: "ICEBERG" | "PARQUET";
  storageLocation: string;
};

export type DatasetColumn = {
  name: string;
  type: string;
};

export type DatasetPreview = {
  datasetId: string;
  columns: DatasetColumn[];
  rows: Record<string, unknown>[];
  rowCount: number;
};

export type QueryResult = {
  columns: string[];
  rows: unknown[][];
  rowCount: number;
  elapsedMs: number;
};

export type SavedQuery = Timestamps & {
  id: string;
  name: string;
  sql: string;
  ownerId: string;
};

/**
 * 传输层列表形态。后端可能返回 `data`/`items`/<resource> 等多种键，
 * 统一在 `normalize.ts` 适配，页面只消费 `ListPayload`。
 */
export type ListPayload<T> = {
  data: T[];
  meta: {
    page: number;
    perPage: number;
    total: number;
    totalPages: number;
  };
};

export type ApiErrorPayload = {
  error?: {
    code?: string;
    message?: string;
    metadata?: Record<string, unknown>;
    retryable?: boolean;
    request_id?: string;
  };
  code?: string;
  message?: string;
  request_id?: string;
};
