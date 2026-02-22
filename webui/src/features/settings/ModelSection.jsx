export default function ModelSection({ t, form, setForm }) {
    return (
        <div className="bg-card border border-border rounded-xl p-5 space-y-4">
            <h3 className="font-semibold">{t('settings.modelTitle')}</h3>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.claudeMapping')}</span>
                    <textarea
                        value={form.claude_mapping_text}
                        onChange={(e) => setForm((prev) => ({ ...prev, claude_mapping_text: e.target.value }))}
                        rows={8}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2 font-mono text-xs"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.modelAliases')}</span>
                    <textarea
                        value={form.model_aliases_text}
                        onChange={(e) => setForm((prev) => ({ ...prev, model_aliases_text: e.target.value }))}
                        rows={8}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2 font-mono text-xs"
                    />
                </label>
            </div>
        </div>
    )
}
