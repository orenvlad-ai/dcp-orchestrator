import type { ForgeConfig } from "@electron-forge/shared-types";
import { VitePlugin } from "@electron-forge/plugin-vite";

// DCP I8 intentionally defines a package-only macOS application. There are no
// makers, publishers, feeds, update metadata, or release upload paths in this
// build graph. Local installation is performed by the repository-owned script.
const config: ForgeConfig = {
	packagerConfig: {
		asar: true,
		appBundleId: "pro.devcontrol.dcp-orchestrator",
		name: "DCP Orchestrator",
		executableName: "dcp-orchestrator",
		appCategoryType: "public.app-category.developer-tools",
		icon: "assets/icon",
		extraResource: [
			"daemon",
			"assets/icon.png",
			"assets/trayIconTemplate.png",
			"assets/trayIconTemplate@2x.png",
			"../LICENSE",
		],
		extendInfo: {
			DCPUpstreamCommit: "1df40e93772c2c48e916870d9c3ddf8f29a69f84",
			DCPContour: "dcp-i8-packaged-app-v1",
			NSHumanReadableCopyright: "Based on Agent Orchestrator, Copyright 2026 Untrivial; DCP modifications",
		},
	},
	rebuildConfig: {},
	makers: [],
	publishers: [],
	plugins: [
		new VitePlugin({
			build: [
				{ entry: "src/main.ts", config: "vite.main.config.ts", target: "main" },
				{ entry: "src/preload.ts", config: "vite.preload.config.ts", target: "preload" },
				{ entry: "src/annotate-preload.ts", config: "vite.preload.config.ts", target: "preload" },
			],
			renderer: [{ name: "main_window", config: "vite.renderer.config.ts" }],
		}),
	],
};

export default config;
