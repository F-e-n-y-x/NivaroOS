import { describe, expect, test } from 'vitest'
import { withHostAccessConfirm } from './hostAccess'

const need = { response: { status: 428, data: { message: 'x', data: { host_access: ['web: host network', '<b>: privileged'] } } } }
const vmSaying = (yes) => {
	const asked = []
	const confirmWindow = (o) => {
		asked.push(o)
		;(yes ? o.onConfirm : o.onCancel)()
	}
	return { asked, $t: (s) => s, confirmWindow }
}

describe('host access confirmation (audit H1)', () => {
	test('asks, then repeats the request with allow_host_access', async () => {
		const vm = vmSaying(true)
		const sent = []
		const call = async (o) => {
			sent.push(o)
			if (!o.params) throw need
			return { status: 200 }
		}
		const [res, options] = await withHostAccessConfirm(vm, call)
		expect(res.status).toBe(200)
		expect(sent).toEqual([{}, { params: { allow_host_access: true } }])
		expect(options).toEqual({ params: { allow_host_access: true } })
		expect(vm.asked[0].message).toContain('web: host network')
		expect(vm.asked[0].message).toContain('&lt;b&gt;') // server text is escaped
	})

	test('no means not installed; other errors pass through; no question for a normal app', async () => {
		const refuse = (err) => async () => {
			throw err
		}
		await expect(withHostAccessConfirm(vmSaying(false), refuse(need))).rejects.toThrow('Not installed')
		const other = { response: { status: 400 } }
		await expect(withHostAccessConfirm(vmSaying(true), refuse(other))).rejects.toBe(other)
		const vm = vmSaying(true)
		const [res] = await withHostAccessConfirm(vm, async () => ({ status: 200 }))
		expect(res.status).toBe(200)
		expect(vm.asked).toEqual([])
	})
})

describe('store update that wants more access (audit H1)', () => {
	test('asks with update wording, repeats with the OK; no means not updated', async () => {
		const vm = vmSaying(true)
		const sent = []
		const call = async (o) => {
			sent.push(o)
			if (!o.params) throw need
			return { status: 200 }
		}
		const [res] = await withHostAccessConfirm(vm, call, { update: true })
		expect(res.status).toBe(200)
		expect(sent[1]).toEqual({ params: { allow_host_access: true } })
		expect(vm.asked[0].confirmText).toBe('Update anyway')
		expect(vm.asked[0].message).toContain('current version keeps running')
		expect(vm.asked[0].message).toContain('web: host network')
		const refuse = async () => {
			throw need
		}
		await expect(withHostAccessConfirm(vmSaying(false), refuse, { update: true })).rejects.toThrow('Not updated')
	})
})
