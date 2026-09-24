// Roving-tabindex radio group keys (spec §13: type and preset cards are
// role="radio" in a radiogroup with arrow keys, Space to select). Returns
// the index to move to, or -1 when the key isn't ours. Arrow keys wrap;
// Home/End jump. `disabled(i)` items are skipped.
export function radioKeyTarget(key, index, count, disabled = () => false) {
	if (!count) return -1
	const step = dir => {
		for (let n = 1; n <= count; n++) {
			const i = (index + dir * n + count * n) % count
			if (!disabled(i)) return i
		}
		return index
	}
	switch (key) {
		case 'ArrowDown':
		case 'ArrowRight':
			return step(1)
		case 'ArrowUp':
		case 'ArrowLeft':
			return step(-1)
		case 'Home':
			for (let i = 0; i < count; i++) if (!disabled(i)) return i
			return index
		case 'End':
			for (let i = count - 1; i >= 0; i--) if (!disabled(i)) return i
			return index
		default:
			return -1
	}
}
