import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import MessageItem from "./MessageItem";

const mockFetchSessionAsset = vi.hoisted(() => vi.fn());
const mockGatewayAPI = vi.hoisted(() => ({ fetchSessionAsset: mockFetchSessionAsset }));

vi.mock("./ToolCallCard", () => ({ default: () => <div>tool-card</div> }));
vi.mock("./AcceptanceMessage", () => ({
  default: () => <div>acceptance-card</div>,
}));
vi.mock("./CodeBlock", () => ({
  default: ({ code }: { code: string }) => <pre>{code}</pre>,
}));
vi.mock("./MarkdownContent", () => ({
  default: ({ content }: { content: string }) => <span>{content}</span>,
}));
vi.mock("@/context/RuntimeProvider", () => ({
  useGatewayAPI: () => mockGatewayAPI,
}));

describe("MessageItem", () => {
  beforeEach(() => {
    mockFetchSessionAsset.mockReset();
  });

  it("renders system message", () => {
    render(
      <MessageItem
        message={
          {
            id: "s1",
            role: "assistant",
            type: "system",
            content: "sys",
            timestamp: 1,
          } as any
        }
      />,
    );
    expect(screen.getByText("sys")).toBeInTheDocument();
  });

  it("renders welcome message", () => {
    render(
      <MessageItem
        message={
          {
            id: "w1",
            role: "assistant",
            type: "welcome",
            content: "hello",
            timestamp: 1,
          } as any
        }
      />,
    );
    expect(screen.getByText("hello")).toBeInTheDocument();
  });

  it("renders thinking message and toggles details", () => {
    render(
      <MessageItem
        message={
          {
            id: "t1",
            role: "assistant",
            type: "thinking",
            content: "reasoning",
            timestamp: 1,
            streaming: false,
            thinkingData: { collapsed: true },
          } as any
        }
      />,
    );
    fireEvent.click(screen.getByText("AI 思考过程"));
    expect(screen.getByText("reasoning")).toBeInTheDocument();
  });

  it("renders tool and acceptance delegates", () => {
    const { rerender } = render(
      <MessageItem
        message={
          {
            id: "m1",
            role: "tool",
            type: "tool_call",
            content: "",
            timestamp: 1,
          } as any
        }
      />,
    );
    expect(screen.getByText("tool-card")).toBeInTheDocument();
    rerender(
      <MessageItem
        message={
          {
            id: "m3",
            role: "assistant",
            type: "acceptance",
            content: "",
            timestamp: 1,
          } as any
        }
      />,
    );
    expect(screen.getByText("acceptance-card")).toBeInTheDocument();
  });

  it("renders plan message card", () => {
    render(
      <MessageItem
        message={
          {
            id: "p1",
            role: "assistant",
            type: "plan",
            content: "",
            timestamp: 1,
            planData: {
              id: "plan-1",
              revision: 2,
              status: "draft",
              spec: {
                goal: "修复计划展示",
                steps: ["发事件"],
                constraints: ["不显示 JSON"],
                open_questions: ["是否需要审批"],
              },
              summary: { goal: "修复计划展示", key_steps: ["发事件"] },
              created_at: "2026-05-20T00:00:00Z",
              updated_at: "2026-05-20T00:00:00Z",
            },
          } as any
        }
      />,
    );
    expect(screen.getByText("计划")).toBeInTheDocument();
    expect(screen.getByText("修复计划展示")).toBeInTheDocument();
    expect(screen.getByText("发事件")).toBeInTheDocument();
    expect(screen.getByText("不显示 JSON")).toBeInTheDocument();
    expect(screen.getByText("是否需要审批")).toBeInTheDocument();
  });

  it("renders code and plain assistant messages", () => {
    const { rerender } = render(
      <MessageItem
        message={
          {
            id: "c1",
            role: "assistant",
            type: "code",
            content: "const a=1",
            timestamp: 1,
          } as any
        }
      />,
    );
    expect(screen.getByText("const a=1")).toBeInTheDocument();
    rerender(
      <MessageItem
        message={
          {
            id: "a1",
            role: "assistant",
            type: "text",
            content: "answer",
            timestamp: 1,
          } as any
        }
      />,
    );
    expect(screen.getByText("answer")).toBeInTheDocument();
  });

  it("renders user image attachments with local preview URLs", () => {
    render(
      <MessageItem
        message={
          {
            id: "u1",
            role: "user",
            type: "text",
            content: "look",
            timestamp: 1,
            attachments: [
              {
                id: "att-1",
                assetId: "asset-1",
                mimeType: "image/png",
                name: "a.png",
                previewUrl: "blob:preview-1",
              },
            ],
          } as any
        }
      />,
    );

    expect(screen.getByText("look")).toBeInTheDocument();
    const image = screen.getByAltText("a.png") as HTMLImageElement;
    expect(image.src).toContain("blob:preview-1");
  });

  it("fetches historical user image attachments with workspace hash", async () => {
    const blob = new Blob(["img"], { type: "image/png" });
    mockFetchSessionAsset.mockResolvedValue(blob);
    if (typeof URL.createObjectURL !== "function") {
      Object.defineProperty(URL, "createObjectURL", { configurable: true, value: vi.fn() });
    }
    const createObjectURL = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:history-1");

    render(
      <MessageItem
        message={
          {
            id: "u1",
            role: "user",
            type: "text",
            content: "look",
            timestamp: 1,
            attachments: [
              {
                id: "att-1",
                sessionId: "session-1",
                workspaceHash: "workspace-b",
                assetId: "asset-1",
                mimeType: "image/png",
                name: "a.png",
              },
            ],
          } as any
        }
      />,
    );

    await waitFor(() => {
      expect(mockFetchSessionAsset).toHaveBeenCalledWith("session-1", "asset-1", "workspace-b");
      expect(screen.getByAltText("a.png")).toHaveAttribute("src", "blob:history-1");
    });
    createObjectURL.mockRestore();
  });

  it("renders user text attachments as chips with name and size", () => {
    render(
      <MessageItem
        message={
          {
            id: "u2",
            role: "user",
            type: "text",
            content: "read this",
            timestamp: 1,
            attachments: [
              {
                id: "att-text-1",
                assetId: "asset-text-1",
                mimeType: "text/markdown",
                name: "notes.md",
                size: 128,
                kind: "text",
              },
            ],
          } as any
        }
      />,
    );

    expect(screen.getByText("read this")).toBeInTheDocument();
    // 文本附件以 chip 展示文件名和大小。
    expect(screen.getByText("notes.md")).toBeInTheDocument();
    expect(screen.getByText("128 B")).toBeInTheDocument();
    // 文本附件不应渲染为 <img>。
    expect(screen.queryByAltText("notes.md")).not.toBeInTheDocument();
    // 文本附件不应触发 session asset 回载。
    expect(mockFetchSessionAsset).not.toHaveBeenCalled();
  });

  it("infers text kind from mimeType when kind is missing for backward compat", () => {
    render(
      <MessageItem
        message={
          {
            id: "u3",
            role: "user",
            type: "text",
            content: "legacy",
            timestamp: 1,
            attachments: [
              {
                id: "att-legacy",
                assetId: "asset-legacy",
                mimeType: "application/json",
                name: "data.json",
                size: 2048,
                // 缺省 kind，按 mimeType 前缀推断为 text。
              },
            ],
          } as any
        }
      />,
    );

    expect(screen.getByText("data.json")).toBeInTheDocument();
    expect(screen.getByText("2.0 KB")).toBeInTheDocument();
    expect(screen.queryByAltText("data.json")).not.toBeInTheDocument();
  });
});
