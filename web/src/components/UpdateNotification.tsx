import { useEffect, useState } from 'react'

interface UpdateInfo {
  version: string
  releaseNotes?: string
}

export function UpdateNotification() {
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null)
  const [downloaded, setDownloaded] = useState(false)

  useEffect(() => {
    const api = window.electronAPI
    if (!api?.onUpdateAvailable) return

    const unsubAvailable = api.onUpdateAvailable((info) => {
      setUpdateInfo(info)
      setDownloaded(false)
    })

    const unsubDownloaded = api.onUpdateDownloaded((info) => {
      setUpdateInfo(info)
      setDownloaded(true)
    })

    return () => {
      unsubAvailable()
      unsubDownloaded()
    }
  }, [])

  const handleInstall = async () => {
    await window.electronAPI?.quitAndInstall()
  }

  const handleDismiss = () => {
    setUpdateInfo(null)
  }

  if (!updateInfo) return null

  return (
    <div style={{
      position: 'fixed',
      top: 0,
      left: 0,
      right: 0,
      zIndex: 9999,
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      gap: 12,
      padding: '10px 16px',
      background: 'var(--accent)',
      color: '#fff',
      fontSize: 13,
      fontWeight: 500,
      boxShadow: '0 2px 8px rgba(0,0,0,0.2)',
    }}>
      <span>
        {downloaded
          ? `NeoCode v${updateInfo.version} is ready to install`
          : `A new version v${updateInfo.version} is available, downloading...`}
      </span>
      {downloaded && (
        <button
          onClick={handleInstall}
          style={{
            padding: '4px 12px',
            borderRadius: 'var(--radius-sm)',
            border: '1px solid rgba(255,255,255,0.4)',
            background: 'rgba(255,255,255,0.15)',
            color: '#fff',
            fontSize: 12,
            fontWeight: 600,
            cursor: 'pointer',
            transition: 'background 0.2s',
          }}
          onMouseEnter={(e) => {
            (e.target as HTMLButtonElement).style.background = 'rgba(255,255,255,0.25)'
          }}
          onMouseLeave={(e) => {
            (e.target as HTMLButtonElement).style.background = 'rgba(255,255,255,0.15)'
          }}
        >
          Restart Now
        </button>
      )}
      <button
        onClick={handleDismiss}
        style={{
          padding: '2px 6px',
          borderRadius: 'var(--radius-sm)',
          border: 'none',
          background: 'transparent',
          color: 'rgba(255,255,255,0.7)',
          fontSize: 16,
          cursor: 'pointer',
          lineHeight: 1,
        }}
        title="Dismiss"
      >
        ×
      </button>
    </div>
  )
}
