import type {
  ApprovalTask,
  Dataset,
  DatasetPreview,
  LinkType,
  ObjectRecord,
  ObjectType,
  Project,
  SavedQuery,
} from "./types";

export type MockDatabase = {
  projects: Project[];
  objectTypes: ObjectType[];
  linkTypes: LinkType[];
  objects: ObjectRecord[];
  datasets: Dataset[];
  datasetPreviews: Record<string, DatasetPreview>;
  savedQueries: SavedQuery[];
  approvals: ApprovalTask[];
};

const T = {
  created: "2026-11-01T09:00:00Z",
  updated: "2026-11-20T15:30:00Z",
  later: "2026-11-22T10:15:00Z",
};

const projects: Project[] = [
  {
    id: "prj_01",
    organizationId: "org_01",
    workspaceId: "ws_01",
    name: "customer-360",
    displayName: "客户 360",
    description: "客户主数据、行为标签与订单关系的一体化探索空间。",
    ownerId: "usr_01",
    createdAt: T.created,
    updatedAt: T.updated,
  },
  {
    id: "prj_02",
    organizationId: "org_01",
    workspaceId: "ws_01",
    name: "supply-chain",
    displayName: "供应链分析",
    description: "供应商、采购订单与库存数据集的对象化建模。",
    ownerId: "usr_02",
    createdAt: T.created,
    updatedAt: T.later,
  },
  {
    id: "prj_03",
    organizationId: "org_01",
    workspaceId: "ws_02",
    name: "risk-control",
    displayName: "风控工作台",
    description: "以订单与支付对象为核心的实时风险查询项目。",
    ownerId: "usr_01",
    createdAt: T.created,
    updatedAt: T.updated,
  },
];

const customerProperties = [
  {
    id: "prop_c1",
    objectTypeId: "ot_customer",
    name: "customer_id",
    displayName: "客户编号",
    description: "客户唯一标识",
    propertyType: "string",
    required: true,
    unique: true,
  },
  {
    id: "prop_c2",
    objectTypeId: "ot_customer",
    name: "name",
    displayName: "客户名称",
    description: "企业客户全称",
    propertyType: "string",
    required: true,
    unique: false,
  },
  {
    id: "prop_c3",
    objectTypeId: "ot_customer",
    name: "tier",
    displayName: "客户等级",
    description: "S/A/B/C 分级",
    propertyType: "string",
    required: false,
    unique: false,
  },
  {
    id: "prop_c4",
    objectTypeId: "ot_customer",
    name: "annual_revenue",
    displayName: "年营收",
    description: "最近财年营收（人民币）",
    propertyType: "double",
    required: false,
    unique: false,
  },
];

const orderProperties = [
  {
    id: "prop_o1",
    objectTypeId: "ot_order",
    name: "order_id",
    displayName: "订单编号",
    description: "订单唯一标识",
    propertyType: "string",
    required: true,
    unique: true,
  },
  {
    id: "prop_o2",
    objectTypeId: "ot_order",
    name: "customer_id",
    displayName: "客户编号",
    description: "下单客户",
    propertyType: "string",
    required: true,
    unique: false,
  },
  {
    id: "prop_o3",
    objectTypeId: "ot_order",
    name: "amount",
    displayName: "订单金额",
    description: "含税金额（人民币）",
    propertyType: "double",
    required: true,
    unique: false,
  },
  {
    id: "prop_o4",
    objectTypeId: "ot_order",
    name: "status",
    displayName: "订单状态",
    description: "created/paid/shipped/closed",
    propertyType: "string",
    required: true,
    unique: false,
  },
];

const productProperties = [
  {
    id: "prop_p1",
    objectTypeId: "ot_product",
    name: "product_id",
    displayName: "产品编号",
    description: "产品唯一标识",
    propertyType: "string",
    required: true,
    unique: true,
  },
  {
    id: "prop_p2",
    objectTypeId: "ot_product",
    name: "title",
    displayName: "产品名称",
    description: "产品展示名称",
    propertyType: "string",
    required: true,
    unique: false,
  },
  {
    id: "prop_p3",
    objectTypeId: "ot_product",
    name: "category",
    displayName: "产品类目",
    description: "一级类目",
    propertyType: "string",
    required: false,
    unique: false,
  },
];

const objectTypes: ObjectType[] = [
  {
    id: "ot_customer",
    name: "Customer",
    displayName: "客户",
    description: "企业客户主数据对象，客户 360 的核心实体。",
    primaryKeyProperty: "customer_id",
    properties: customerProperties,
    createdAt: T.created,
    updatedAt: T.updated,
  },
  {
    id: "ot_order",
    name: "Order",
    displayName: "订单",
    description: "交易订单对象，关联客户与产品。",
    primaryKeyProperty: "order_id",
    properties: orderProperties,
    createdAt: T.created,
    updatedAt: T.later,
  },
  {
    id: "ot_product",
    name: "Product",
    displayName: "产品",
    description: "可售产品目录对象。",
    primaryKeyProperty: "product_id",
    properties: productProperties,
    createdAt: T.created,
    updatedAt: T.updated,
  },
];

const linkTypes: LinkType[] = [
  {
    id: "lt_customer_order",
    name: "customer_places_order",
    displayName: "客户下单",
    sourceTypeId: "ot_customer",
    targetTypeId: "ot_order",
    createdAt: T.created,
    updatedAt: T.updated,
  },
  {
    id: "lt_order_product",
    name: "order_contains_product",
    displayName: "订单包含产品",
    sourceTypeId: "ot_order",
    targetTypeId: "ot_product",
    createdAt: T.created,
    updatedAt: T.updated,
  },
];

const objects: ObjectRecord[] = [
  {
    id: "obj_c_001",
    objectTypeId: "ot_customer",
    properties: { customer_id: "C-1001", name: "远山科技", tier: "S", annual_revenue: 128000000 },
    version: 3,
    createdAt: T.created,
    updatedAt: T.later,
  },
  {
    id: "obj_c_002",
    objectTypeId: "ot_customer",
    properties: { customer_id: "C-1002", name: "海川制造", tier: "A", annual_revenue: 56000000 },
    version: 1,
    createdAt: T.created,
    updatedAt: T.updated,
  },
  {
    id: "obj_c_003",
    objectTypeId: "ot_customer",
    properties: { customer_id: "C-1003", name: "北辰零售", tier: "A", annual_revenue: 83000000 },
    version: 2,
    createdAt: T.created,
    updatedAt: T.updated,
  },
  {
    id: "obj_o_001",
    objectTypeId: "ot_order",
    properties: { order_id: "SO-2026-0001", customer_id: "C-1001", amount: 128000, status: "paid" },
    version: 2,
    createdAt: T.created,
    updatedAt: T.later,
  },
  {
    id: "obj_o_002",
    objectTypeId: "ot_order",
    properties: { order_id: "SO-2026-0002", customer_id: "C-1002", amount: 76000, status: "shipped" },
    version: 1,
    createdAt: T.created,
    updatedAt: T.updated,
  },
  {
    id: "obj_o_003",
    objectTypeId: "ot_order",
    properties: { order_id: "SO-2026-0003", customer_id: "C-1003", amount: 43000, status: "created" },
    version: 1,
    createdAt: T.created,
    updatedAt: T.updated,
  },
  {
    id: "obj_p_001",
    objectTypeId: "ot_product",
    properties: { product_id: "P-01", title: "Ontology 建模平台", category: "数据平台" },
    version: 1,
    createdAt: T.created,
    updatedAt: T.updated,
  },
  {
    id: "obj_p_002",
    objectTypeId: "ot_product",
    properties: { product_id: "P-02", title: "实时数据集成", category: "数据集成" },
    version: 1,
    createdAt: T.created,
    updatedAt: T.updated,
  },
];

const datasets: Dataset[] = [
  {
    id: "ds_orders_raw",
    name: "orders_raw",
    displayName: "订单原始数据",
    description: "来自交易系统的订单明细快照，用于 Order 对象物化。",
    ownerId: "usr_01",
    format: "ICEBERG",
    storageLocation: "s3://ontoaxis-dev/warehouse/orders_raw",
    createdAt: T.created,
    updatedAt: T.later,
  },
  {
    id: "ds_customer_profiles",
    name: "customer_profiles",
    displayName: "客户画像",
    description: "客户主数据与标签的日增量数据集。",
    ownerId: "usr_02",
    format: "PARQUET",
    storageLocation: "s3://ontoaxis-dev/warehouse/customer_profiles",
    createdAt: T.created,
    updatedAt: T.updated,
  },
  {
    id: "ds_products",
    name: "products",
    displayName: "产品目录",
    description: "产品主数据，用于 Product 对象建模。",
    ownerId: "usr_01",
    format: "ICEBERG",
    storageLocation: "s3://ontoaxis-dev/warehouse/products",
    createdAt: T.created,
    updatedAt: T.updated,
  },
];

const datasetPreviews: Record<string, DatasetPreview> = {
  ds_orders_raw: {
    datasetId: "ds_orders_raw",
    columns: [
      { name: "order_id", type: "string" },
      { name: "customer_id", type: "string" },
      { name: "amount", type: "double" },
      { name: "status", type: "string" },
    ],
    rows: [
      { order_id: "SO-2026-0001", customer_id: "C-1001", amount: 128000, status: "paid" },
      { order_id: "SO-2026-0002", customer_id: "C-1002", amount: 76000, status: "shipped" },
      { order_id: "SO-2026-0003", customer_id: "C-1003", amount: 43000, status: "created" },
    ],
    rowCount: 3,
  },
  ds_customer_profiles: {
    datasetId: "ds_customer_profiles",
    columns: [
      { name: "customer_id", type: "string" },
      { name: "name", type: "string" },
      { name: "tier", type: "string" },
      { name: "annual_revenue", type: "double" },
    ],
    rows: [
      { customer_id: "C-1001", name: "远山科技", tier: "S", annual_revenue: 128000000 },
      { customer_id: "C-1002", name: "海川制造", tier: "A", annual_revenue: 56000000 },
    ],
    rowCount: 2,
  },
  ds_products: {
    datasetId: "ds_products",
    columns: [
      { name: "product_id", type: "string" },
      { name: "title", type: "string" },
      { name: "category", type: "string" },
    ],
    rows: [
      { product_id: "P-01", title: "Ontology 建模平台", category: "数据平台" },
      { product_id: "P-02", title: "实时数据集成", category: "数据集成" },
    ],
    rowCount: 2,
  },
};

const savedQueries: SavedQuery[] = [
  {
    id: "sq_01",
    name: "高价值客户订单",
    sql: "SELECT customer_id, amount, status FROM orders WHERE amount > 100000 ORDER BY amount DESC",
    ownerId: "usr_01",
    createdAt: T.created,
    updatedAt: T.updated,
  },
  {
    id: "sq_02",
    name: "产品目录快照",
    sql: "SELECT product_id, title, category FROM products ORDER BY product_id",
    ownerId: "usr_02",
    createdAt: T.created,
    updatedAt: T.later,
  },
  {
    id: "sq_03",
    name: "客户等级分布",
    sql: "SELECT tier, COUNT(*) AS customer_count FROM customer_profiles GROUP BY tier",
    ownerId: "usr_01",
    createdAt: T.created,
    updatedAt: T.updated,
  },
];

const approvals: ApprovalTask[] = [
  {
    id: "a1f0c2d4-6b3e-4f5a-8c7d-9e0b1a2c3d40",
    tenantId: "7d1c9a3e-5b2f-4e8d-8a6c-1f0b3d7e9c21",
    executionId: "5e2b8c47-9d1a-4f3b-b6c5-7a8f9d0e1c23",
    stepIndex: 1,
    status: "pending",
    requestedAt: "2026-11-22T08:15:00Z",
    deadlineAt: "2026-11-23T08:15:00Z",
    decidedAt: null,
    decidedBy: null,
    comment: "",
    reasonCode: "",
  },
  {
    id: "f4b7e2a9-1c8d-4a3b-9e6f-2d5c8b0a7e41",
    tenantId: "7d1c9a3e-5b2f-4e8d-8a6c-1f0b3d7e9c21",
    executionId: "9c4d1e7a-2b8f-4a6c-9d3e-5f7b8a0c2d46",
    stepIndex: 0,
    status: "pending",
    requestedAt: "2026-11-22T09:40:00Z",
    deadlineAt: "2026-11-23T09:40:00Z",
    decidedAt: null,
    decidedBy: null,
    comment: "",
    reasonCode: "",
  },
  {
    id: "c9e3b7a1-5d2f-4c8e-8a4b-7f1d9c2e6b44",
    tenantId: "7d1c9a3e-5b2f-4e8d-8a6c-1f0b3d7e9c21",
    executionId: "c7a2f3b9-1d5e-4c8a-8b6f-2e9d0a1b3c45",
    stepIndex: 2,
    status: "approved",
    requestedAt: "2026-11-21T10:30:00Z",
    deadlineAt: "2026-11-22T10:30:00Z",
    decidedAt: "2026-11-21T12:05:00Z",
    decidedBy: "usr_01",
    comment: "金额与预算一致，同意执行。",
    reasonCode: "approved",
  },
  {
    id: "d2a8f4c6-9e3b-4a7d-8c1f-5b0e2d9a4c47",
    tenantId: "7d1c9a3e-5b2f-4e8d-8a6c-1f0b3d7e9c21",
    executionId: "e3b8d6a1-7c4f-4a2d-9e5b-1f0c8a3d6e42",
    stepIndex: 3,
    status: "rejected",
    requestedAt: "2026-11-20T14:10:00Z",
    deadlineAt: "2026-11-21T14:10:00Z",
    decidedAt: "2026-11-20T16:45:00Z",
    decidedBy: "usr_02",
    comment: "导出涉及敏感字段，驳回。",
    reasonCode: "approval_rejected",
  },
  {
    id: "e8c1a5d3-7b4e-4f9a-9d2c-6a3e0b8f5d48",
    tenantId: "7d1c9a3e-5b2f-4e8d-8a6c-1f0b3d7e9c21",
    executionId: "b1d9e4f7-3a6c-4b8e-8d2f-5c7a0e9b1d43",
    stepIndex: 1,
    status: "expired",
    requestedAt: "2026-11-19T09:00:00Z",
    deadlineAt: "2026-11-21T00:00:00Z",
    decidedAt: null,
    decidedBy: null,
    comment: "",
    reasonCode: "approval_timeout",
  },
];

export function createFixtureDatabase(): MockDatabase {
  const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T;
  return {
    projects: clone(projects),
    objectTypes: clone(objectTypes),
    linkTypes: clone(linkTypes),
    objects: clone(objects),
    datasets: clone(datasets),
    datasetPreviews: clone(datasetPreviews),
    savedQueries: clone(savedQueries),
    approvals: clone(approvals),
  };
}
