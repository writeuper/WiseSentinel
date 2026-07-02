import {
  BookOutlined,
  SendOutlined,
  ToolOutlined,
} from '@ant-design/icons';
import { Button, Checkbox, Input, Tag, Typography } from 'antd';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import PageTopbar from '@/components/PageTopbar';
import {
  createSession,
  getMessages,
  sendChat,
  sendChatStream,
  type StreamEventCallback,
} from '@/api/client';
import type { CitationItem, MessageItem, ToolCallSummary } from '@/api/types';

/** Local chat message with display metadata. */
interface ChatMessage {
  role: 'user' | 'assistant' | 'system';
  content: string;
  citations?: CitationItem[];
  toolCalls?: ToolCallSummary[];
  timestamp: string;
}

export default function ChatPage() {
  const [searchParams] = useSearchParams();

  // Session state
  const [sessionId, setSessionId] = useState<string | null>(() => {
    // Load from URL query param if present
    return searchParams.get('session') || null;
  });
  const [sessionTitle, setSessionTitle] = useState('新对话');

  // Messages
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const [streaming, setStreaming] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Options
  const [enableRag, setEnableRag] = useState(true);
  const [enableTools, setEnableTools] = useState(true);
  const [useStream, setUseStream] = useState(true);

  // Refs
  const abortRef = useRef<AbortController | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  // Auto-scroll
  const scrollToBottom = useCallback(() => {
    setTimeout(() => {
      scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' });
    }, 50);
  }, []);

  // Load messages when session changes
  useEffect(() => {
    if (!sessionId) return;
    (async () => {
      try {
        const data = await getMessages(sessionId);
        const msgs: ChatMessage[] = data.messages.map((m: MessageItem) => ({
          role: m.role as 'user' | 'assistant',
          content: m.content,
          timestamp: m.timestamp,
        }));
        setMessages(msgs);
        scrollToBottom();
      } catch {
        // Session may not have messages yet
      }
    })();
  }, [sessionId, scrollToBottom]);

  // Ensure a session exists
  const ensureSession = useCallback(async (title?: string): Promise<string> => {
    if (sessionId) return sessionId;
    const data = await createSession(title || '新对话');
    setSessionId(data.session_id);
    setSessionTitle(data.title);
    return data.session_id;
  }, [sessionId]);

  // Create a new session
  const handleNewChat = useCallback(async () => {
    if (abortRef.current) abortRef.current.abort();
    setMessages([]);
    setSessionId(null);
    setSessionTitle('新对话');
    setError(null);
    setSending(false);
    setStreaming(false);
    inputRef.current?.focus();
  }, []);

  // Send a message
  const handleSend = useCallback(async () => {
    const text = input.trim();
    if (!text || sending || streaming) return;

    setInput('');
    setError(null);

    // Add user message immediately
    const userMsg: ChatMessage = {
      role: 'user',
      content: text,
      timestamp: new Date().toISOString(),
    };
    setMessages((prev) => [...prev, userMsg]);
    scrollToBottom();

    // Ensure session
    let sid: string;
    try {
      sid = await ensureSession(text.slice(0, 30));
    } catch (err: any) {
      setError(`创建会话失败: ${err.message}`);
      return;
    }

    setSending(true);

    if (useStream) {
      // Streaming mode
      setStreaming(true);
      const assistantMsg: ChatMessage = {
        role: 'assistant',
        content: '',
        timestamp: new Date().toISOString(),
      };
      setMessages((prev) => [...prev, assistantMsg]);
      scrollToBottom();

      const callbacks: StreamEventCallback = {
        onMessage: (chunk) => {
          setMessages((prev) => {
            const updated = [...prev];
            const last = updated[updated.length - 1];
            if (last && last.role === 'assistant') {
              updated[updated.length - 1] = { ...last, content: last.content + chunk };
            }
            return updated;
          });
          scrollToBottom();
        },
        onCitation: (citation) => {
          setMessages((prev) => {
            const updated = [...prev];
            const last = updated[updated.length - 1];
            if (last && last.role === 'assistant') {
              const citations = last.citations || [];
              citations.push(citation);
              updated[updated.length - 1] = { ...last, citations };
            }
            return updated;
          });
        },
        onToolStart: (data) => {
          setMessages((prev) => {
            const updated = [...prev];
            const last = updated[updated.length - 1];
            if (last && last.role === 'assistant') {
              const toolCalls = last.toolCalls || [];
              toolCalls.push({ tool: data, status: 'running' });
              updated[updated.length - 1] = { ...last, toolCalls };
            }
            return updated;
          });
        },
        onToolEnd: (data) => {
          try {
            const parsed = JSON.parse(data);
            setMessages((prev) => {
              const updated = [...prev];
              const last = updated[updated.length - 1];
              if (last && last.role === 'assistant') {
                const toolCalls = last.toolCalls || [];
                for (let i = toolCalls.length - 1; i >= 0; i--) {
                  if (toolCalls[i].tool === parsed.tool || toolCalls[i].status === 'running') {
                    toolCalls[i] = { ...toolCalls[i], status: parsed.status || 'success' };
                    break;
                  }
                }
                updated[updated.length - 1] = { ...last, toolCalls };
              }
              return updated;
            });
          } catch { /* ignore parse errors */ }
        },
        onError: (errMsg) => {
          setError(errMsg);
          setStreaming(false);
          setSending(false);
        },
        onDone: () => {
          setStreaming(false);
          setSending(false);
        },
      };

      abortRef.current = sendChatStream(sid, text, { enable_rag: enableRag, enable_tools: enableTools }, callbacks);
    } else {
      // Synchronous mode
      try {
        const result = await sendChat(sid, text, { enable_rag: enableRag, enable_tools: enableTools });
        const assistantMsg: ChatMessage = {
          role: 'assistant',
          content: result.answer,
          citations: result.citations || [],
          toolCalls: result.tool_calls || [],
          timestamp: new Date().toISOString(),
        };
        setMessages((prev) => [...prev, assistantMsg]);
        scrollToBottom();

        // Update session title from first interaction
        if (result.answer) {
          setSessionTitle(text.slice(0, 30) + (text.length > 30 ? '...' : ''));
        }
      } catch (err: any) {
        setError(err.message || '请求失败');
      } finally {
        setSending(false);
      }
    }
  }, [input, sending, streaming, sessionId, ensureSession, useStream, enableRag, enableTools, scrollToBottom]);

  // Handle Enter key
  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }, [handleSend]);

  return (
    <div className="page-shell">
      <PageTopbar
        title={sessionTitle}
        tags={[
          sessionId ? `session: ${sessionId.slice(0, 8)}...` : '新对话',
          useStream ? '流式' : '同步',
          `RAG: ${enableRag ? '开' : '关'}`,
          `工具: ${enableTools ? '开' : '关'}`,
        ]}
      />

      <div className="chat-scroll" ref={scrollRef}>
        <div className="chat-inner">
          {messages.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '60px 20px' }}>
              <Typography.Title level={3}>有什么可以帮您？</Typography.Title>
              <Typography.Paragraph type="secondary">
                智哨可检索 Runbook、查询告警与日志，辅助 OnCall 诊断
              </Typography.Paragraph>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, justifyContent: 'center', marginTop: 24 }}>
                {[
                  '服务下线告警怎么处理？',
                  'Pod CrashLoopBackOff 怎么排查？',
                  'Redis 连接超时诊断',
                ].map((p) => (
                  <Button
                    key={p}
                    shape="round"
                    onClick={() => {
                      setInput(p);
                      inputRef.current?.focus();
                    }}
                  >
                    {p}
                  </Button>
                ))}
              </div>
            </div>
          ) : (
            messages.map((msg, i) => (
              <div key={i} style={{ marginBottom: 20 }}>
                {msg.role === 'user' ? (
                  <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                    <div className="user-bubble">{msg.content}</div>
                  </div>
                ) : (
                  <div className="ai-bubble">
                    <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', margin: 0 }}>
                      {msg.content || (streaming && i === messages.length - 1 ? (
                        <Typography.Text type="secondary">思考中...</Typography.Text>
                      ) : msg.content)}
                    </Typography.Paragraph>

                    {/* Citations */}
                    {msg.citations && msg.citations.length > 0 && (
                      <div style={{ marginTop: 12 }}>
                        {msg.citations.map((c, ci) => (
                          <div key={ci} className="citation-card">
                            <strong style={{ color: '#166534' }}>
                              <BookOutlined style={{ marginRight: 4 }} />
                              {c.source || '文档'}
                            </strong>
                            <div style={{ fontSize: 13, marginTop: 4 }}>{c.snippet}</div>
                          </div>
                        ))}
                      </div>
                    )}

                    {/* Tool calls */}
                    {msg.toolCalls && msg.toolCalls.length > 0 && (
                      <div style={{ marginTop: 8 }}>
                        {msg.toolCalls.map((tc, ti) => (
                          <div key={ti} className="tool-call-bar">
                            <ToolOutlined style={{ marginRight: 6 }} />
                            {tc.tool}
                            <Tag
                              color={tc.status === 'success' ? 'success' : tc.status === 'running' ? 'processing' : 'error'}
                              style={{ marginLeft: 8 }}
                            >
                              {tc.status === 'running' ? '运行中...' : tc.status}
                            </Tag>
                            {tc.latency_ms ? <Tag>{tc.latency_ms}ms</Tag> : null}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
            ))
          )}

          {/* Error display */}
          {error && (
            <div style={{ padding: '12px 16px', background: '#fef2f2', border: '1px solid #fecaca', borderRadius: 8, marginBottom: 16 }}>
              <Typography.Text type="danger">{error}</Typography.Text>
            </div>
          )}
        </div>
      </div>

      {/* Input area */}
      <div style={{ padding: '16px 24px 24px', display: 'flex', justifyContent: 'center' }}>
        <div
          style={{
            width: '100%',
            maxWidth: 768,
            background: '#fff',
            border: '1px solid #e2e8f0',
            borderRadius: 12,
            padding: '12px 16px',
          }}
        >
          <Input.TextArea
            ref={inputRef as any}
            placeholder="输入问题，Enter 发送…"
            autoSize={{ minRows: 2, maxRows: 6 }}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            disabled={sending || streaming}
          />
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              marginTop: 10,
              paddingTop: 10,
              borderTop: '1px solid #e2e8f0',
            }}
          >
            <div style={{ display: 'flex', gap: 16, fontSize: 12, color: '#64748b', alignItems: 'center' }}>
              <Checkbox checked={enableRag} onChange={(e) => setEnableRag(e.target.checked)}>
                RAG 知识库
              </Checkbox>
              <Checkbox checked={enableTools} onChange={(e) => setEnableTools(e.target.checked)}>
                工具调用
              </Checkbox>
              <Checkbox checked={useStream} onChange={(e) => setUseStream(e.target.checked)}>
                流式
              </Checkbox>
              <Button size="small" type="link" onClick={handleNewChat}>
                新对话
              </Button>
            </div>
            <Button
              type="primary"
              icon={<SendOutlined />}
              onClick={handleSend}
              loading={sending || streaming}
              disabled={!input.trim()}
            >
              {streaming ? '生成中' : sending ? '发送中' : '发送'}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}