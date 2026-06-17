import { memo, useEffect, useState } from "react";
import { type ChatMessage, type ChatAttachment } from "@/stores/useChatStore";
import { type PlanArtifact } from "@/api/protocol";
import { useGatewayAPI } from "@/context/RuntimeProvider";
import { formatBytes } from "@/utils/format";
import ToolCallCard from "./ToolCallCard";
import AcceptanceMessage from "./AcceptanceMessage";
import CodeBlock from "./CodeBlock";
import MarkdownContent from "./MarkdownContent";
import { Bot, ChevronRight, ClipboardList, FileText, Info, Loader2 } from "lucide-react";

interface MessageItemProps {
  message: ChatMessage;
  isLast?: boolean;
  /** 是否与上一条 AI/工具消息属于同一回合（同回合压缩 avatar 与上下间距） */
  groupedWithPrev?: boolean;
}

/** 单条消息渲染 */
const MessageItem = memo(function MessageItem({
  message,
  isLast = false,
  groupedWithPrev = false,
}: MessageItemProps) {
  if (message.type === "system") {
    return <SystemMessage message={message} />;
  }

  if (message.type === "welcome") {
    return <WelcomeMessage message={message} />;
  }

  if (message.type === "thinking") {
    return (
      <ThinkingMessage message={message} groupedWithPrev={groupedWithPrev} />
    );
  }

  if (message.type === "tool_call") {
    return <ToolCallCard message={message} groupedWithPrev={groupedWithPrev} />;
  }

  if (message.type === "acceptance") {
    return (
      <AcceptanceMessage message={message} groupedWithPrev={groupedWithPrev} />
    );
  }

  if (message.type === "plan") {
    return <PlanMessage message={message} groupedWithPrev={groupedWithPrev} />;
  }

  if (message.type === "code") {
    return (
      <AIMessage
        message={message}
        isLast={isLast}
        groupedWithPrev={groupedWithPrev}
      >
        <CodeBlock
          code={message.content}
          language={message.language || "text"}
          filename={message.filename}
        />
      </AIMessage>
    );
  }

  if (message.role === "user") {
    return <UserMessage message={message} />;
  }

  return (
    <AIMessage
      message={message}
      isLast={isLast}
      groupedWithPrev={groupedWithPrev}
    />
  );
});

function UserMessage({ message }: { message: ChatMessage }) {
  return (
    <div style={styles.userRow} className="animate-slide-up user-row-hoverable">
      <div style={styles.userContent}>
        <div style={styles.userBubble}>
          {message.content && <div>{message.content}</div>}
          <UserAttachments message={message} />
        </div>
      </div>
    </div>
  );
}

// formatBytes 已提取到 @/utils/format，composer 与消息历史共用同一实现。

// isImageAttachment 判定附件是否为图片类：优先使用显式 kind，缺省时按 mimeType 前缀推断（向后兼容）。
function isImageAttachment(attachment: ChatAttachment): boolean {
  if (attachment.kind) return attachment.kind === "image";
  return (attachment.mimeType || "").toLowerCase().startsWith("image/");
}

function UserAttachments({ message }: { message: ChatMessage }) {
  const gatewayAPI = useGatewayAPI();
  const [loadedURLs, setLoadedURLs] = useState<Record<string, string>>({});
  const attachments = message.attachments || [];

  useEffect(() => {
    if (!gatewayAPI || attachments.length === 0) return;
    let cancelled = false;
    const created: string[] = [];
    attachments.forEach((attachment) => {
      // 文本附件 chip 只展示文件名+大小，无需回载 blob；仅图片附件需要 fetch。
      if (!isImageAttachment(attachment)) return;
      if (attachment.previewUrl || !attachment.sessionId || !attachment.assetId) return;
      gatewayAPI.fetchSessionAsset(attachment.sessionId, attachment.assetId, attachment.workspaceHash)
        .then((blob) => {
          if (cancelled) return;
          const url = URL.createObjectURL(blob);
          created.push(url);
          setLoadedURLs((current) => ({ ...current, [attachment.id]: url }));
        })
        .catch(() => {});
    });
    return () => {
      cancelled = true;
      created.forEach((url) => URL.revokeObjectURL(url));
    };
  }, [attachments, gatewayAPI]);

  if (attachments.length === 0) return null;
  return (
    <div style={styles.userAttachmentGrid}>
      {attachments.map((attachment) => {
        // 文本附件以 chip 形式展示文件名和大小，与图片缩略图区分。
        if (!isImageAttachment(attachment)) {
          return (
            <div key={attachment.id} style={styles.userTextChip}>
              <FileText size={14} style={{ flexShrink: 0 }} />
              <span style={styles.userTextChipName} title={attachment.name || ""}>
                {attachment.name || "text file"}
              </span>
              <span style={styles.userTextChipSize}>{formatBytes(attachment.size)}</span>
            </div>
          );
        }
        const src = attachment.previewUrl || loadedURLs[attachment.id] || "";
        return (
          <div key={attachment.id} style={styles.userAttachmentThumb}>
            {src ? (
              <img src={src} alt={attachment.name || "uploaded image"} style={styles.userAttachmentImage} />
            ) : (
              <div style={styles.userAttachmentPlaceholder}>image</div>
            )}
          </div>
        );
      })}
    </div>
  );
}

function AIMessage({
  message,
  isLast,
  children,
  groupedWithPrev = false,
}: {
  message: ChatMessage;
  isLast: boolean;
  children?: React.ReactNode;
  groupedWithPrev?: boolean;
}) {
  return (
    <div
      style={groupedWithPrev ? styles.aiRowGrouped : styles.aiRow}
      className="animate-slide-up"
    >
      {groupedWithPrev ? (
        <div style={styles.avatarSpacer} aria-hidden />
      ) : (
        <div style={styles.aiAvatar}>
          <Bot size={16} />
        </div>
      )}
      <div style={styles.aiContent}>
        {children || (
          <div style={styles.aiText}>
            <MarkdownContent
              content={message.content}
              streaming={message.streaming}
            />
            {isLast && !message.content && message.streaming && (
              <span style={styles.typing}>
                <span className="thinking-dot">.</span>
                <span className="thinking-dot">.</span>
                <span className="thinking-dot">.</span>
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function ThinkingMessage({
  message,
  groupedWithPrev = false,
}: {
  message: ChatMessage;
  groupedWithPrev?: boolean;
}) {
  const collapsed = message.thinkingData?.collapsed ?? false;
  const isStreaming = message.streaming === true;
  const [manualExpanded, setManualExpanded] = useState<boolean | null>(null);

  // streaming 时展开，collapsed 且无手动覆盖时折叠
  const expanded =
    manualExpanded !== null ? manualExpanded : isStreaming || !collapsed;

  return (
    <div
      style={groupedWithPrev ? styles.aiRowGrouped : styles.aiRow}
      className="animate-fade-in"
    >
      {groupedWithPrev ? (
        <div style={styles.avatarSpacer} aria-hidden />
      ) : (
        <div
          style={{
            ...styles.aiAvatar,
            background: "var(--warning)",
            color: "#fff",
          }}
        >
          <Bot size={16} />
        </div>
      )}
      <div style={styles.aiContent}>
        <button
          style={styles.thinkingToggle}
          onClick={() => setManualExpanded(!expanded)}
        >
          <span
            style={{
              transform: expanded ? "rotate(90deg)" : "rotate(0deg)",
              transition: "transform 0.2s",
              display: "flex",
            }}
          >
            <ChevronRight size={14} />
          </span>
          <span style={styles.thinkingLabel}>
            {isStreaming ? "AI 正在思考..." : "AI 思考过程"}
          </span>
          {isStreaming && (
            <Loader2
              size={12}
              className="animate-spin"
              style={{ marginLeft: 4 }}
            />
          )}
        </button>
        {expanded && (
          <div style={styles.thinkingContent}>
            <pre
              style={{
                margin: 0,
                fontFamily: "var(--font-mono)",
                fontSize: 12,
                lineHeight: 1.7,
                whiteSpace: "pre-wrap",
              }}
            >
              {message.content}
            </pre>
          </div>
        )}
      </div>
    </div>
  );
}

function PlanMessage({
  message,
  groupedWithPrev = false,
}: {
  message: ChatMessage;
  groupedWithPrev?: boolean;
}) {
  const plan = message.planData;
  if (!plan) {
    return (
      <AIMessage
        message={{ ...message, type: "text" }}
        isLast={false}
        groupedWithPrev={groupedWithPrev}
      />
    );
  }
  const spec = plan.spec || { goal: "" };
  const steps = listOf(spec.steps);
  const legacyTodoSteps =
    steps.length > 0 ? steps : listOf(spec.todos?.map((todo) => todo.content));
  const constraints = listOf(spec.constraints);
  const questions = listOf(spec.open_questions);

  return (
    <div
      style={groupedWithPrev ? styles.aiRowGrouped : styles.aiRow}
      className="animate-slide-up"
    >
      {groupedWithPrev ? (
        <div style={styles.avatarSpacer} aria-hidden />
      ) : (
        <div
          style={{
            ...styles.aiAvatar,
            background: "var(--warning-muted)",
            color: "var(--warning)",
          }}
        >
          <ClipboardList size={16} />
        </div>
      )}
      <div style={styles.aiContent}>
        <div style={styles.planCard}>
          <div style={styles.planHeader}>
            <span style={styles.planTitle}>计划</span>
            <span style={styles.planMeta}>
              rev {plan.revision} · {plan.status || "draft"}
            </span>
          </div>
          {spec.goal && <div style={styles.planGoal}>{spec.goal}</div>}
          <PlanSection title="实施步骤" items={legacyTodoSteps} />
          <PlanSection title="约束" items={constraints} />
          <PlanSection title="未决问题" items={questions} />
        </div>
      </div>
    </div>
  );
}

function PlanSection({ title, items }: { title: string; items: string[] }) {
  if (items.length === 0) return null;
  return (
    <div style={styles.planSection}>
      <div style={styles.planSectionTitle}>{title}</div>
      <ul style={styles.planList}>
        {items.map((item, index) => (
          <li key={`${title}-${index}`} style={styles.planListItem}>
            {item}
          </li>
        ))}
      </ul>
    </div>
  );
}

function listOf(value: PlanArtifact["spec"]["steps"]): string[] {
  return Array.isArray(value)
    ? value
        .filter((item) => typeof item === "string" && item.trim())
        .map((item) => item.trim())
    : [];
}

function SystemMessage({ message }: { message: ChatMessage }) {
  return (
    <div style={styles.systemRow} className="animate-fade-in">
      <div style={styles.systemBadge}>
        <Info size={12} />
        <span style={styles.systemLabel}>系统</span>
      </div>
      <pre style={styles.systemPre}>{message.content}</pre>
    </div>
  );
}

function WelcomeMessage({ message }: { message: ChatMessage }) {
  return (
    <div
      style={{ ...styles.aiRow, justifyContent: "center" }}
      className="animate-slide-up"
    >
      <div style={styles.welcomeCard}>
        <div style={styles.welcomeIcon}>NeoCode</div>
        <p style={styles.welcomeText}>{message.content}</p>
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  userRow: {
    display: "flex",
    justifyContent: "flex-end",
    alignItems: "flex-start",
    padding: "12px 0 10px",
    position: "relative",
    gap: 6,
  },
  userContent: {
    maxWidth: "85%",
    minWidth: 0,
  },
  userBubble: {
    background: "var(--user-bubble)",
    color: "var(--user-bubble-text)",
    padding: "10px 14px",
    borderRadius: "var(--radius-lg)",
    fontSize: 14,
    lineHeight: 1.6,
    border: "1px solid var(--border-primary)",
    maxWidth: "100%",
    whiteSpace: "pre-wrap",
    overflowWrap: "anywhere",
    wordBreak: "break-word",
    textWrap: "pretty" as any,
  },
  userAttachmentGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fill, minmax(96px, 1fr))",
    gap: 8,
    marginTop: 8,
    maxWidth: 340,
  },
  userAttachmentThumb: {
    width: 96,
    height: 72,
    borderRadius: "var(--radius-md)",
    border: "1px solid var(--border-primary)",
    background: "var(--bg-secondary)",
    overflow: "hidden",
  },
  userAttachmentImage: {
    width: "100%",
    height: "100%",
    objectFit: "cover",
    display: "block",
  },
  userAttachmentPlaceholder: {
    width: "100%",
    height: "100%",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    color: "var(--text-tertiary)",
    fontSize: 11,
    fontFamily: "var(--font-mono)",
  },
  // 文本附件 chip：展示文件名+大小，与图片缩略图视觉区分。
  userTextChip: {
    display: "flex",
    alignItems: "center",
    gap: 6,
    padding: "6px 10px",
    borderRadius: "var(--radius-md)",
    border: "1px solid var(--border-primary)",
    background: "var(--bg-secondary)",
    color: "var(--text-primary)",
    fontSize: 12,
    overflow: "hidden",
  },
  userTextChipName: {
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  userTextChipSize: {
    flexShrink: 0,
    color: "var(--text-tertiary)",
    fontSize: 11,
  },
  aiRow: {
    display: "flex",
    gap: 10,
    padding: "8px 0 10px",
  },
  aiRowGrouped: {
    display: "flex",
    gap: 10,
    padding: "2px 0",
  },
  avatarSpacer: {
    width: 28,
    flexShrink: 0,
  },
  aiAvatar: {
    width: 28,
    height: 28,
    borderRadius: "var(--radius-md)",
    background: "var(--accent-muted)",
    color: "var(--accent)",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    flexShrink: 0,
    marginTop: 2,
  },
  aiContent: {
    flex: 1,
    minWidth: 0,
  },
  aiText: {
    fontSize: 14,
    lineHeight: 1.7,
    color: "var(--text-primary)",
    textWrap: "pretty" as any,
  },
  typing: {
    marginLeft: 4,
    color: "var(--text-tertiary)",
  },
  thinkingToggle: {
    display: "flex",
    alignItems: "center",
    gap: 6,
    padding: "4px 8px",
    borderRadius: "var(--radius-sm)",
    border: "none",
    background: "var(--bg-tertiary)",
    color: "var(--text-secondary)",
    fontSize: 12,
    cursor: "pointer",
    fontFamily: "var(--font-ui)",
    marginBottom: 8,
  },
  thinkingLabel: {
    fontWeight: 500,
  },
  thinkingContent: {
    padding: "10px 12px",
    borderRadius: "var(--radius-md)",
    background: "var(--bg-tertiary)",
    color: "var(--text-secondary)",
    marginBottom: 8,
  },
  planCard: {
    border: "1px solid var(--border-primary)",
    borderRadius: "var(--radius-md)",
    background: "var(--bg-secondary)",
    padding: "12px 14px",
    maxWidth: 720,
  },
  planHeader: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
    marginBottom: 8,
  },
  planTitle: {
    fontSize: 13,
    fontWeight: 700,
    color: "var(--warning)",
  },
  planMeta: {
    fontSize: 11,
    color: "var(--text-tertiary)",
    fontFamily: "var(--font-mono)",
    whiteSpace: "nowrap",
  },
  planGoal: {
    fontSize: 14,
    lineHeight: 1.6,
    color: "var(--text-primary)",
    marginBottom: 10,
    overflowWrap: "anywhere",
  },
  planSection: {
    marginTop: 10,
  },
  planSectionTitle: {
    fontSize: 12,
    fontWeight: 700,
    color: "var(--text-secondary)",
    marginBottom: 4,
  },
  planList: {
    margin: 0,
    paddingLeft: 18,
    color: "var(--text-primary)",
    fontSize: 13,
    lineHeight: 1.65,
  },
  planListItem: {
    margin: "2px 0",
    overflowWrap: "anywhere",
  },
  welcomeCard: {
    display: "flex",
    flexDirection: "column",
    alignItems: "center",
    gap: 12,
    padding: "24px 32px",
    textAlign: "center",
    maxWidth: 480,
  },
  welcomeIcon: {
    width: 48,
    height: 48,
    borderRadius: "var(--radius-xl)",
    background: "var(--accent-muted)",
    color: "var(--accent)",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    fontSize: 11,
    fontWeight: 700,
    fontFamily: "var(--font-mono)",
  },
  welcomeText: {
    fontSize: 14,
    lineHeight: 1.7,
    color: "var(--text-secondary)",
    margin: 0,
  },
  systemRow: {
    display: "flex",
    flexDirection: "column",
    alignItems: "center",
    gap: 6,
    padding: "10px 16px",
    margin: "4px 0",
  },
  systemBadge: {
    display: "flex",
    alignItems: "center",
    gap: 4,
    padding: "3px 10px",
    borderRadius: "var(--radius-md)",
    background: "var(--bg-tertiary)",
    color: "var(--text-tertiary)",
    fontSize: 11,
    fontWeight: 600,
  },
  systemLabel: {
    fontSize: 11,
    fontWeight: 600,
    letterSpacing: "0.5px",
  },
  systemPre: {
    fontSize: 13,
    lineHeight: 1.6,
    color: "var(--text-secondary)",
    textAlign: "left",
    maxWidth: "85%",
    whiteSpace: "pre-wrap",
    fontFamily: "var(--font-mono)",
    margin: 0,
  },
};

export default MessageItem;
