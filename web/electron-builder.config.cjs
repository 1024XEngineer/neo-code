/**
 * electron-builder 配置
 */
const config = {
	appId: 'com.neocode.app',
	productName: 'NeoCode',
	directories: {
		output: 'release',
		buildResources: 'build',
	},
	files: [
		'dist-electron/**/*',
	],
	extraResources: [
		{
			from: 'build',
			to: '.',
			filter: ['neocode-gateway', 'neocode-gateway.exe'],
		},
	],
	win: {
		target: [
			{
				target: 'nsis',
				arch: ['x64'],
			},
			{
				target: 'portable',
				arch: ['x64'],
			},
		],
	},
	nsis: {
		oneClick: false,
		perMachine: false,
		allowToChangeInstallationDirectory: true,
		deleteAppDataOnUninstall: false,
		artifactName: '${productName}-${version}-Setup.${ext}',
		language: '2052',
	},
	portable: {
		artifactName: '${productName}-${version}-Portable.${ext}',
	},
	mac: {
		target: [
			{
				target: 'dmg',
				arch: ['x64', 'arm64'],
			},
		],
		artifactName: '${productName}-${version}-${arch}.${ext}',
	},
	linux: {
		target: [
			{
				target: 'AppImage',
				arch: ['x64'],
			},
		],
		artifactName: '${productName}-${version}.${ext}',
	},
	publish: {
		provider: 'github',
		owner: '1024XEngineer',
		repo: 'neo-code',
		releaseType: 'release',
	},
}

module.exports = config
