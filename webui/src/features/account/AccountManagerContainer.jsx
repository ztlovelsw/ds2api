import { useI18n } from '../../i18n'
import { useAccountsData } from './useAccountsData'
import { useAccountActions } from './useAccountActions'
import QueueCards from './QueueCards'
import ApiKeysPanel from './ApiKeysPanel'
import AccountsTable from './AccountsTable'
import AddKeyModal from './AddKeyModal'
import AddAccountModal from './AddAccountModal'

export default function AccountManagerContainer({ config, onRefresh, onMessage, authFetch }) {
    const { t } = useI18n()
    const apiFetch = authFetch || fetch

    const {
        queueStatus,
        keysExpanded,
        setKeysExpanded,
        accounts,
        page,
        totalPages,
        totalAccounts,
        loadingAccounts,
        fetchAccounts,
        resolveAccountIdentifier,
    } = useAccountsData({ apiFetch })

    const {
        showAddKey,
        setShowAddKey,
        showAddAccount,
        setShowAddAccount,
        newKey,
        setNewKey,
        copiedKey,
        setCopiedKey,
        newAccount,
        setNewAccount,
        loading,
        testing,
        testingAll,
        batchProgress,
        addKey,
        deleteKey,
        addAccount,
        deleteAccount,
        testAccount,
        testAllAccounts,
    } = useAccountActions({
        apiFetch,
        t,
        onMessage,
        onRefresh,
        config,
        fetchAccounts,
        resolveAccountIdentifier,
    })

    return (
        <div className="space-y-6">
            <QueueCards queueStatus={queueStatus} t={t} />

            <ApiKeysPanel
                t={t}
                config={config}
                keysExpanded={keysExpanded}
                setKeysExpanded={setKeysExpanded}
                setShowAddKey={setShowAddKey}
                copiedKey={copiedKey}
                setCopiedKey={setCopiedKey}
                onDeleteKey={deleteKey}
            />

            <AccountsTable
                t={t}
                accounts={accounts}
                loadingAccounts={loadingAccounts}
                testing={testing}
                testingAll={testingAll}
                batchProgress={batchProgress}
                totalAccounts={totalAccounts}
                page={page}
                totalPages={totalPages}
                resolveAccountIdentifier={resolveAccountIdentifier}
                onTestAll={testAllAccounts}
                onShowAddAccount={() => setShowAddAccount(true)}
                onTestAccount={testAccount}
                onDeleteAccount={deleteAccount}
                onPrevPage={() => fetchAccounts(page - 1)}
                onNextPage={() => fetchAccounts(page + 1)}
            />

            <AddKeyModal
                show={showAddKey}
                t={t}
                newKey={newKey}
                setNewKey={setNewKey}
                loading={loading}
                onClose={() => setShowAddKey(false)}
                onAdd={addKey}
            />

            <AddAccountModal
                show={showAddAccount}
                t={t}
                newAccount={newAccount}
                setNewAccount={setNewAccount}
                loading={loading}
                onClose={() => setShowAddAccount(false)}
                onAdd={addAccount}
            />
        </div>
    )
}
