import { describe, expect, it, vi } from "vitest"

const store = {}
vi.mock("@/service/users.js", () => ({
	default: {
		getCustomStorage: vi.fn(key => Promise.resolve({ data: { data: store[key] } })),
		setCustomStorage: vi.fn((key, data) => {
			if (store.fail) return Promise.reject(new Error("offline"))
			store[key] = JSON.parse(JSON.stringify(data))
			return Promise.resolve({ data: { success: 200 } })
		}),
	},
}))

const { hiddenFields, loadHiddenFields, showsField, toggleField, STORAGE_KEY } = await import("./widgetFields.js")

describe("widget field toggles", () => {
	it("loads the user's saved choices, toggles and persists them", async () => {
		store[STORAGE_KEY] = { cpu: ["temp"] }
		await loadHiddenFields()
		expect(showsField("cpu", "temp")).toBe(false)
		expect(showsField("cpu", "clock")).toBe(true)
		expect(showsField("network", "down")).toBe(true) // unknown widget: everything on

		await toggleField("cpu", "clock")
		await toggleField("cpu", "temp")
		expect(store[STORAGE_KEY]).toEqual({ cpu: ["clock"] })
		expect(showsField("cpu", "temp")).toBe(true)
	})

	it("rolls back when the save fails", async () => {
		store.fail = true
		await expect(toggleField("network", "up")).rejects.toThrow()
		expect(showsField("network", "up")).toBe(true)
		expect(hiddenFields.network).toEqual([])
		store.fail = false
	})
})
