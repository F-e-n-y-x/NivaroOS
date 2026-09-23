const webpack = require("webpack");
const path = require("path");
const NodePolyfillPlugin = require("node-polyfill-webpack-plugin");
const dotenv = require("dotenv");
// .env.production used to say NODE_ENV=prod - not "production", so both
// webpack (no minification, dev module format) and Vue (dev-mode warnings
// and checks on every render) ran their development builds in production:
// a ~31 MB unminified bundle and a visibly laggy desktop.
const isProd = process.env.NODE_ENV === "production" || process.env.NODE_ENV === "prod";
const TerserPlugin = require("terser-webpack-plugin");

module.exports = {
	// Overridable only for test builds served from a sub-path.
	publicPath: process.env.NVOS_PUBLIC_PATH || "/",
	runtimeCompiler: true,
	lintOnSave: false,
	productionSourceMap: false,
	pluginOptions: {},
	css: {
		loaderOptions: {
			sass: {
				sassOptions: {
					includePaths: ["./node_modules", "./src/assets"],
				},
			},
		},
	},

	chainWebpack: (config) => {
		// @novnc/novnc@1.7.0 ships as a real ESM package ("type": "module")
		// with a top-level await in its browser-feature-detection util -
		// needs this experiment on to build.
		config.set("experiments", { topLevelAwait: true });

		config.module
			.rule("mjs")
			.test(/\.mjs$/)
			.type("javascript/auto")
			.include.add(/node_modules/)
			.end();
		const oneOfsMap = config.module.rule("scss").oneOfs.store;
		oneOfsMap.forEach((item) => {
			item.use("style-resources-loader")
				.loader("style-resources-loader")
				.options({
					patterns: ["./src/assets/scss/common/_variables.scss", "./src/assets/scss/common/_color.scss"],
				})
				.end();
		});
		config.plugin("ignore").use(
			new webpack.IgnorePlugin({
				resourceRegExp: /^\.\/locale$/, // 这是一个示例，忽略所有 locale 文件
				contextRegExp: /moment$/, // 这是一个示例，只在 moment 库中忽略
			})
		);

		// Only these values reach the browser bundle. This used to inline
		// the ENTIRE build-machine environment ("process.env":
		// JSON.stringify(process.env)) into the public app.js - every shell
		// variable of whoever ran the build (tokens, SSH agent sockets, home
		// paths) readable by anyone who can load the login page.
		const publicEnv = { NODE_ENV: process.env.NODE_ENV, BASE_URL: "/" };
		for (const k of Object.keys(process.env)) {
			if (k.startsWith("VUE_APP_")) publicEnv[k] = process.env[k];
		}
		config.plugin("define").use(require("webpack/lib/DefinePlugin"), [
			{
				"process.env": JSON.stringify(publicEnv),
				BUILT_TIME: JSON.stringify(Date()),
			},
		]);
		// Some vendored stylesheets (@mdi/font, iconfonts-casaos) start with a
		// UTF-8 BOM. Harmless as separate <style> tags (the dev build), but a
		// production build concatenates them into one CSS file, leaving
		// U+FEFF right in front of their @font-face rules - browsers then
		// discard those rules as invalid and every icon in the UI vanishes.
		config.plugin("strip-css-bom").use({
			apply(compiler) {
				compiler.hooks.thisCompilation.tap("StripCssBom", compilation => {
					compilation.hooks.processAssets.tap(
						{ name: "StripCssBom", stage: webpack.Compilation.PROCESS_ASSETS_STAGE_OPTIMIZE_SIZE + 1 },
						assets => {
							for (const name of Object.keys(assets)) {
								if (!name.endsWith(".css")) continue
								const src = assets[name].source().toString()
								if (src.includes("\uFEFF")) {
									compilation.updateAsset(name, new webpack.sources.RawSource(src.replace(/\uFEFF/g, "")))
									// The content hash in the filename was computed before this
									// fix-up - rename so browsers holding a cached broken copy
									// fetch the fixed file instead.
									compilation.renameAsset(name, name.replace(/\.css$/, ".nobom.css"))
								}
							}
						}
					)
				})
			}
		});

		// 添加 NodePolyfillPlugin wbepack5 专用插件
		config.plugin("node-polyfill").use(NodePolyfillPlugin);

		// Production only
		if (isProd) {
			config.output.filename("[name].[contenthash:8].js").end();
			config.output.chunkFilename("[name].[contenthash:8].js").end();
			config.optimization.minimize(true);
			config.optimization.splitChunks({
				chunks: "all",
			});

			config.optimization
				.minimizer("css")
				.use(require("css-minimizer-webpack-plugin"), [
					{ minimizerOptions: { preset: ["default", { discardComments: { removeAll: true } }] } },
				]);
		} else if (process.env.ANALYZE) {
			// Development only, opt-in (was unconditional - collided with
			// other services already using its default port 8888 on this box)
			config.plugin('webpack-bundle-analyzer')
				.use(require('webpack-bundle-analyzer').BundleAnalyzerPlugin)
		}
	},
	devServer: {
		open: true,
		port: 8080,
		hot: true,
		proxy: {
			"/v1": {
				target: `http://${process.env.VUE_APP_DEV_IP}:${process.env.VUE_APP_DEV_PORT}`,
				changeOrigin: true,
			},
			"/v2": {
				target: `http://${process.env.VUE_APP_DEV_IP}:${process.env.VUE_APP_DEV_PORT}`,
				changeOrigin: true,
			},
			// getFileUrl() (src/mixins/mixin.js) returns bare `/v3/file?...`
			// URLs for <img>/<video> src attributes and downloads - without
			// this, those requests hit the dev server itself (which has no
			// such route and falls back to index.html), not the real
			// backend, so every image/video/doc/excel/pdf viewer shows a
			// broken-image icon instead of the actual file.
			"/v3": {
				target: `http://${process.env.VUE_APP_DEV_IP}:${process.env.VUE_APP_DEV_PORT}`,
				changeOrigin: true,
			},
		},
	},
};
