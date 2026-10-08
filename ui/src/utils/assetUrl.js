// URL of an image under src/assets: assetUrl('img/logo/logo.svg'). The build
// fingerprints these files, so templates can't hard-code their paths.
// Unknown paths give undefined (b-image then shows its src-fallback).
const urls = import.meta.glob('../assets/{img,background}/**/*.{svg,png,jpg,jpeg,webp,gif}', { eager: true, query: '?url', import: 'default' })

export function assetUrl(path) {
	return urls[`../assets/${path}`]
}

// A saved wallpaper names a built-in image by its URL in the build that
// saved it (/img/wallpaper01.a4b92b0e.jpg, hash per build): point any
// build's name at this build's file.
export function currentBuiltinUrl(url) {
	const m = /\/img\/(wallpaper01|wallpaper02|default_wallpaper)\.[\w-]+\.jpg$/.exec(url || '')
	return (m && assetUrl(`background/${m[1]}.jpg`)) || url
}
