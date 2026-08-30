const webpack = require("webpack");
const path = require("path");
const NodePolyfillPlugin = require("node-polyfill-webpack-plugin");
const dotenv = require("dotenv");
const isProd = process.env.NODE_ENV === "prod";
const TerserPlugin = require("terser-webpack-plugin");

module.exports = {
	publicPath: "/",
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

		config.plugin("define").use(require("webpack/lib/DefinePlugin"), [
			{
				"process.env": JSON.stringify(process.env),
				BUILT_TIME: JSON.stringify(Date()),
			},
		]);
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
