import { build } from 'esbuild'
import { copyFile, mkdir } from 'node:fs/promises'

await mkdir('dist', { recursive: true })
await build({
  entryPoints: ['src/background.ts', 'src/content.ts', 'src/page.ts', 'src/options.ts'],
  bundle: true,
  outdir: 'dist',
  target: 'chrome120',
})
for (const file of ['manifest.json', 'rules.json', 'managed.json', 'options.html', 'shorts.html', 'guardian.css']) {
  await copyFile(file, `dist/${file}`)
}
