import React, { useState, useEffect, useRef, useCallback } from 'react'
import './style.css'
import { StartBatch, SubmitCaptcha, SaveZip } from '../wailsjs/go/main/App'
import { EventsOn, Quit, WindowMinimise, WindowToggleMaximise } from '../wailsjs/runtime/runtime'

// ─── Types ────────────────────────────────────────────────────────────────────
interface LogEntry {
  id: number
  level: 'info' | 'success' | 'error' | 'warn'
  msg: string
  time: string
}

interface ProgressPayload {
  done: number
  total: number
  failed: number
  current: string
}

interface CaptchaPayload {
  usn: string
  imgB64: string
}

interface DonePayload {
  zipPath: string
  done: number
  failed: number
}

type Phase = 'idle' | 'running' | 'captcha' | 'done'

// ─── Exam sessions pulled from scraper.py pattern ─────────────────────────────
const SESSIONS = [
  { value: 'Jan', label: 'January / February' },
  { value: 'Jun', label: 'June / July' },
  { value: 'Dec', label: 'December' },
]

let logId = 0
const now = () => new Date().toLocaleTimeString('en-IN', { hour12: false })

// ─── App ──────────────────────────────────────────────────────────────────────
export default function App() {
  // Config state
  const [usnPrefix, setUsnPrefix] = useState('1JS22CS')
  const [sem, setSem] = useState(3)
  const [vtuFolder, setVtuFolder] = useState('DJcbcs24')
  const [startNum, setStartNum] = useState('1')
  const [endNum, setEndNum] = useState('60')
  const [isOverride, setIsOverride] = useState(false)

  // Runtime state
  const [phase, setPhase] = useState<Phase>('idle')
  const [progress, setProgress] = useState<ProgressPayload>({ done: 0, total: 0, failed: 0, current: '' })
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [captcha, setCaptcha] = useState<CaptchaPayload | null>(null)
  const [captchaInput, setCaptchaInput] = useState('')
  const [captchaSubmitting, setCaptchaSubmitting] = useState(false)
  const [done, setDone] = useState<DonePayload | null>(null)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const logEndRef = useRef<HTMLDivElement>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)
  const [dropdownOpen, setDropdownOpen] = useState(false)

  // Close dropdown on click outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setDropdownOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  // Auto-scroll log to bottom
  useEffect(() => { logEndRef.current?.scrollIntoView({ behavior: 'smooth' }) }, [logs])

  const pushLog = useCallback((level: LogEntry['level'], msg: string) => {
    setLogs(prev => [...prev.slice(-200), { id: logId++, level, msg, time: now() }])
  }, [])

  // ─── Auto-compute VTU Folder based on acatrack logic ──────────────────────────
  useEffect(() => {
    if (isOverride) return

    // Extract 2-digit batch year from USN, e.g. "1JS22CS" -> 22
    const yearMatch = usnPrefix.match(/\d{2}/)
    if (!yearMatch) return

    const batchYear = 2000 + parseInt(yearMatch[0], 10)
    let session = ''
    let year = 0

    if (sem % 2 === 1) { // odd sem
      session = 'DJ'
      year = batchYear + Math.floor(sem / 2) + 1
    } else { // even sem
      session = 'JJE'
      year = batchYear + Math.floor(sem / 2)
    }

    const yearSuffix = String(year).slice(-2)
    setVtuFolder(`${session}cbcs${yearSuffix}`)
  }, [usnPrefix, sem, isOverride])

  // ─── Wails event listeners ─────────────────────────────────────────────────
  useEffect(() => {
    const offs = [
      EventsOn('scraper:progress', (p: ProgressPayload) => setProgress(p)),
      EventsOn('scraper:log', (e: { level: LogEntry['level']; msg: string }) => {
        pushLog(e.level, e.msg)
      }),
      EventsOn('scraper:captcha', (c: CaptchaPayload) => {
        setCaptcha(c)
        setCaptchaInput('')
        setPhase('captcha')
      }),
      EventsOn('scraper:done', (d: DonePayload) => {
        setDone(d)
        setPhase('done')
        pushLog('success', `Batch complete — ${d.done} fetched, ${d.failed} failed`)
      }),
    ]
    return () => offs.forEach(off => off())
  }, [pushLog])

  // ─── Handlers ─────────────────────────────────────────────────────────────
  const handleStart = async () => {
    setError('')
    setLogs([])
    setDone(null)
    setProgress({ done: 0, total: 0, failed: 0, current: '' })
    const s = parseInt(startNum, 10)
    const e = parseInt(endNum, 10)
    if (!vtuFolder || !usnPrefix || isNaN(s) || isNaN(e) || s > e) {
      setError('Please fill in all fields correctly.')
      return
    }
    setPhase('running')
    pushLog('info', `Starting batch: ${usnPrefix}${String(s).padStart(3,'0')} → ${usnPrefix}${String(e).padStart(3,'0')}`)
    try {
      await StartBatch(vtuFolder, usnPrefix, s, e)
    } catch (err: unknown) {
      setError(String(err))
      setPhase('idle')
    }
  }

  const handleCaptchaSubmit = async () => {
    if (!captchaInput.trim()) return
    setCaptchaSubmitting(true)
    try {
      await SubmitCaptcha(captchaInput.trim())
    } finally {
      setCaptchaSubmitting(false)
      setCaptcha(null)
      setPhase('running')
    }
  }

  const handleSaveZip = async () => {
    setSaving(true)
    try {
      const dest = await SaveZip()
      if (dest) pushLog('success', `ZIP saved to: ${dest}`)
    } catch (err) {
      pushLog('error', `Save failed: ${err}`)
    } finally {
      setSaving(false)
    }
  }

  // ─── Derived ───────────────────────────────────────────────────────────────
  const pct = progress.total > 0 ? Math.round(((progress.done + progress.failed) / progress.total) * 100) : 0
  const isComplete = phase === 'done'
  const isRunning = phase === 'running' || phase === 'captcha'

  // ─── Render ────────────────────────────────────────────────────────────────
  return (
    <div className="app-layout">

      {/* Header */}
      <header className="header">
        <div className="window-controls">
          <div className="control-dot close" onClick={() => Quit()} title="Close" />
          <div className="control-dot minimize" onClick={() => WindowMinimise()} title="Minimize" />
          <div className="control-dot maximize" onClick={() => WindowToggleMaximise()} title="Maximize" />
        </div>
        <div className="header-logo">🎓</div>
        <div>
          <div className="header-title">VTU Result Scraper</div>
          <div className="header-subtitle">acatrack · PDF Batch Generator</div>
        </div>
        <div className="header-badge">
          {phase === 'idle' ? 'READY' : phase === 'done' ? 'DONE' : 'RUNNING'}
        </div>
      </header>

      <div className="main-content">

        {/* ── Left Config Panel ── */}
        <aside className="config-panel">
          <div className="config-panel-inner">

            {/* Exam Details */}
            <div>
              <div className="section-label">Exam Details</div>
              <div className="field-group">
                <div className="field" ref={dropdownRef}>
                  <label>Semester</label>
                  <div className="custom-select-container">
                    <div
                      className={`custom-select-trigger ${dropdownOpen ? 'open' : ''} ${isRunning ? 'disabled' : ''}`}
                      onClick={() => !isRunning && setDropdownOpen(!dropdownOpen)}
                    >
                      <span>Semester {sem}</span>
                      <span className="custom-select-arrow"></span>
                    </div>
                    {dropdownOpen && !isRunning && (
                      <div className="custom-select-options">
                        {[1, 2, 3, 4, 5, 6, 7, 8].map(s => (
                          <div
                            key={s}
                            className={`custom-select-option ${s === sem ? 'selected' : ''}`}
                            onClick={() => {
                              setSem(s)
                              setDropdownOpen(false)
                            }}
                          >
                            Semester {s}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>

                <div className="field">
                  <label style={{ display: 'flex', justifyContent: 'space-between' }}>
                    <span>VTU Portal Folder</span>
                    <span
                      className={`override-toggle ${isOverride ? 'active' : 'inactive'}`}
                      onClick={() => setIsOverride(!isOverride)}
                    >
                      {isOverride ? '🔒 Auto-Calculate' : '🔓 Edit Folder'}
                    </span>
                  </label>
                  <input
                    type="text"
                    value={vtuFolder}
                    onChange={e => {
                      setVtuFolder(e.target.value)
                      setIsOverride(true)
                    }}
                    placeholder="e.g. DJcbcs24"
                    disabled={isRunning || !isOverride}
                    className={`mono ${!isOverride ? 'read-only-calc' : ''}`}
                  />
                </div>
              </div>
            </div>

            <div className="divider" />

            {/* USN Range */}
            <div>
              <div className="section-label">USN Range</div>
              <div className="field-group">
                <div className="field">
                  <label>USN Prefix</label>
                  <input
                    type="text" value={usnPrefix} onChange={e => setUsnPrefix(e.target.value)}
                    placeholder="e.g. 1JS22CS" disabled={isRunning}
                    className="mono"
                  />
                </div>
                <div className="field-row">
                  <div className="field">
                    <label>Start №</label>
                    <input type="number" value={startNum} onChange={e => setStartNum(e.target.value)} disabled={isRunning} min="1" />
                  </div>
                  <div className="field">
                    <label>End №</label>
                    <input type="number" value={endNum} onChange={e => setEndNum(e.target.value)} disabled={isRunning} min="1" />
                  </div>
                </div>
              </div>
            </div>

            <div className="divider" />

            {/* Progress */}
            {(isRunning || isComplete) && (
              <div className="progress-section">
                <div className="section-label">Progress</div>
                <div className="progress-info">
                  <span className="progress-label">
                    {isComplete ? 'Complete' : `${progress.done + progress.failed} / ${progress.total}`}
                  </span>
                  <span className="progress-pct">{pct}%</span>
                </div>
                <div className="progress-track">
                  <div className={`progress-fill ${isComplete ? 'complete' : ''}`} style={{ width: `${pct}%` }} />
                </div>
                {progress.current && (
                  <div className="current-usn-box">
                    <span className="current-usn-label">Now</span>
                    <span>{progress.current}</span>
                  </div>
                )}
              </div>
            )}

            {/* Completion banner */}
            {isComplete && done && (
              <div className="complete-banner">
                <div className="complete-banner-title">
                  ✓ Batch Complete
                </div>
                <div className="zip-path">{done.zipPath}</div>
                <button className="btn btn-success" onClick={handleSaveZip} disabled={saving}>
                  {saving ? <><div className="spinner" /> Saving…</> : '💾 Save ZIP File'}
                </button>
              </div>
            )}

             {/* Error */}
            {error && (
              <div className="error-banner">
                {error}
              </div>
            )}

            {/* Start / Reset */}
            <div style={{ marginTop: 'auto', paddingTop: 8 }}>
              {!isRunning && !isComplete && (
                <button id="btn-start" className="btn btn-primary" onClick={handleStart}>
                  ▶ Start Scraping
                </button>
              )}
              {isRunning && (
                <button className="btn btn-scraping" disabled>
                  <div className="spinner" /> Scraping Batch…
                </button>
              )}
              {isComplete && (
                <button className="btn btn-ghost" onClick={() => { setPhase('idle'); setDone(null); setLogs([]) }}>
                  ↩ New Batch
                </button>
              )}
            </div>

          </div>
        </aside>

        {/* ── Right Feed Panel ── */}
        <section className="feed-panel">
          <div className="feed-header">
            <div className={`pulse-dot ${phase === 'idle' ? 'idle' : phase === 'done' ? 'complete' : ''}`} />
            <span className="feed-title">Activity Log</span>
            <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--text-muted)' }}>
              {logs.length} entries
            </span>
          </div>

          <div className="feed-log">
            {logs.length === 0 && (
              <div style={{ color: 'var(--text-muted)', padding: '24px 0', textAlign: 'center' }}>
                Waiting to start…
              </div>
            )}
            {logs.map(entry => (
              <div key={entry.id} className={`log-entry ${entry.level}`}>
                <span className="log-time">{entry.time}</span>
                <span className="log-msg">{entry.msg}</span>
              </div>
            ))}
            <div ref={logEndRef} />
          </div>

          {/* Stats bar */}
          <div className="stats-bar">
            <div className="stat-card">
              <div className="stat-value success">{progress.done}</div>
              <div className="stat-label">Fetched</div>
            </div>
            <div className="stat-card">
              <div className="stat-value danger">{progress.failed}</div>
              <div className="stat-label">Failed</div>
            </div>
            <div className="stat-card">
              <div className="stat-value accent">{progress.total}</div>
              <div className="stat-label">Total</div>
            </div>
          </div>
        </section>
      </div>

      {/* ── CAPTCHA Modal ── */}
      {phase === 'captcha' && captcha && (
        <div className="modal-overlay">
          <div className="modal">
            <div className="modal-header">
              <div className="modal-icon">🔐</div>
              <div>
                <div className="modal-title">CAPTCHA Required</div>
                <div className="modal-usn">{captcha.usn}</div>
              </div>
            </div>

            <div className="captcha-img-wrapper">
              {captcha.imgB64
                ? <img className="captcha-img" src={`data:image/png;base64,${captcha.imgB64}`} alt="CAPTCHA" />
                : <span className="captcha-placeholder">Loading image…</span>
              }
            </div>

            <div className="field">
              <label>Enter the text shown above</label>
              <input
                id="captcha-input"
                type="text"
                autoFocus
                value={captchaInput}
                onChange={e => setCaptchaInput(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleCaptchaSubmit()}
                placeholder="Type CAPTCHA here…"
                disabled={captchaSubmitting}
                className="captcha-input-field"
              />
            </div>

            <div className="modal-actions">
              <button className="btn btn-ghost" onClick={() => { setCaptcha(null); setPhase('running'); SubmitCaptcha('__skip__') }}>
                Skip USN
              </button>
              <button
                id="btn-captcha-submit"
                className="btn btn-primary"
                onClick={handleCaptchaSubmit}
                disabled={!captchaInput.trim() || captchaSubmitting}
              >
                {captchaSubmitting ? <><div className="spinner" /> Submitting…</> : 'Submit →'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
