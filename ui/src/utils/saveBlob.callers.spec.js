// @vitest-environment jsdom
import { describe, test, expect, vi, beforeEach } from 'vitest'

import RunLog from '@/apps/backup/components/RunLog.vue'
import BackupJobWizardWindow from '@/apps/backup/windows/BackupJobWizardWindow.vue'
import AboutPanel from '@/apps/settings/AboutPanel.vue'
import ContainerConsolePanel from '@/shell/desktop/ContainerConsolePanel.vue'
import { saveBlob } from '@/utils/saveBlob'

// The hand-written <a download> blocks all route through saveBlob, with the
// same file names and contents they had before. (vi.mock is hoisted.)
vi.mock('@/utils/saveBlob', () => ({ saveBlob: vi.fn() }))

// jsdom's Blob has no .text()
function readText(blob) {
	return new Promise((resolve) => {
		const f = new FileReader()
		f.onload = () => resolve(f.result)
		f.readAsText(blob)
	})
}

const saved = async () => {
	expect(saveBlob).toHaveBeenCalledTimes(1)
	const [blob, name] = saveBlob.mock.calls[0]
	return { name, type: blob.type, text: await readText(blob) }
}

describe('download buttons use saveBlob', () => {
	beforeEach(() => saveBlob.mockClear())

	test('AboutPanel crash report', async () => {
		const ctx = { $api: { sys: { getCrashReport: () => Promise.resolve({ data: { a: 1 } }) } } }
		await AboutPanel.methods.downloadCrash.call(ctx, 'c7')
		await Promise.resolve()
		expect(await saved()).toEqual({ name: 'nivaroos-crash-c7.json', type: 'application/json', text: JSON.stringify({ a: 1 }, null, 2) })
	})

	test('RunLog full log', async () => {
		const ctx = {
			runId: 'r1',
			bkApi: { getRunLog: () => Promise.resolve({ lines: ['x'], done: true }) },
			plainText: lines => lines.join('\n')
		}
		await RunLog.methods.download.call(ctx)
		expect(await saved()).toEqual({ name: 'r1.log', type: 'text/plain;charset=utf-8', text: 'x\n' })
	})

	test('wizard recovery key', async () => {
		const ctx = { created: { name: 'a/b', dest: { label: 'L', sub_path: 'p' } }, recoveryKey: 'K', $t: (k, v) => `${v.name}|${v.key}|${v.folder}` }
		BackupJobWizardWindow.methods.downloadRecoveryKey.call(ctx)
		expect(await saved()).toEqual({ name: 'NivaroOS recovery key - a-b.txt', type: 'text/plain', text: 'a/b|K|L / p\n' })
	})

	test('container console logs', async () => {
		const ctx = { visibleText: () => 'log', displayTitle: 'web' }
		ContainerConsolePanel.methods.downloadLogs.call(ctx)
		const day = new Date().toISOString().slice(0, 10)
		expect(await saved()).toEqual({ name: `web-logs-${day}.log`, type: 'text/plain;charset=utf-8', text: 'log\n' })
	})
})
