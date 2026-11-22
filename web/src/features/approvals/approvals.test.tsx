import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  Refine,
  type BaseRecord,
  type CustomParams,
  type CustomResponse,
  type DataProvider,
} from "@refinedev/core";
import { describe, expect, it } from "vitest";
import { ApiError } from "../../api/errors";
import { createFixtureDatabase, type MockDatabase } from "../../api/fixtures";
import { createEmptyMockDatabase, createMockDataProvider } from "../../api/mock-data-provider";
import { en } from "../../i18n/en";
import { zh } from "../../i18n/zh";
import i18n from "../../i18n";
import { ApprovalsPage } from "./ApprovalsPage";

const FIRST_APPROVAL_ID = "a1f0c2d4-6b3e-4f5a-8c7d-9e0b1a2c3d40";
const FIRST_EXECUTION_ID = "5e2b8c47-9d1a-4f3b-b6c5-7a8f9d0e1c23";
const FIRST_EXECUTION_TRUNCATED = "5e2b8c47-9d1…";
const SECOND_EXECUTION_TRUNCATED = "9c4d1e7a-2b8…";
const APPROVED_EXECUTION_TRUNCATED = "c7a2f3b9-1d5…";

type CustomCall = {
  url: string;
  method: string;
  payload: unknown;
  query: unknown;
};

type TestProviderOptions = {
  database?: MockDatabase;
  throwFor?: (call: CustomCall) => ApiError | null;
};

/** 包裹真实 mock provider，记录 custom 调用，便于断言请求参数与服务端刷新顺序。 */
function createTestProvider(options: TestProviderOptions = {}) {
  const base = createMockDataProvider({ database: options.database ?? createFixtureDatabase() });
  const calls: CustomCall[] = [];

  async function custom<TData extends BaseRecord = BaseRecord, TQuery = unknown, TPayload = unknown>(
    params: CustomParams<TQuery, TPayload>,
  ): Promise<CustomResponse<TData>> {
    const call: CustomCall = {
      url: params.url,
      method: params.method,
      payload: params.payload,
      query: params.query,
    };
    calls.push(call);

    const thrown = options.throwFor?.(call);
    if (thrown) {
      throw thrown;
    }

    const baseCustom = base.custom;
    if (!baseCustom) {
      throw new Error("mock data provider 必须实现 custom");
    }
    return baseCustom<TData, TQuery, TPayload>(params);
  }

  const dataProvider: DataProvider = { ...base, custom };
  return { dataProvider, calls };
}

function renderApprovalsPage(dataProvider: DataProvider) {
  return render(
    <Refine dataProvider={dataProvider}>
      <ApprovalsPage />
    </Refine>,
  );
}

async function openPendingDetail(user: ReturnType<typeof userEvent.setup>): Promise<HTMLElement> {
  const cell = await screen.findByText(FIRST_EXECUTION_TRUNCATED);
  const row = cell.closest("tr");
  if (!row) {
    throw new Error("未找到待审批任务行");
  }
  await user.click(within(row).getByRole("button", { name: i18n.t("approvals.view") }));
  const dialog = await screen.findByRole("dialog");
  await within(dialog).findByText(FIRST_EXECUTION_ID);
  return dialog;
}

function collectKeys(value: unknown, prefix = ""): string[] {
  if (typeof value !== "object" || value === null) {
    return [prefix];
  }
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    collectKeys(child, prefix ? `${prefix}.${key}` : key),
  );
}

const DECISION_ERROR_CASES: Array<[number, string]> = [
  [403, "approvals.errors.noPermission"],
  [409, "approvals.errors.alreadyDecided"],
  [503, "approvals.errors.serviceUnavailable"],
];

describe("ApprovalsPage", () => {
  it("默认按待审批筛选列表，并可按状态切换", async () => {
    const user = userEvent.setup();
    const { dataProvider, calls } = createTestProvider();
    renderApprovalsPage(dataProvider);

    expect(await screen.findByText(FIRST_EXECUTION_TRUNCATED)).toBeInTheDocument();
    expect(screen.getByText(SECOND_EXECUTION_TRUNCATED)).toBeInTheDocument();
    expect(screen.queryByText(APPROVED_EXECUTION_TRUNCATED)).not.toBeInTheDocument();
    expect(calls[0]?.query).toMatchObject({ status: "pending", limit: 20, offset: 0 });

    await user.click(screen.getByRole("tab", { name: i18n.t("approvals.filterApproved") }));

    expect(await screen.findByText(APPROVED_EXECUTION_TRUNCATED)).toBeInTheDocument();
    expect(screen.queryByText(FIRST_EXECUTION_TRUNCATED)).not.toBeInTheDocument();
    expect(
      calls.some((call) => {
        const query = call.query as { status?: string; offset?: number };
        return query.status === "approved" && query.offset === 0;
      }),
    ).toBe(true);
  });

  it("批准后重新拉取详情与列表，只展示服务端状态", async () => {
    const user = userEvent.setup();
    const { dataProvider, calls } = createTestProvider();
    renderApprovalsPage(dataProvider);

    const dialog = await openPendingDetail(user);
    await user.type(within(dialog).getByLabelText(i18n.t("approvals.comment")), "同意执行");
    await user.click(within(dialog).getByRole("button", { name: i18n.t("approvals.approve") }));

    await waitFor(() => {
      expect(
        calls.some(
          (call) => call.method === "post" && call.url === `actions/approvals/${FIRST_APPROVAL_ID}/approve`,
        ),
      ).toBe(true);
    });
    const approveCall = calls.find((call) => call.method === "post");
    expect(approveCall?.payload).toEqual({ comment: "同意执行" });

    await waitFor(() => {
      expect(within(dialog).getByText(i18n.t("approvals.status.approved"))).toBeInTheDocument();
    });

    const detailUrl = `actions/approvals/${FIRST_APPROVAL_ID}`;
    const approveIndex = calls.findIndex((call) => call.method === "post");
    const detailGetIndexes = calls.flatMap((call, index) =>
      call.method === "get" && call.url === detailUrl ? [index] : [],
    );
    expect(detailGetIndexes.length).toBeGreaterThanOrEqual(2);
    expect(Math.max(...detailGetIndexes)).toBeGreaterThan(approveIndex);

    await waitFor(() => {
      expect(screen.queryByText(FIRST_EXECUTION_TRUNCATED)).not.toBeInTheDocument();
    });
    expect(screen.getByText(i18n.t("approvals.approveSuccess"))).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: i18n.t("approvals.approve") })).toBeDisabled();
  });

  it.each(DECISION_ERROR_CASES)("决定失败 HTTP %i 时展示对应错误且不伪装成功", async (status, messageKey) => {
    const user = userEvent.setup();
    const { dataProvider } = createTestProvider({
      throwFor: (call) =>
        call.method === "post"
          ? new ApiError({ code: `http_${status}`, message: "decision failed", status })
          : null,
    });
    renderApprovalsPage(dataProvider);

    const dialog = await openPendingDetail(user);
    await user.click(within(dialog).getByRole("button", { name: i18n.t("approvals.approve") }));

    const alert = await within(dialog).findByRole("alert");
    expect(alert).toHaveTextContent(i18n.t(messageKey));
    expect(screen.queryByText(i18n.t("approvals.approveSuccess"))).not.toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: i18n.t("approvals.approve") })).toBeEnabled();
  });

  it("没有审批任务时展示空状态", async () => {
    const { dataProvider } = createTestProvider({ database: createEmptyMockDatabase() });
    renderApprovalsPage(dataProvider);

    expect(await screen.findByText(i18n.t("approvals.empty"))).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("中英文 approvals 文案键保持一致", () => {
    expect(collectKeys(en.approvals).sort()).toEqual(collectKeys(zh.approvals).sort());
    expect(en.nav.approvals).not.toBe("");
    expect(zh.nav.approvals).not.toBe("");
    expect(en.approvals.stepValue).toContain("{{index}}");
    expect(zh.approvals.stepValue).toContain("{{index}}");
  });
});