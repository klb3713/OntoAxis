// 租户不变量：任何资源动作都不得跨租户。
// 所有资源实体（含 Tenant）都声明 tenant_id。
forbid (
    principal,
    action,
    resource
)
when {
    principal.tenant_id != resource.tenant_id
};
