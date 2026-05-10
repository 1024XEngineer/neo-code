import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import FileChangePanel from "./FileChangePanel";
import { useChatStore } from "@/stores/useChatStore";
import { useSessionStore } from "@/stores/useSessionStore";
import {
  CHANGES_PREVIEW_TAB_ID,
  GIT_DIFF_PREVIEW_TAB_ID,
  useUIStore,
} from "@/stores/useUIStore";

const mockGatewayAPI = {
  listGitDiffFiles: vi.fn(),
  readGitDiffFile: vi.fn(),
  restoreCheckpoint: vi.fn(),
  loadSession: vi.fn(),
  listSessionTodos: vi.fn(),
  listCheckpoints: vi.fn(),
};

vi.mock("@/context/RuntimeProvider", () => ({
  useGatewayAPI: () => mockGatewayAPI,
}));

vi.mock("./CodePreviewEditor", () => ({
  default: ({
    path,
    content,
    theme,
  }: {
    path: string;
    content: string;
    theme: string;
  }) => (
    <div data-testid="code-preview-editor" data-path={path} data-theme={theme}>
      {content}
    </div>
  ),
}));

vi.mock("./GitDiffPreviewEditor", () => ({
  default: ({
    path,
    originalContent,
    modifiedContent,
    renderSideBySide,
  }: {
    path: string;
    originalContent: string;
    modifiedContent: string;
    renderSideBySide: boolean;
  }) => (
    <div
      data-testid="git-diff-preview-editor"
      data-path={path}
      data-side-by-side={String(renderSideBySide)}
    >
      {originalContent}::{modifiedContent}
    </div>
  ),
}));

describe("FileChangePanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGatewayAPI.listGitDiffFiles.mockResolvedValue({
      payload: {
        in_git_repo: true,
        branch: "main",
        ahead: 1,
        behind: 0,
        truncated: false,
        total_count: 1,
        files: [
          {
            path: "src/main.go",
            status: "modified",
          },
        ],
      },
    });
    mockGatewayAPI.readGitDiffFile.mockResolvedValue({
      payload: {
        path: "src/main.go",
        status: "modified",
        original_content: "before",
        modified_content: "after",
        size_original: 6,
        size_modified: 5,
      },
    });
    mockGatewayAPI.restoreCheckpoint.mockResolvedValue({
      payload: {
        checkpoint_id: "cp-1",
        session_id: "sess-1",
      },
    });
    mockGatewayAPI.loadSession.mockResolvedValue({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [{ role: "assistant", content: "reloaded" }],
      },
    });
    mockGatewayAPI.listSessionTodos.mockResolvedValue({
      payload: { items: [] },
    });
    mockGatewayAPI.listCheckpoints.mockResolvedValue({
      payload: [],
    });
    useChatStore.setState({ isGenerating: false } as never);
    useSessionStore.setState({ currentSessionId: "sess-1" } as never);
    useUIStore.setState({
      fileChanges: [
        {
          id: "fc-1",
          path: "src/a.txt",
          status: "modified",
          additions: 2,
          deletions: 2,
          checkpoint_id: "cp-1",
          rollback_checkpoint_id: "cp-rollback-1",
          hunks: [
            {
              header: "@@ -1,3 +1,3 @@",
              additions: 1,
              deletions: 1,
              lines: [
                { type: "header", content: "@@ -1,3 +1,3 @@" },
                { type: "context", content: "line 1" },
                { type: "del", content: "line 2 old" },
                { type: "add", content: "line 2 new" },
              ],
            },
          ],
        },
      ],
      gitDiffSummary: {
        in_git_repo: true,
        branch: "main",
        ahead: 1,
        behind: 0,
        truncated: false,
        total_count: 1,
        files: [
          {
            path: "src/main.go",
            status: "modified",
          },
        ],
      },
      gitDiffLoading: false,
      gitDiffError: "",
      previewTabs: [
        {
          id: CHANGES_PREVIEW_TAB_ID,
          kind: "changes",
          title: "文件变更",
          closable: false,
        },
        {
          id: GIT_DIFF_PREVIEW_TAB_ID,
          kind: "git-diff",
          title: "Git Diff",
          closable: false,
        },
      ],
      activePreviewTabId: CHANGES_PREVIEW_TAB_ID,
      changesPanelOpen: true,
      changesPanelWidth: 560,
      theme: "dark",
      isRestoringCheckpoint: false,
    } as never);
  });

  it("renders change diff blocks and keeps accept as a UI-only review marker", () => {
    render(<FileChangePanel />);

    fireEvent.click(screen.getByText("src/a.txt"));

    expect(screen.getByTestId("accept-change-fc-1")).toBeTruthy();
    expect(screen.getAllByTestId(/diff-hunk-fc-1-/)).toHaveLength(1);
    expect(screen.getByText("line 1")).toBeTruthy();
    expect(screen.getByText("line 2 new")).toBeTruthy();

    fireEvent.click(screen.getByTestId("accept-change-fc-1"));

    expect(useUIStore.getState().fileChanges[0]?.status).toBe("accepted");
  });

  it("renders restore button per file change and disables it when rollback checkpoint is missing", () => {
    useUIStore.setState({
      fileChanges: [
        {
          id: "fc-with-cp",
          path: "src/with-cp.txt",
          status: "modified",
          additions: 1,
          deletions: 0,
          checkpoint_id: "cp-1",
          rollback_checkpoint_id: "cp-rollback-1",
          hunks: [],
        },
        {
          id: "fc-without-cp",
          path: "src/no-cp.txt",
          status: "modified",
          additions: 1,
          deletions: 0,
          checkpoint_id: "cp-legacy-only",
          hunks: [],
        },
      ],
    } as never);

    render(<FileChangePanel />);

    fireEvent.click(screen.getByText("src/with-cp.txt"));
    fireEvent.click(screen.getByText("src/no-cp.txt"));

    expect(screen.getByTestId("restore-change-fc-with-cp")).toBeEnabled();
    expect(screen.getByTestId("restore-change-fc-without-cp")).toBeDisabled();
  });

  it("calls restoreCheckpoint after confirming rollback", async () => {
    render(<FileChangePanel />);

    fireEvent.click(screen.getByText("src/a.txt"));
    fireEvent.click(screen.getByTestId("restore-change-fc-1"));

    const confirmButtons = screen.getAllByRole("button", { name: "Rollback" });
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);

    await waitFor(() => {
      expect(mockGatewayAPI.restoreCheckpoint).toHaveBeenCalledWith({
        session_id: "sess-1",
        checkpoint_id: "cp-rollback-1",
        mode: "baseline",
        paths: ["src/a.txt"],
      });
    });
    expect(mockGatewayAPI.loadSession).not.toHaveBeenCalled();
  });

  it("rolls back all current file changes with one baseline restore request", async () => {
    render(<FileChangePanel />);

    fireEvent.click(screen.getByTestId("restore-all-changes"));
    const confirmButtons = screen.getAllByRole("button", {
      name: "Rollback all",
    });
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);

    await waitFor(() => {
      expect(mockGatewayAPI.restoreCheckpoint).toHaveBeenCalledWith({
        session_id: "sess-1",
        checkpoint_id: "cp-rollback-1",
        mode: "baseline",
        paths: ["src/a.txt"],
      });
    });
  });

  it("disables accept and restore actions while the session is generating", () => {
    useChatStore.setState({ isGenerating: true } as never);
    render(<FileChangePanel />);

    fireEvent.click(screen.getByText("src/a.txt"));

    expect(screen.getByTestId("accept-change-fc-1")).toBeDisabled();
    expect(screen.getByTestId("restore-change-fc-1")).toBeDisabled();
    fireEvent.click(screen.getByTestId("restore-change-fc-1"));
    expect(mockGatewayAPI.restoreCheckpoint).not.toHaveBeenCalled();
  });

  it("disables accept and restore actions while checkpoint restore is in progress", () => {
    useUIStore.setState({ isRestoringCheckpoint: true } as never);
    render(<FileChangePanel />);

    fireEvent.click(screen.getByText("src/a.txt"));

    expect(screen.getByTestId("accept-change-fc-1")).toBeDisabled();
    expect(screen.getByTestId("restore-change-fc-1")).toBeDisabled();
  });

  it("prevents duplicate restore calls while restore is in-flight", async () => {
    let resolveRestore: ((value?: unknown) => void) | undefined;
    mockGatewayAPI.restoreCheckpoint.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveRestore = resolve;
        }),
    );

    render(<FileChangePanel />);

    fireEvent.click(screen.getByText("src/a.txt"));
    fireEvent.click(screen.getByTestId("restore-change-fc-1"));
    const confirmButtons = screen.getAllByRole("button", { name: "Rollback" });
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);

    await waitFor(() => {
      expect(mockGatewayAPI.restoreCheckpoint).toHaveBeenCalledTimes(1);
    });

    const restoreButton = screen.getByTestId("restore-change-fc-1");
    expect(restoreButton).toBeDisabled();
    fireEvent.click(restoreButton);
    expect(mockGatewayAPI.restoreCheckpoint).toHaveBeenCalledTimes(1);

    resolveRestore?.();
  });

  it("keeps the panel body clipped and uses the content area as the scroll container", () => {
    render(<FileChangePanel />);

    const panelBody = screen.getByTestId("file-change-panel-body");
    const scrollArea = screen.getByTestId("changes-scroll-area");
    const view = scrollArea.parentElement as HTMLElement;
    const contentStack = screen.getByTestId("changes-content-stack");

    expect(panelBody).toHaveStyle({ overflow: "hidden", minHeight: "0px" });
    expect(view).toHaveStyle({
      flex: "1 1 0%",
      minHeight: "0px",
      overflow: "hidden",
    });
    expect(scrollArea).toHaveStyle({
      overflow: "auto",
      minHeight: "0px",
      flex: "1 1 0%",
    });
    expect(scrollArea).not.toHaveStyle({ display: "flex" });
    expect(contentStack).toHaveStyle({
      display: "flex",
      flexDirection: "column",
      gap: "10px",
    });
  });

  it("caps expanded diff blocks so long hunks do not stretch the whole panel", () => {
    render(<FileChangePanel />);

    fireEvent.click(screen.getByText("src/a.txt"));

    expect(screen.getByTestId("diff-scroller-fc-1")).toHaveStyle({
      display: "flex",
      flexDirection: "column",
    });
    expect(screen.getByTestId("change-card-fc-1")).toHaveStyle({
      flexShrink: "0",
    });
    expect(screen.getByTestId("diff-hunk-scroller-fc-1-0")).toHaveStyle({
      overflowX: "auto",
      overflowY: "hidden",
      maxWidth: "100%",
    });
    expect(screen.getByText("line 2 new").parentElement).toHaveStyle({
      width: "max-content",
      minWidth: "100%",
    });
    expect(screen.getByText("line 2 new")).not.toHaveStyle({
      overflowX: "auto",
    });
  });

  it("forwards vertical wheel events from hunk scrollers to the outer changes scroll area", () => {
    render(<FileChangePanel />);

    fireEvent.click(screen.getByText("src/a.txt"));

    const scrollArea = screen.getByTestId(
      "changes-scroll-area",
    ) as HTMLDivElement;
    const hunkScroller = screen.getByTestId("diff-hunk-scroller-fc-1-0");
    scrollArea.scrollBy = vi.fn();

    fireEvent.wheel(hunkScroller, { deltaY: 120, deltaX: 0 });

    expect(scrollArea.scrollBy).toHaveBeenCalledWith({ top: 120 });
  });

  it("keeps multiple hunks inside the outer vertical scroll container", () => {
    useUIStore.setState({
      fileChanges: [
        {
          id: "fc-2",
          path: "src/multi.txt",
          status: "modified",
          additions: 2,
          deletions: 1,
          hunks: [
            {
              header: "@@ -1,2 +1,2 @@",
              additions: 1,
              deletions: 1,
              lines: [
                { type: "header", content: "@@ -1,2 +1,2 @@" },
                { type: "del", content: "old line" },
                { type: "add", content: "new line" },
              ],
            },
            {
              header: "@@ -8,0 +9,1 @@",
              additions: 1,
              deletions: 0,
              lines: [
                { type: "header", content: "@@ -8,0 +9,1 @@" },
                { type: "context", content: "ctx" },
                { type: "add", content: "tail line" },
              ],
            },
          ],
        },
      ],
    } as never);

    render(<FileChangePanel />);

    fireEvent.click(screen.getByText("src/multi.txt"));

    expect(screen.getByTestId("changes-scroll-area")).toHaveStyle({
      overflow: "auto",
    });
    expect(screen.getAllByTestId(/diff-hunk-fc-2-/)).toHaveLength(2);
    expect(screen.getByTestId("diff-hunk-scroller-fc-2-0")).toHaveStyle({
      overflowX: "auto",
      overflowY: "hidden",
      maxWidth: "100%",
    });
    expect(screen.getByTestId("diff-hunk-scroller-fc-2-1")).toHaveStyle({
      overflowX: "auto",
      overflowY: "hidden",
      maxWidth: "100%",
    });
  });

  it("keeps later files in normal document flow when the first file is expanded", () => {
    useUIStore.setState({
      fileChanges: [
        {
          id: "fc-3",
          path: "src/first.txt",
          status: "modified",
          additions: 3,
          deletions: 1,
          hunks: [
            {
              header: "@@ -1,2 +1,4 @@",
              additions: 2,
              deletions: 1,
              lines: [
                { type: "header", content: "@@ -1,2 +1,4 @@" },
                { type: "context", content: "head" },
                { type: "del", content: "old body" },
                { type: "add", content: "new body" },
                { type: "add", content: "tail" },
              ],
            },
            {
              header: "@@ -10,0 +13,1 @@",
              additions: 1,
              deletions: 0,
              lines: [
                { type: "header", content: "@@ -10,0 +13,1 @@" },
                { type: "add", content: "after block" },
              ],
            },
          ],
        },
        {
          id: "fc-4",
          path: "src/second.txt",
          status: "modified",
          additions: 1,
          deletions: 0,
          hunks: [
            {
              header: "@@ -0,0 +1,1 @@",
              additions: 1,
              deletions: 0,
              lines: [
                { type: "header", content: "@@ -0,0 +1,1 @@" },
                { type: "add", content: "next file line" },
              ],
            },
          ],
        },
      ],
    } as never);

    render(<FileChangePanel />);

    fireEvent.click(screen.getByText("src/first.txt"));

    const contentStack = screen.getByTestId("changes-content-stack");
    expect(contentStack).toHaveTextContent("src/second.txt");
    expect(screen.getByTestId("change-card-fc-4")).toHaveStyle({
      flexShrink: "0",
    });
  });

  it("keeps both fixed tabs visible in the primary switcher and hides secondary chips by default", () => {
    render(<FileChangePanel />);

    const primaryTabs = screen.getByTestId("preview-primary-tabs");
    expect(primaryTabs).toContainElement(
      screen.getByTestId(`preview-tab-${CHANGES_PREVIEW_TAB_ID}`),
    );
    expect(primaryTabs).toContainElement(
      screen.getByTestId(`preview-tab-${GIT_DIFF_PREVIEW_TAB_ID}`),
    );
    expect(screen.queryByTestId("preview-secondary-tabs")).toBeNull();
  });

  it("opens a git diff file tab from the fixed git diff view", async () => {
    mockGatewayAPI.readGitDiffFile.mockResolvedValueOnce({
      payload: {
        path: "src/main.go",
        status: "modified",
        old_path: "src/legacy.go",
        encoding: "utf-8",
        original_content: "before",
        modified_content: "after",
        size_original: 6,
        size_modified: 5,
      },
    });

    render(<FileChangePanel />);

    fireEvent.click(
      screen.getByTestId(`preview-tab-${GIT_DIFF_PREVIEW_TAB_ID}`),
    );
    expect(await screen.findByTestId("git-diff-content-stack")).toHaveStyle({
      display: "flex",
      flexDirection: "column",
      gap: "10px",
    });
    expect(screen.getByTestId("git-diff-entry-src/main.go")).toHaveStyle({
      flexShrink: "0",
    });
    fireEvent.click(await screen.findByTestId("git-diff-entry-src/main.go"));

    await waitFor(() => {
      expect(mockGatewayAPI.readGitDiffFile).toHaveBeenCalledWith({
        path: "src/main.go",
      });
    });

    const preview = await screen.findByTestId("git-diff-preview-editor");
    expect(preview).toHaveAttribute("data-path", "src/main.go");
    expect(preview).toHaveAttribute("data-side-by-side", "true");
    expect(preview.textContent).toContain("before::after");

    const gitDiffHeader = screen.getByTestId("git-diff-file-preview-header");
    const gitDiffPath = within(gitDiffHeader).getByTestId(
      "git-diff-file-preview-path",
    );
    expect(gitDiffPath.textContent).toBe("src/main.go");
    expect(gitDiffPath).toHaveAttribute("title", "src/main.go");
    expect(gitDiffPath).toHaveStyle({
      whiteSpace: "nowrap",
      overflow: "hidden",
      textOverflow: "ellipsis",
    });
    expect(gitDiffHeader.textContent?.trim()).toBe("src/main.go");
  });

  it("renders file preview tabs in the secondary chip row instead of mixing them into the primary switcher", () => {
    useUIStore.setState({
      previewTabs: [
        {
          id: CHANGES_PREVIEW_TAB_ID,
          kind: "changes",
          title: "文件变更",
          closable: false,
        },
        {
          id: GIT_DIFF_PREVIEW_TAB_ID,
          kind: "git-diff",
          title: "Git Diff",
          closable: false,
        },
        {
          id: "file:cmd/neocode/main.go",
          kind: "file",
          title: "main.go",
          closable: true,
          path: "cmd/neocode/main.go",
          size: 1024,
          encoding: "utf-8",
          mod_time: "2026-05-08T10:53:48Z",
          content: "package main",
          loading: false,
          loaded: true,
          error: "",
          truncated: false,
          is_binary: false,
        },
      ],
      activePreviewTabId: "file:cmd/neocode/main.go",
    } as never);

    render(<FileChangePanel />);

    const primaryTabs = screen.getByTestId("preview-primary-tabs");
    const secondaryTabs = screen.getByTestId("preview-secondary-tabs");
    expect(primaryTabs).toContainElement(
      screen.getByTestId(`preview-tab-${CHANGES_PREVIEW_TAB_ID}`),
    );
    expect(primaryTabs).toContainElement(
      screen.getByTestId(`preview-tab-${GIT_DIFF_PREVIEW_TAB_ID}`),
    );
    expect(primaryTabs).not.toContainElement(
      screen.getByTestId("preview-tab-file:cmd/neocode/main.go"),
    );
    expect(secondaryTabs).toContainElement(
      screen.getByTestId("preview-tab-file:cmd/neocode/main.go"),
    );
  });

  it("marks the fixed git diff switcher as context-active while a git diff file chip is selected", async () => {
    render(<FileChangePanel />);

    fireEvent.click(
      screen.getByTestId(`preview-tab-${GIT_DIFF_PREVIEW_TAB_ID}`),
    );
    fireEvent.click(await screen.findByTestId("git-diff-entry-src/main.go"));

    await waitFor(() => {
      expect(mockGatewayAPI.readGitDiffFile).toHaveBeenCalledWith({
        path: "src/main.go",
      });
    });

    expect(
      screen.getByTestId(`preview-tab-${GIT_DIFF_PREVIEW_TAB_ID}`),
    ).toHaveAttribute("data-context-active", "true");
    expect(
      screen.getByTestId("preview-tab-git-diff-file:src/main.go"),
    ).toHaveAttribute("aria-selected", "true");
  });

  it("falls back to the fixed git diff tab after closing the active git diff file chip", async () => {
    render(<FileChangePanel />);

    fireEvent.click(
      screen.getByTestId(`preview-tab-${GIT_DIFF_PREVIEW_TAB_ID}`),
    );
    fireEvent.click(await screen.findByTestId("git-diff-entry-src/main.go"));

    await waitFor(() => {
      expect(
        screen.getByTestId("preview-tab-git-diff-file:src/main.go"),
      ).toBeTruthy();
    });

    fireEvent.click(
      screen.getByTestId("preview-tab-close-git-diff-file:src/main.go"),
    );

    expect(useUIStore.getState().activePreviewTabId).toBe(
      GIT_DIFF_PREVIEW_TAB_ID,
    );
    expect(
      screen.queryByTestId("preview-tab-git-diff-file:src/main.go"),
    ).toBeNull();
  });

  it("keeps keyboard roving focus across both fixed tabs and file chips", () => {
    useUIStore.setState({
      previewTabs: [
        {
          id: CHANGES_PREVIEW_TAB_ID,
          kind: "changes",
          title: "文件变更",
          closable: false,
        },
        {
          id: GIT_DIFF_PREVIEW_TAB_ID,
          kind: "git-diff",
          title: "Git Diff",
          closable: false,
        },
        {
          id: "file:cmd/neocode/main.go",
          kind: "file",
          title: "main.go",
          closable: true,
          path: "cmd/neocode/main.go",
          content: "package main",
          loading: false,
          loaded: true,
          error: "",
          truncated: false,
          is_binary: false,
        },
      ],
      activePreviewTabId: CHANGES_PREVIEW_TAB_ID,
    } as never);

    render(<FileChangePanel />);

    const changesTab = screen.getByTestId(
      `preview-tab-${CHANGES_PREVIEW_TAB_ID}`,
    );
    changesTab.focus();
    fireEvent.keyDown(changesTab, { key: "ArrowRight" });
    expect(useUIStore.getState().activePreviewTabId).toBe(
      GIT_DIFF_PREVIEW_TAB_ID,
    );

    const gitDiffTab = screen.getByTestId(
      `preview-tab-${GIT_DIFF_PREVIEW_TAB_ID}`,
    );
    fireEvent.keyDown(gitDiffTab, { key: "ArrowRight" });
    expect(useUIStore.getState().activePreviewTabId).toBe(
      "file:cmd/neocode/main.go",
    );
  });

  it("opens expanded nested untracked files from the git diff list", async () => {
    mockGatewayAPI.listGitDiffFiles.mockResolvedValue({
      payload: {
        in_git_repo: true,
        branch: "main",
        ahead: 0,
        behind: 0,
        truncated: false,
        total_count: 1,
        files: [
          {
            path: "handwrite_res/nested/result.txt",
            status: "untracked",
          },
        ],
      },
    });
    mockGatewayAPI.readGitDiffFile.mockResolvedValue({
      payload: {
        path: "handwrite_res/nested/result.txt",
        status: "untracked",
        original_content: "",
        modified_content: "expanded",
        size_original: 0,
        size_modified: 8,
      },
    });
    useUIStore.setState({
      gitDiffSummary: {
        in_git_repo: true,
        branch: "main",
        ahead: 0,
        behind: 0,
        truncated: false,
        total_count: 1,
        files: [
          {
            path: "handwrite_res/nested/result.txt",
            status: "untracked",
          },
        ],
      },
    } as never);

    render(<FileChangePanel />);

    fireEvent.click(
      screen.getByTestId(`preview-tab-${GIT_DIFF_PREVIEW_TAB_ID}`),
    );
    fireEvent.click(
      await screen.findByTestId(
        "git-diff-entry-handwrite_res/nested/result.txt",
      ),
    );

    await waitFor(() => {
      expect(mockGatewayAPI.readGitDiffFile).toHaveBeenCalledWith({
        path: "handwrite_res/nested/result.txt",
      });
    });
  });

  it("renders the Monaco-based preview host for loaded text files", async () => {
    useUIStore.setState({
      previewTabs: [
        {
          id: CHANGES_PREVIEW_TAB_ID,
          kind: "changes",
          title: "文件变更",
          closable: false,
        },
        {
          id: GIT_DIFF_PREVIEW_TAB_ID,
          kind: "git-diff",
          title: "Git Diff",
          closable: false,
        },
        {
          id: "file:cmd/neocode/main.go",
          kind: "file",
          title: "main.go",
          closable: true,
          path: "cmd/neocode/main.go",
          content: "package main",
          loading: false,
          loaded: true,
          error: "",
          truncated: false,
          is_binary: false,
        },
      ],
      activePreviewTabId: "file:cmd/neocode/main.go",
      theme: "light",
    } as never);

    render(<FileChangePanel />);

    const preview = await screen.findByTestId("code-preview-editor");
    expect(preview).toHaveAttribute("data-path", "cmd/neocode/main.go");
    expect(preview).toHaveAttribute("data-theme", "light");
    expect(preview.textContent).toContain("package main");
    const fileHeader = screen.getByTestId("file-preview-header");
    const filePath = within(fileHeader).getByTestId("file-preview-path");
    expect(filePath.textContent).toBe("cmd/neocode/main.go");
    expect(filePath).toHaveAttribute("title", "cmd/neocode/main.go");
    expect(filePath).toHaveStyle({
      whiteSpace: "nowrap",
      overflow: "hidden",
      textOverflow: "ellipsis",
    });
    expect(fileHeader.textContent?.trim()).toBe("cmd/neocode/main.go");
  });

  it("uses theme variables instead of fixed diff colors in light mode", () => {
    useUIStore.setState({ theme: "light" } as never);

    render(<FileChangePanel />);

    fireEvent.click(screen.getByText("src/a.txt"));

    expect(screen.getByText("line 2 new").parentElement).toHaveStyle({
      color: "var(--diff-add-text)",
      background: "var(--diff-add-bg)",
    });
    expect(screen.getByText("line 2 old").parentElement).toHaveStyle({
      color: "var(--diff-del-text)",
      background: "var(--diff-del-bg)",
    });
    expect(screen.getByText("@@ -1,3 +1,3 @@").parentElement).toHaveStyle({
      color: "var(--diff-header-text)",
      background: "var(--diff-header-bg)",
    });
  });
});
