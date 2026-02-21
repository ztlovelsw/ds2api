import { useCallback, useEffect, useMemo, useState } from 'react'
import { AlertTriangle, Download, Lock, Save, Upload } from 'lucide-react'
import { useI18n } from '../i18n'

const MAX_AUTO_FETCH_FAILURES = 3

export default function Settings({ onRefresh, onMessage, authFetch, onForceLogout, isVercel = false }) {
    const { t } = useI18n()
    const apiFetch = authFetch || fetch

    const [loading, setLoading] = useState(false)
    const [saving, setSaving] = useState(false)
    const [changingPassword, setChangingPassword] = useState(false)
    const [importing, setImporting] = useState(false)
    const [exportData, setExportData] = useState(null)
    const [importMode, setImportMode] = useState('merge')
    const [importText, setImportText] = useState('')
    const [newPassword, setNewPassword] = useState('')
    const [consecutiveFailures, setConsecutiveFailures] = useState(0)
    const [autoFetchPaused, setAutoFetchPaused] = useState(false)
    const [lastError, setLastError] = useState('')
    const [settingsMeta, setSettingsMeta] = useState({ default_password_warning: false, env_backed: false, needs_vercel_sync: false })

    const [form, setForm] = useState({
        admin: { jwt_expire_hours: 24 },
        runtime: { account_max_inflight: 2, account_max_queue: 10, global_max_inflight: 10 },
        toolcall: { mode: 'feature_match', early_emit_confidence: 'high' },
        responses: { store_ttl_seconds: 900 },
        embeddings: { provider: '' },
        claude_mapping_text: '{\n  "fast": "deepseek-chat",\n  "slow": "deepseek-reasoner"\n}',
        model_aliases_text: '{}',
    })

    const parseJSONMap = (raw, fieldName) => {
        const text = String(raw || '').trim()
        if (!text) {
            return {}
        }
        let parsed
        try {
            parsed = JSON.parse(text)
        } catch (_e) {
            throw new Error(t('settings.invalidJsonField', { field: fieldName }))
        }
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
            throw new Error(t('settings.invalidJsonField', { field: fieldName }))
        }
        return parsed
    }

    const parseJSONResponse = useCallback(async (res) => {
        const contentType = String(res.headers.get('content-type') || '').toLowerCase()
        if (!contentType.includes('application/json')) {
            throw new Error(t('settings.nonJsonResponse', { status: res.status }))
        }
        return res.json()
    }, [t])

    const loadSettings = useCallback(async ({ manual = false } = {}) => {
        if (isVercel && autoFetchPaused && !manual) {
            return
        }
        setLoading(true)
        try {
            const res = await apiFetch('/admin/settings')
            const data = await parseJSONResponse(res)
            if (!res.ok) {
                const detail = data.detail || t('settings.loadFailed')
                setLastError(detail)
                onMessage('error', detail)
                setConsecutiveFailures((prev) => {
                    const next = prev + 1
                    if (isVercel && next >= MAX_AUTO_FETCH_FAILURES) {
                        setAutoFetchPaused(true)
                    }
                    return next
                })
                return
            }
            setConsecutiveFailures(0)
            setAutoFetchPaused(false)
            setLastError('')
            setSettingsMeta({
                default_password_warning: Boolean(data.admin?.default_password_warning),
                env_backed: Boolean(data.env_backed),
                needs_vercel_sync: Boolean(data.needs_vercel_sync),
            })
            setForm({
                admin: { jwt_expire_hours: Number(data.admin?.jwt_expire_hours || 24) },
                runtime: {
                    account_max_inflight: Number(data.runtime?.account_max_inflight || 2),
                    account_max_queue: Number(data.runtime?.account_max_queue || 10),
                    global_max_inflight: Number(data.runtime?.global_max_inflight || 10),
                },
                toolcall: {
                    mode: data.toolcall?.mode || 'feature_match',
                    early_emit_confidence: data.toolcall?.early_emit_confidence || 'high',
                },
                responses: {
                    store_ttl_seconds: Number(data.responses?.store_ttl_seconds || 900),
                },
                embeddings: {
                    provider: data.embeddings?.provider || '',
                },
                claude_mapping_text: JSON.stringify(data.claude_mapping || {}, null, 2),
                model_aliases_text: JSON.stringify(data.model_aliases || {}, null, 2),
            })
        } catch (e) {
            const detail = e?.message || t('settings.loadFailed')
            setLastError(detail)
            onMessage('error', detail)
            setConsecutiveFailures((prev) => {
                const next = prev + 1
                if (isVercel && next >= MAX_AUTO_FETCH_FAILURES) {
                    setAutoFetchPaused(true)
                }
                return next
            })
            // eslint-disable-next-line no-console
            console.error(e)
        } finally {
            setLoading(false)
        }
    }, [apiFetch, autoFetchPaused, isVercel, onMessage, parseJSONResponse, t])

    useEffect(() => {
        loadSettings()
    }, [loadSettings])

    const retryLoadSettings = () => {
        setAutoFetchPaused(false)
        loadSettings({ manual: true })
    }

    const saveSettings = async () => {
        let claudeMapping = {}
        let modelAliases = {}
        try {
            claudeMapping = parseJSONMap(form.claude_mapping_text, 'claude_mapping')
            modelAliases = parseJSONMap(form.model_aliases_text, 'model_aliases')
        } catch (e) {
            onMessage('error', e.message)
            return
        }

        const payload = {
            admin: { jwt_expire_hours: Number(form.admin.jwt_expire_hours) },
            runtime: {
                account_max_inflight: Number(form.runtime.account_max_inflight),
                account_max_queue: Number(form.runtime.account_max_queue),
                global_max_inflight: Number(form.runtime.global_max_inflight),
            },
            toolcall: {
                mode: String(form.toolcall.mode || '').trim(),
                early_emit_confidence: String(form.toolcall.early_emit_confidence || '').trim(),
            },
            responses: { store_ttl_seconds: Number(form.responses.store_ttl_seconds) },
            embeddings: { provider: String(form.embeddings.provider || '').trim() },
            claude_mapping: claudeMapping,
            model_aliases: modelAliases,
        }

        setSaving(true)
        try {
            const res = await apiFetch('/admin/settings', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            })
            const data = await res.json()
            if (!res.ok) {
                onMessage('error', data.detail || t('settings.saveFailed'))
                return
            }
            onMessage('success', t('settings.saveSuccess'))
            if (typeof onRefresh === 'function') {
                onRefresh()
            }
            await loadSettings()
        } catch (e) {
            onMessage('error', t('settings.saveFailed'))
            // eslint-disable-next-line no-console
            console.error(e)
        } finally {
            setSaving(false)
        }
    }

    const updatePassword = async () => {
        if (String(newPassword || '').trim().length < 4) {
            onMessage('error', t('settings.passwordTooShort'))
            return
        }
        setChangingPassword(true)
        try {
            const res = await apiFetch('/admin/settings/password', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ new_password: newPassword.trim() }),
            })
            const data = await res.json()
            if (!res.ok) {
                onMessage('error', data.detail || t('settings.passwordUpdateFailed'))
                return
            }
            onMessage('success', t('settings.passwordUpdated'))
            setNewPassword('')
            if (typeof onForceLogout === 'function') {
                onForceLogout()
            }
        } catch (e) {
            onMessage('error', t('settings.passwordUpdateFailed'))
        } finally {
            setChangingPassword(false)
        }
    }

    const loadExportData = async () => {
        try {
            const res = await apiFetch('/admin/config/export')
            const data = await res.json()
            if (!res.ok) {
                onMessage('error', data.detail || t('settings.exportFailed'))
                return
            }
            setExportData(data)
            onMessage('success', t('settings.exportLoaded'))
        } catch (e) {
            onMessage('error', t('settings.exportFailed'))
        }
    }

    const doImport = async () => {
        if (!String(importText || '').trim()) {
            onMessage('error', t('settings.importEmpty'))
            return
        }
        let parsed
        try {
            parsed = JSON.parse(importText)
        } catch (_e) {
            onMessage('error', t('settings.importInvalidJson'))
            return
        }
        setImporting(true)
        try {
            const res = await apiFetch(`/admin/config/import?mode=${encodeURIComponent(importMode)}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ config: parsed, mode: importMode }),
            })
            const data = await res.json()
            if (!res.ok) {
                onMessage('error', data.detail || t('settings.importFailed'))
                return
            }
            onMessage('success', t('settings.importSuccess', { mode: importMode }))
            if (typeof onRefresh === 'function') {
                onRefresh()
            }
            await loadSettings()
        } catch (e) {
            onMessage('error', t('settings.importFailed'))
        } finally {
            setImporting(false)
        }
    }

    const syncHintVisible = useMemo(() => settingsMeta.env_backed || settingsMeta.needs_vercel_sync, [settingsMeta.env_backed, settingsMeta.needs_vercel_sync])

    return (
        <div className="space-y-6">
            {autoFetchPaused && (
                <div className="p-4 rounded-lg border border-destructive/30 bg-destructive/10 text-destructive flex items-center justify-between gap-4">
                    <div className="flex items-center gap-2">
                        <AlertTriangle className="w-4 h-4" />
                        <span className="text-sm">
                            {t('settings.autoFetchPaused', { count: consecutiveFailures, error: lastError || t('settings.loadFailed') })}
                        </span>
                    </div>
                    <button
                        type="button"
                        onClick={retryLoadSettings}
                        className="px-3 py-1.5 text-xs rounded-md border border-destructive/40 hover:bg-destructive/10"
                    >
                        {t('settings.retryLoad')}
                    </button>
                </div>
            )}
            {settingsMeta.default_password_warning && (
                <div className="p-4 rounded-lg border border-amber-300/30 bg-amber-500/10 text-amber-700 flex items-center gap-2">
                    <AlertTriangle className="w-4 h-4" />
                    <span className="text-sm">{t('settings.defaultPasswordWarning')}</span>
                </div>
            )}
            {syncHintVisible && (
                <div className="p-4 rounded-lg border border-amber-300/30 bg-amber-500/10 text-amber-700 flex items-center gap-2">
                    <AlertTriangle className="w-4 h-4" />
                    <span className="text-sm">{t('settings.vercelSyncHint')}</span>
                </div>
            )}

            <div className="bg-card border border-border rounded-xl p-5 space-y-4">
                <h3 className="font-semibold">{t('settings.securityTitle')}</h3>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.jwtExpireHours')}</span>
                        <input
                            type="number"
                            min={1}
                            max={720}
                            value={form.admin.jwt_expire_hours}
                            onChange={(e) => setForm((prev) => ({ ...prev, admin: { ...prev.admin, jwt_expire_hours: Number(e.target.value || 1) } }))}
                            className="w-full bg-background border border-border rounded-lg px-3 py-2"
                        />
                    </label>
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.newPassword')}</span>
                        <div className="flex gap-2">
                            <input
                                type="password"
                                value={newPassword}
                                onChange={(e) => setNewPassword(e.target.value)}
                                placeholder={t('settings.newPasswordPlaceholder')}
                                className="w-full bg-background border border-border rounded-lg px-3 py-2"
                            />
                            <button
                                type="button"
                                onClick={updatePassword}
                                disabled={changingPassword}
                                className="px-3 py-2 rounded-lg bg-secondary border border-border hover:bg-secondary/80 text-sm flex items-center gap-1"
                            >
                                <Lock className="w-4 h-4" />
                                {changingPassword ? t('settings.updating') : t('settings.updatePassword')}
                            </button>
                        </div>
                    </label>
                </div>
            </div>

            <div className="bg-card border border-border rounded-xl p-5 space-y-4">
                <h3 className="font-semibold">{t('settings.runtimeTitle')}</h3>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.accountMaxInflight')}</span>
                        <input type="number" min={1} value={form.runtime.account_max_inflight} onChange={(e) => setForm((prev) => ({ ...prev, runtime: { ...prev.runtime, account_max_inflight: Number(e.target.value || 1) } }))} className="w-full bg-background border border-border rounded-lg px-3 py-2" />
                    </label>
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.accountMaxQueue')}</span>
                        <input type="number" min={1} value={form.runtime.account_max_queue} onChange={(e) => setForm((prev) => ({ ...prev, runtime: { ...prev.runtime, account_max_queue: Number(e.target.value || 1) } }))} className="w-full bg-background border border-border rounded-lg px-3 py-2" />
                    </label>
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.globalMaxInflight')}</span>
                        <input type="number" min={1} value={form.runtime.global_max_inflight} onChange={(e) => setForm((prev) => ({ ...prev, runtime: { ...prev.runtime, global_max_inflight: Number(e.target.value || 1) } }))} className="w-full bg-background border border-border rounded-lg px-3 py-2" />
                    </label>
                </div>
            </div>

            <div className="bg-card border border-border rounded-xl p-5 space-y-4">
                <h3 className="font-semibold">{t('settings.behaviorTitle')}</h3>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.toolcallMode')}</span>
                        <select value={form.toolcall.mode} onChange={(e) => setForm((prev) => ({ ...prev, toolcall: { ...prev.toolcall, mode: e.target.value } }))} className="w-full bg-background border border-border rounded-lg px-3 py-2">
                            <option value="feature_match">feature_match</option>
                            <option value="off">off</option>
                        </select>
                    </label>
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.earlyEmitConfidence')}</span>
                        <select value={form.toolcall.early_emit_confidence} onChange={(e) => setForm((prev) => ({ ...prev, toolcall: { ...prev.toolcall, early_emit_confidence: e.target.value } }))} className="w-full bg-background border border-border rounded-lg px-3 py-2">
                            <option value="high">high</option>
                            <option value="low">low</option>
                            <option value="off">off</option>
                        </select>
                    </label>
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.responsesTTL')}</span>
                        <input type="number" min={30} value={form.responses.store_ttl_seconds} onChange={(e) => setForm((prev) => ({ ...prev, responses: { ...prev.responses, store_ttl_seconds: Number(e.target.value || 30) } }))} className="w-full bg-background border border-border rounded-lg px-3 py-2" />
                    </label>
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.embeddingsProvider')}</span>
                        <input type="text" value={form.embeddings.provider} onChange={(e) => setForm((prev) => ({ ...prev, embeddings: { ...prev.embeddings, provider: e.target.value } }))} className="w-full bg-background border border-border rounded-lg px-3 py-2" />
                    </label>
                </div>
            </div>

            <div className="bg-card border border-border rounded-xl p-5 space-y-4">
                <h3 className="font-semibold">{t('settings.modelTitle')}</h3>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.claudeMapping')}</span>
                        <textarea value={form.claude_mapping_text} onChange={(e) => setForm((prev) => ({ ...prev, claude_mapping_text: e.target.value }))} rows={8} className="w-full bg-background border border-border rounded-lg px-3 py-2 font-mono text-xs" />
                    </label>
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.modelAliases')}</span>
                        <textarea value={form.model_aliases_text} onChange={(e) => setForm((prev) => ({ ...prev, model_aliases_text: e.target.value }))} rows={8} className="w-full bg-background border border-border rounded-lg px-3 py-2 font-mono text-xs" />
                    </label>
                </div>
            </div>

            <div className="bg-card border border-border rounded-xl p-5 space-y-4">
                <h3 className="font-semibold">{t('settings.backupTitle')}</h3>
                <div className="flex flex-wrap items-center gap-3">
                    <button type="button" onClick={loadExportData} className="px-3 py-2 rounded-lg bg-secondary border border-border hover:bg-secondary/80 text-sm flex items-center gap-2">
                        <Download className="w-4 h-4" />
                        {t('settings.loadExport')}
                    </button>
                    <select value={importMode} onChange={(e) => setImportMode(e.target.value)} className="bg-background border border-border rounded-lg px-3 py-2 text-sm">
                        <option value="merge">{t('settings.importModeMerge')}</option>
                        <option value="replace">{t('settings.importModeReplace')}</option>
                    </select>
                    <button type="button" onClick={doImport} disabled={importing} className="px-3 py-2 rounded-lg bg-secondary border border-border hover:bg-secondary/80 text-sm flex items-center gap-2">
                        <Upload className="w-4 h-4" />
                        {importing ? t('settings.importing') : t('settings.importNow')}
                    </button>
                </div>
                <textarea value={importText} onChange={(e) => setImportText(e.target.value)} rows={8} className="w-full bg-background border border-border rounded-lg px-3 py-2 font-mono text-xs" placeholder={t('settings.importPlaceholder')} />
                {exportData && (
                    <div className="space-y-2">
                        <label className="text-sm text-muted-foreground">{t('settings.exportJson')}</label>
                        <textarea value={exportData.json || ''} readOnly rows={6} className="w-full bg-background border border-border rounded-lg px-3 py-2 font-mono text-xs" />
                    </div>
                )}
            </div>

            <div className="flex justify-end">
                <button type="button" onClick={saveSettings} disabled={loading || saving} className="px-4 py-2 rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 flex items-center gap-2">
                    <Save className="w-4 h-4" />
                    {saving ? t('settings.saving') : t('settings.save')}
                </button>
            </div>
        </div>
    )
}
