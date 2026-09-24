import { describe, test, expect, beforeEach } from 'vitest'
import { record, remove, clear, items, typeText, MAX_ITEMS, MAX_TYPED_CHARS } from './remoteClipboard'

const win = { kind: 'vm', name: 'win11' }
const host = { kind: 'host', name: 'Host' }

describe('remote clipboard history', () => {
	beforeEach(() => clear())

	test('keeps sent and received text, newest first', () => {
		record({ text: 'hello', direction: 'to', target: win }, 1000)
		record({ text: 'from host', direction: 'from', target: host }, 2000)
		expect(items().map((i) => [i.direction, i.target.name, i.text])).toEqual([
			['from', 'Host', 'from host'],
			['to', 'win11', 'hello'],
		])
	})

	test("the remote echoing back what was just sent isn't a new entry", () => {
		record({ text: 'p4ss', direction: 'to', target: win }, 1000)
		expect(record({ text: 'p4ss', direction: 'from', target: win }, 1500)).toBeNull()
		expect(items()).toHaveLength(1)
		// Copying the same text again much later is a real copy.
		expect(record({ text: 'p4ss', direction: 'from', target: win }, 60000)).not.toBeNull()
	})

	test('the same text again moves to the top instead of duplicating', () => {
		record({ text: 'a', direction: 'to', target: win }, 1)
		record({ text: 'b', direction: 'to', target: win }, 2)
		record({ text: 'a', direction: 'to', target: win }, 3)
		expect(items().map((i) => i.text)).toEqual(['a', 'b'])
		// ...but the same text sent to another machine is its own entry.
		record({ text: 'a', direction: 'to', target: host }, 4)
		expect(items()).toHaveLength(3)
	})

	test('ignores empty text and caps the list', () => {
		expect(record({ text: '', direction: 'to', target: win })).toBeNull()
		for (let i = 0; i < MAX_ITEMS + 5; i++) record({ text: 't' + i, direction: 'to', target: win }, i)
		expect(items()).toHaveLength(MAX_ITEMS)
		expect(items()[0].text).toBe('t' + (MAX_ITEMS + 4))
	})

	test('remove and clear', () => {
		const a = record({ text: 'a', direction: 'to', target: win })
		record({ text: 'b', direction: 'to', target: win })
		remove(a.id)
		expect(items().map((i) => i.text)).toEqual(['b'])
		clear()
		expect(items()).toHaveLength(0)
	})
})

describe('typeText', () => {
	const fakeRfb = () => {
		const keys = []
		return { keys, sendKey: (sym, code, down) => down && keys.push(sym) }
	}

	test('types Latin-1, Unicode, Enter and Tab as keysyms', () => {
		const rfb = fakeRfb()
		typeText(rfb, 'aé€\r\n\t😀')
		expect(rfb.keys).toEqual([0x61, 0xe9, 0x010020ac, 0xff0d, 0xff09, 0x0101f600])
	})

	test('stops at the typing limit', () => {
		const rfb = fakeRfb()
		expect(typeText(rfb, 'x'.repeat(MAX_TYPED_CHARS + 10))).toBe(MAX_TYPED_CHARS)
	})
})
