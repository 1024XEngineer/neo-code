import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import electron from 'vite-plugin-electron'
import renderer from 'vite-plugin-electron-renderer'
import { gatewayDevPlugin } from './vite-plugins/gateway-dev'
import path from 'path'

export default defineConfig(({ mode }) => {
	const isElectron = mode === 'electron'

	return {
		plugins: [
			react(),
			!isElectron && gatewayDevPlugin(),
			isElectron && electron([
				{
					entry: 'electron/main.ts',
				},
				{
					entry: 'electron/preload.ts',
					onstart(args) {
						args.reload()
					},
					vite: {
						build: {
							lib: {
								entry: 'electron/preload.ts',
								formats: ['cjs'],
								fileName: () => 'preload.cjs',
							},
						},
					},
				},
			]),
			isElectron && renderer(),
		].filter(Boolean),
		resolve: {
			alias: {
				'@': path.resolve(__dirname, './src'),
			},
		},
		server: {
			port: 5173,
			open: !isElectron,
			proxy: {
				'/healthz': {
					target: 'http://127.0.0.1:8080',
					changeOrigin: true,
				},
			},
		},
	}
})
