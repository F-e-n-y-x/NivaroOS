import { describe, expect, test } from 'vitest'
import Steps from './OAuthSignInSteps.vue'

// Runs the component's logic on a plain object: data + computed getters + methods.
function make(state) {
	const emitted = []
	const vm = { type: 'drive', own: false, clientId: '', clientSecret: '', token: '', $t: s => s, $emit: (_, v) => emitted.push(v), ...state }
	for (const [k, get] of Object.entries(Steps.computed)) Object.defineProperty(vm, k, { get: get.bind(vm) })
	for (const [k, fn] of Object.entries(Steps.methods)) vm[k] = fn.bind(vm)
	return { vm, emitted }
}

describe('OAuthSignInSteps', () => {
	test("rclone's app: token only, client cleared (so a reconnect switches back)", () => {
		const { vm, emitted } = make({ token: ' {"access_token":"a"} ' })
		vm.emit()
		expect(emitted.pop()).toEqual({ token: '{"access_token":"a"}', client_id: '', client_secret: '' })
		expect(vm.command).toBe('rclone authorize "drive"')
	})

	test('own app: sends the client and puts it in the command', () => {
		const { vm, emitted } = make({ own: true, clientId: '123-abc.apps.googleusercontent.com', clientSecret: 'GOCSPX-x_y', token: '{"access_token":"a"}' })
		vm.emit()
		expect(emitted.pop()).toEqual({ token: '{"access_token":"a"}', client_id: '123-abc.apps.googleusercontent.com', client_secret: 'GOCSPX-x_y' })
		expect(vm.command).toBe('rclone authorize "drive" "123-abc.apps.googleusercontent.com" "GOCSPX-x_y"')
	})

	test('own app: nothing to save until the client is complete', () => {
		const { vm, emitted } = make({ own: true, clientId: 'id', token: '{"access_token":"a"}' })
		vm.emit()
		expect(emitted.pop()).toBeNull()
	})

	test('a partial token is refused before it reaches the server', () => {
		for (const bad of ['{"access_token":"ya29.a","tok', '"access_token":"a"}', 'Paste the following', '{"token_type":"Bearer"}']) {
			const { vm, emitted } = make({ token: bad })
			expect(vm.tokenError).not.toBe('')
			vm.emit()
			expect(emitted.pop()).toBeNull()
		}
	})

	test('shell characters are refused (the command runs in a terminal)', () => {
		for (const bad of ['a"; rm -rf /', 'a$(id)', 'a`id`', 'a b']) {
			const { vm, emitted } = make({ own: true, clientId: bad, clientSecret: 'ok', token: 'tok' })
			expect(vm.credsOk).toBe(false)
			expect(vm.idError).not.toBe('')
			vm.emit()
			expect(emitted.pop()).toBeNull()
		}
	})
})
