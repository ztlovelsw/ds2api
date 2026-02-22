import { useCallback, useEffect, useState } from 'react'

export function useAdminConfig({ token, showMessage, t }) {
    const [config, setConfig] = useState({ keys: [], accounts: [] })

    const fetchConfig = useCallback(async () => {
        if (!token) return
        try {
            const res = await fetch('/admin/config', {
                headers: { 'Authorization': `Bearer ${token}` }
            })
            if (res.ok) {
                const data = await res.json()
                setConfig(data)
            }
        } catch (e) {
            console.error('Failed to fetch config:', e)
            showMessage('error', t('errors.fetchConfig', { error: e.message }))
        }
    }, [showMessage, t, token])

    useEffect(() => {
        if (token) {
            fetchConfig()
        }
    }, [fetchConfig, token])

    return {
        config,
        fetchConfig,
    }
}
