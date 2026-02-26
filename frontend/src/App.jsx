import { useState, useEffect, useRef } from 'react'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8081'
const WS_URL = import.meta.env.VITE_WS_URL || 'ws://localhost:8082'

export default function App() {
  const [inputText, setInputText] = useState('')
  const [messages, setMessages] = useState([])
  const [wsStatus, setWsStatus] = useState('Connecting...')
  const wsRef = useRef(null)
  const reconnectTimerRef = useRef(null)

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
    ws.onmessage = (event) => {
      setMessages((prev) => [...prev, event.data])
    }
  }

  async function handleSend() {
    const text = inputText.trim()
    if (!text) return
    try {
      const res = await fetch(`${API_URL}/api/message`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text }),
      })
      if (!res.ok) throw new Error('Failed to send')
      setInputText('')
    } catch (err) {
      alert(`Error: ${err.message}`)
    }
  }

  function handleKeyDown(e) {
    if (e.key === 'Enter') handleSend()
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
          onChange={(e) => setInputText(e.target.value)}
          onKeyDown={handleKeyDown}
        />
        <button style={styles.button} onClick={handleSend}>
          Send
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

const styles = {
  container: {
    maxWidth: '600px',
    margin: '40px auto',
    fontFamily: 'sans-serif',
    padding: '0 16px',
  },
  title: { marginBottom: '8px' },
  status: { marginBottom: '16px', color: '#555' },
  inputRow: { display: 'flex', gap: '8px', marginBottom: '16px' },
  input: {
    flex: 1,
    padding: '8px 12px',
    fontSize: '16px',
    border: '1px solid #ccc',
    borderRadius: '4px',
  },
  button: {
    padding: '8px 20px',
    fontSize: '16px',
    backgroundColor: '#4f46e5',
    color: '#fff',
    border: 'none',
    borderRadius: '4px',
    cursor: 'pointer',
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
