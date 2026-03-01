import { useState, useEffect, useRef, type CSSProperties, type KeyboardEvent } from 'react'
import { context, propagation, trace, SpanStatusCode, SpanKind } from '@opentelemetry/api'
import { tracer } from './tracing'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8081'
const WS_URL = import.meta.env.VITE_WS_URL || 'ws://localhost:8082'

export default function App() {
  const [inputText, setInputText] = useState('')
  const [messages, setMessages] = useState<string[]>([])
  const [wsStatus, setWsStatus] = useState('Connecting...')
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    connectWS()
    return () => {
      if (wsRef.current) wsRef.current.close()
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
    }
  }, [])

  function connectWS() {
    const ws = new WebSocket(`${WS_URL}/ws`)
    wsRef.current = ws

    ws.onopen = () => setWsStatus('Connected')
    ws.onclose = () => {
      setWsStatus('Reconnecting...')
      reconnectTimerRef.current = setTimeout(connectWS, 3000)
    }
    ws.onerror = () => setWsStatus('Error')
    ws.onmessage = (event: MessageEvent) => {
      const data = typeof event.data === 'string' ? event.data : ''
      let body = data
      let ctx = context.active()

      try {
        const parsed = JSON.parse(data) as { traceparent?: string; tracestate?: string; body?: string }
        if (parsed.traceparent && parsed.body !== undefined) {
          body = parsed.body
          const carrier: Record<string, string> = {}
          if (parsed.traceparent) carrier.traceparent = parsed.traceparent
          if (parsed.tracestate) carrier.tracestate = parsed.tracestate
          ctx = propagation.extract(ctx, carrier)
        }
      } catch {
        /* plain text message, no trace */
      }

      context.with(ctx, () => {
        const span = tracer.startSpan('receive message', {
          kind: SpanKind.CONSUMER,
          attributes: {
            'message.content': body,
            'messaging.operation': 'receive',
          },
        })
        span.setStatus({ code: SpanStatusCode.OK })
        span.end()
      })
      setMessages((prev: string[]) => [...prev, body])
    }
  }

  async function sendToEndpoint(endpoint: string, spanName: string) {
    const text = inputText.trim()
    if (!text) return

    const url = `${API_URL}${endpoint}`
    const span = tracer.startSpan(spanName, {
      kind: SpanKind.CLIENT,
      attributes: {
        'message.content': text,
        'http.request.method': 'POST',
        'url.full': url,
      },
    })
    const ctx = trace.setSpan(context.active(), span)

    try {
      const headers: Record<string, string> = { 'Content-Type': 'application/json' }
      propagation.inject(ctx, headers)

      const res = await fetch(url, {
        method: 'POST',
        headers,
        body: JSON.stringify({ text }),
      })
      span.setAttribute('http.response.status_code', res.status)
      if (!res.ok) throw new Error('Failed to send')
      span.setStatus({ code: SpanStatusCode.OK })
      setInputText('')
    } catch (err) {
      const error = err instanceof Error ? err : new Error(String(err))
      span.setStatus({ code: SpanStatusCode.ERROR, message: error.message })
      span.recordException(error)
      alert(`Error: ${error.message}`)
    } finally {
      span.end()
    }
  }

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') sendToEndpoint('/api/message', 'send-message')
  }

  return (
    <div style={styles.container}>
      <h1 style={styles.title}>Message Demo</h1>
      <p style={styles.status}>WebSocket: <strong>{wsStatus}</strong></p>
      <div style={styles.inputRow}>
        <input
          style={styles.input}
          type="text"
          placeholder="Enter a message..."
          value={inputText}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setInputText(e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </div>
      <div style={styles.buttonRow}>
        <button
          style={styles.button}
          onClick={() => sendToEndpoint('/api/message', 'send-message')}
          title="nats.JetStreamContext (內建)"
        >
          送出（nats 內建 JetStreamContext）
        </button>
        <button
          style={{ ...styles.button, ...styles.buttonSecondary }}
          onClick={() => sendToEndpoint('/api/message-v2', 'send-message-v2')}
          title="jetstream 套件 Publisher 介面"
        >
          送出（jetstream 套件 Publisher）
        </button>
        <button
          style={{ ...styles.button, ...styles.buttonTertiary }}
          onClick={() => sendToEndpoint('/api/message-core', 'send-message-core')}
          title="Core NATS nc.Publish（非 JetStream）"
        >
          送出（Core NATS）
        </button>
      </div>
      <textarea
        style={styles.textarea}
        readOnly
        value={messages.join('\n')}
        placeholder="Messages will appear here..."
      />
    </div>
  )
}

const styles: Record<string, CSSProperties> = {
  container: {
    maxWidth: '600px',
    margin: '40px auto',
    fontFamily: 'sans-serif',
    padding: '0 16px',
  },
  title: { marginBottom: '8px' },
  status: { marginBottom: '16px', color: '#555' },
  inputRow: { display: 'flex', gap: '8px', marginBottom: '8px' },
  buttonRow: { display: 'flex', gap: '8px', marginBottom: '16px', flexWrap: 'wrap' as const },
  input: {
    flex: 1,
    padding: '8px 12px',
    fontSize: '16px',
    border: '1px solid #ccc',
    borderRadius: '4px',
  },
  button: {
    padding: '8px 16px',
    fontSize: '14px',
    backgroundColor: '#4f46e5',
    color: '#fff',
    border: 'none',
    borderRadius: '4px',
    cursor: 'pointer',
  },
  buttonSecondary: {
    backgroundColor: '#0d9488',
  },
  buttonTertiary: {
    backgroundColor: '#b45309',
  },
  textarea: {
    width: '100%',
    height: '300px',
    padding: '8px 12px',
    fontSize: '15px',
    border: '1px solid #ccc',
    borderRadius: '4px',
    resize: 'vertical',
    boxSizing: 'border-box',
  },
}
