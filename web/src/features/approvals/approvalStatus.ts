import type { ApprovalStatus } from "../../api/types";

const STATUS_BADGE_CLASS: Record<ApprovalStatus, string> = {
  pending: "badge badge--amber",
  approved: "badge badge--green",
  rejected: "badge badge--neutral",
  expired: "badge badge--neutral",
};

/** 审批状态 → 现有 badge 样式；拒绝/过期没有红色 badge，使用中性色。 */
export function approvalStatusBadgeClass(status: ApprovalStatus): string {
  return STATUS_BADGE_CLASS[status];
}

/** 长 UUID 在列表里截断显示，完整值通过 title 暴露。 */
export function truncateId(value: string, maxLength = 12): string {
  return value.length > maxLength ? `${value.slice(0, maxLength)}…` : value;
}