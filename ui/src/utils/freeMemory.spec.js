import { describe, expect, it } from "vitest"
import { describeClear, formatBytes } from "./freeMemory.js"

const answer = {
	before: { mem_free: 1446326272, buffers: 536870912, cached: 15032385536, swap_used: 2147483648 },
	after: { mem_free: 3701617664, buffers: 104857600, cached: 12884901888, swap_used: 2147483648 },
	freed: 2255291392,
	swap_reclaimed: false,
}

describe("free up memory", () => {
	it("formats sizes with one decimal", () => {
		expect(formatBytes(0)).toBe("0 B")
		expect(formatBytes(512)).toBe("512 B")
		expect(formatBytes(2255291392)).toBe("2.1 GB")
	})

	it("says what was freed, with before and after", () => {
		const r = describeClear(answer)
		expect(r.freedText).toBe("2.1 GB")
		expect(r.alreadyClear).toBe(false)
		expect(r.rows.map((x) => [x.label, x.before, x.after])).toEqual([
			["Free", "1.3 GB", "3.4 GB"],
			["Cache and buffers", "14.5 GB", "12.1 GB"],
		])
	})

	it("adds swap only when it changed", () => {
		const r = describeClear({ ...answer, swap_reclaimed: true, after: { ...answer.after, swap_used: 0 } })
		expect(r.rows[2]).toMatchObject({ label: "Swap in use", before: "2.0 GB", after: "0 B" })
	})

	it("nothing to free reads as already clear", () => {
		expect(describeClear({ before: {}, after: {}, freed: 1000 }).alreadyClear).toBe(true)
	})
})
