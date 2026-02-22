import { useCallback } from 'react'

export function useChatStreamClient({
    t,
    onMessage,
    model,
    message,
    effectiveKey,
    selectedAccount,
    streamingMode,
    abortControllerRef,
    setLoading,
    setIsStreaming,
    setResponse,
    setStreamingContent,
    setStreamingThinking,
}) {
    const stopGeneration = useCallback(() => {
        if (abortControllerRef.current) {
            abortControllerRef.current.abort()
            abortControllerRef.current = null
        }
        setLoading(false)
        setIsStreaming(false)
    }, [abortControllerRef, setIsStreaming, setLoading])

    const extractErrorMessage = useCallback(async (res) => {
        let raw = ''
        try {
            raw = await res.text()
        } catch {
            return t('apiTester.requestFailed')
        }
        if (!raw) {
            return t('apiTester.requestFailed')
        }
        try {
            const data = JSON.parse(raw)
            const fromErrorObject = data?.error?.message
            const fromErrorString = typeof data?.error === 'string' ? data.error : ''
            const detail = typeof data?.detail === 'string' ? data.detail : ''
            const msg = typeof data?.message === 'string' ? data.message : ''
            return fromErrorObject || fromErrorString || detail || msg || t('apiTester.requestFailed')
        } catch {
            return raw.length > 240 ? `${raw.slice(0, 240)}...` : raw
        }
    }, [t])

    const runTest = useCallback(async () => {
        if (!effectiveKey) {
            onMessage('error', t('apiTester.missingApiKey'))
            return
        }

        const startedAt = Date.now()
        setLoading(true)
        setIsStreaming(true)
        setResponse(null)
        setStreamingContent('')
        setStreamingThinking('')

        abortControllerRef.current = new AbortController()

        try {
            const headers = {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${effectiveKey}`,
            }
            if (selectedAccount) {
                headers['X-Ds2-Target-Account'] = selectedAccount
            }

            const endpoint = streamingMode ? '/v1/chat/completions' : '/v1/chat/completions?__go=1'
            const res = await fetch(endpoint, {
                method: 'POST',
                headers,
                body: JSON.stringify({
                    model,
                    messages: [{ role: 'user', content: message }],
                    stream: streamingMode,
                }),
                signal: abortControllerRef.current.signal,
            })

            if (!res.ok) {
                const errorMsg = await extractErrorMessage(res)
                setResponse({ success: false, error: errorMsg })
                onMessage('error', errorMsg)
                setLoading(false)
                setIsStreaming(false)
                return
            }

            if (streamingMode) {
                setResponse({ success: true, status_code: res.status })

                const reader = res.body.getReader()
                const decoder = new TextDecoder()
                let buffer = ''

                while (true) {
                    const { done, value } = await reader.read()
                    if (done) break

                    buffer += decoder.decode(value, { stream: true })
                    const lines = buffer.split('\n')
                    buffer = lines.pop() || ''

                    for (const line of lines) {
                        const trimmed = line.trim()
                        if (!trimmed || !trimmed.startsWith('data: ')) continue

                        const dataStr = trimmed.slice(6)
                        if (dataStr === '[DONE]') continue

                        try {
                            const json = JSON.parse(dataStr)
                            const choice = json.choices?.[0]
                            if (choice?.delta) {
                                const delta = choice.delta
                                if (delta.reasoning_content) {
                                    setStreamingThinking(prev => prev + delta.reasoning_content)
                                }
                                if (delta.content) {
                                    setStreamingContent(prev => prev + delta.content)
                                }
                            }
                        } catch (e) {
                            console.error('Invalid JSON hunk:', dataStr, e)
                        }
                    }
                }
            } else {
                const data = await res.json()
                setResponse({ success: true, status_code: res.status, ...data })
                const elapsed = Math.max(0, Date.now() - startedAt)
                onMessage('success', t('apiTester.testSuccess', { account: selectedAccount || 'Auto', time: elapsed }))
            }
        } catch (e) {
            if (e.name === 'AbortError') {
                onMessage('info', t('messages.generationStopped'))
            } else {
                onMessage('error', t('apiTester.networkError', { error: e.message }))
                setResponse({ error: e.message, success: false })
            }
        } finally {
            setLoading(false)
            setIsStreaming(false)
            abortControllerRef.current = null
        }
    }, [
        abortControllerRef,
        effectiveKey,
        extractErrorMessage,
        message,
        model,
        onMessage,
        selectedAccount,
        setIsStreaming,
        setLoading,
        setResponse,
        setStreamingContent,
        setStreamingThinking,
        streamingMode,
        t,
    ])

    return {
        runTest,
        stopGeneration,
    }
}
