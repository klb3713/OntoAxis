// 组织级管理动作只允许 tenant-admin/platform-admin；
// workspace-admin 只能管理工作空间/项目，不能管理组织。
// 注意不覆盖 tenancy:workspace:create（资源为 Organization），该动作允许 workspace-admin。
forbid (
    principal,
    action in [
        Action::"tenancy:organization:update",
        Action::"tenancy:organization:delete",
        Action::"tenancy:membership:add",
        Action::"tenancy:membership:remove"
    ],
    resource
)
when {
    resource is Organization &&
    !(principal.roles.contains("tenant-admin") || principal.roles.contains("platform-admin"))
};

// 创建组织是租户级管理，不能由 workspace-admin 执行。
forbid (
    principal,
    action in [Action::"tenancy:organization:create"],
    resource
)
when {
    !(principal.roles.contains("tenant-admin") || principal.roles.contains("platform-admin"))
};
