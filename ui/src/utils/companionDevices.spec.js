import { describe, expect, it } from "vitest"
import { applyRename, cleanDeviceName, removalMessage, withoutDevice } from "./companionDevices.js"

const devices = [
	{ id: "dev_s25", name: "Galaxy S25", name_source: "device", is_online: true },
	{ id: "dev_tab", name: "Tab S9", name_source: "user", is_online: false },
]

describe("companion devices list", () => {
	it("shows the name the server saved, marked as the user's", () => {
		const out = applyRename(devices, "dev_s25", { id: "dev_s25", name: "Work phone", name_source: "user" }, "ignored")
		expect(out[0]).toMatchObject({ id: "dev_s25", name: "Work phone", name_source: "user", is_online: true })
		expect(out[1]).toBe(devices[1])
		expect(devices[0].name).toBe("Galaxy S25") // not mutated
	})

	it("falls back to the typed name when the answer has no device", () => {
		expect(applyRename(devices, "dev_tab", null, "Kitchen")[1].name).toBe("Kitchen")
		expect(applyRename(devices, "dev_tab", { id: "other", name: "x" }, "Kitchen")[1].name).toBe("Kitchen")
	})

	it("drops a removed device at once - no ghost 'offline' row", () => {
		expect(withoutDevice(devices, "dev_s25").map((d) => d.id)).toEqual(["dev_tab"])
	})

	it("says the phone's app was signed out when the server did that", () => {
		expect(removalMessage({ kept_backups: "/DATA/Companion/S25", signed_out: true })).toMatch(/signed out/)
		expect(removalMessage({ kept_backups: "" })).not.toMatch(/signed out/)
		expect(removalMessage(undefined)).toMatch(/removed/)
	})

	it("trims names and refuses empty or overlong ones", () => {
		expect(cleanDeviceName("  Pocket ")).toBe("Pocket")
		expect(cleanDeviceName("   ")).toBeNull()
		expect(cleanDeviceName("x".repeat(101))).toBeNull()
		expect(cleanDeviceName("x".repeat(100))).toHaveLength(100)
	})
})
