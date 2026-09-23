// An image require()'d with a template literal (`require(\`@/assets/...\`)`)
// comes back from the production build as the ES module namespace
// ({ default: "/img/x.svg" }) rather than the bare URL string the dev build
// returned - and Buefy's <b-image> calls src.split() on it, which throws and
// leaves the tile without an icon. Normalize at every such call site.
export function assetUrl(mod) {
	if (mod && typeof mod === 'object' && 'default' in mod) return mod.default
	return mod
}
