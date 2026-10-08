import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

// Some vendored stylesheets (@mdi/font, iconfonts-casaos) start with a
// UTF-8 BOM. Concatenated into one CSS file it lands right in front of
// their @font-face rules - browsers then drop those rules and every icon
// in the UI vanishes.
const stripCssBom = {
	name: 'strip-css-bom',
	generateBundle(_, bundle) {
		for (const f of Object.values(bundle)) {
			if (f.type === 'asset' && f.fileName.endsWith('.css') && typeof f.source === 'string') f.source = f.source.replace(/﻿/g, '')
		}
	},
}

export default defineConfig(({ mode }) => {
	// .env.dev: where `pnpm dev` proxies the API to.
	const env = loadEnv(mode, process.cwd(), 'VUE_APP_')
	const backend = `http://${env.VUE_APP_DEV_IP}:${env.VUE_APP_DEV_PORT}`
	return {
		// Overridable only for test builds served from a sub-path.
		base: process.env.NVOS_PUBLIC_PATH || '/',
		// Only VUE_APP_* values reach the bundle (import.meta.env).
		envPrefix: 'VUE_APP_',
		plugins: [vue(), stripCssBom],
		resolve: {
			alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
			extensions: ['.mjs', '.js', '.json', '.vue'],
		},
		define: {
			// jshint's bundled Node shims (util, console-browserify) read process.env.
			'process.env': '{}',
			__VUE_PROD_DEVTOOLS__: false,
			__VUE_PROD_HYDRATION_MISMATCH_DETAILS__: false,
			__VUE_I18N_FULL_INSTALL__: true,
			__VUE_I18N_LEGACY_API__: true,
			__INTLIFY_PROD_DEVTOOLS__: false,
		},
		css: {
			preprocessorOptions: {
				scss: {
					loadPaths: ['node_modules', 'src/assets'],
					additionalData: '@import "@/assets/scss/common/_variables.scss";\n@import "@/assets/scss/common/_color.scss";\n',
					// Bulma 0.9 / Buefy (what Buefy-next still builds on) predate Sass modules.
					quietDeps: true,
					silenceDeprecations: ['import', 'global-builtin', 'color-functions', 'slash-div', 'legacy-js-api', 'if-function'],
				},
			},
		},
		build: {
			outDir: 'build/sysroot/var/lib/nivaroos/www',
			emptyOutDir: true,
			// Images and fonts stay files (cached by the browser), never inlined in JS/CSS.
			assetsInlineLimit: 0,
			chunkSizeWarningLimit: 4096,
			// The webpack build's layout: app.<hash>.js (utils/updateWatcher.js
			// reads it from index.html, hex so tabs on an older build still
			// recognise it), css/, img/, fonts/. Saved wallpapers point into img/.
			rollupOptions: {
				output: {
					hashCharacters: 'hex',
					entryFileNames: 'app.[hash].js',
					chunkFileNames: 'js/[name].[hash].js',
					assetFileNames: ({ names }) => {
						const ext = (names[0] || '').split('.').pop()
						const dir = ext === 'css' ? 'css' : /^(woff2?|ttf|eot|otf)$/.test(ext) ? 'fonts' : /^(svg|png|jpe?g|gif|webp|ico)$/.test(ext) ? 'img' : 'assets'
						return `${dir}/[name].[hash][extname]`
					},
				},
			},
		},
		// The vendored API client is a linked CommonJS package: pre-bundle it for dev.
		optimizeDeps: { include: ['recasa-appmanagement-openapi'] },
		server: {
			port: 8080,
			open: true,
			proxy: {
				'/v1': { target: backend, changeOrigin: true },
				'/v2': { target: backend, changeOrigin: true, ws: true },
				// getFileUrl() (src/mixins/mixin.js) returns bare /v3/file?... URLs for
				// <img>/<video> src attributes and downloads.
				'/v3': { target: backend, changeOrigin: true },
			},
		},
		test: {},
	}
})
