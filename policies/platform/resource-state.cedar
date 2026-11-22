// 已归档 Pipeline 只读。
forbid (
    principal,
    action in [Action::"pipeline:update", Action::"pipeline:delete"],
    resource
)
when {
    resource is Pipeline &&
    resource.status == "archived"
};

// 只有已发布 Pipeline 可以触发。
forbid (
    principal,
    action in [Action::"pipeline:invoke"],
    resource
)
when {
    resource is Pipeline &&
    resource.status != "published"
};

// 已归档 Pipeline 不得再发布。
forbid (
    principal,
    action in [Action::"pipeline:publish"],
    resource
)
when {
    resource is Pipeline &&
    resource.status == "archived"
};

// 只有 active ActionType 可以运行；draft/disabled 允许校验/预览但不允许执行。
forbid (
    principal,
    action in [Action::"action_type:run"],
    resource
)
when {
    resource is ActionType &&
    resource.status != "active"
};

// 只有 active Connector 可以同步；test/infer_schema 不受此限。
forbid (
    principal,
    action in [Action::"connector:sync"],
    resource
)
when {
    resource is Connector &&
    resource.status != "active"
};

// 只有 queued/running 的同步任务可以取消。
forbid (
    principal,
    action in [Action::"ingestion:cancel"],
    resource
)
when {
    resource is IngestJob &&
    !(resource.status == "queued" || resource.status == "running")
};

// 只有 queued/running 的动作执行可以取消。
forbid (
    principal,
    action in [Action::"action_execution:cancel"],
    resource
)
when {
    resource is ActionExecution &&
    !(resource.status == "queued" || resource.status == "running")
};

// 只有 failed/cancelled 的动作执行可以重试。
forbid (
    principal,
    action in [Action::"action_execution:retry"],
    resource
)
when {
    resource is ActionExecution &&
    !(resource.status == "failed" || resource.status == "cancelled")
};
