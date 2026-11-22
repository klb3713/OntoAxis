// read：viewer 及以上
permit (
    principal,
    action in [Action::"read"],
    resource
)
when {
    principal.roles.contains("viewer") ||
    principal.roles.contains("member") ||
    principal.roles.contains("workspace-admin") ||
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};

// write：member 及以上
permit (
    principal,
    action in [Action::"write"],
    resource
)
when {
    principal.roles.contains("member") ||
    principal.roles.contains("workspace-admin") ||
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};

// delete：member 及以上
permit (
    principal,
    action in [Action::"delete"],
    resource
)
when {
    principal.roles.contains("member") ||
    principal.roles.contains("workspace-admin") ||
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};

// invoke：member 及以上
permit (
    principal,
    action in [Action::"invoke"],
    resource
)
when {
    principal.roles.contains("member") ||
    principal.roles.contains("workspace-admin") ||
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};

// admin：workspace-admin 及以上
permit (
    principal,
    action in [Action::"admin"],
    resource
)
when {
    principal.roles.contains("workspace-admin") ||
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};

// audit：tenant-admin 及以上
permit (
    principal,
    action in [Action::"audit"],
    resource
)
when {
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};
