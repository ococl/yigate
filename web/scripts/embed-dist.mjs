import { mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { dirname, extname, join, relative, resolve } from 'node:path'
import { gzipSync } from 'node:zlib'
import { fileURLToPath } from 'node:url'

const scriptDir = dirname(fileURLToPath(import.meta.url))
const webDir = resolve(scriptDir, '..')
const repoRoot = resolve(webDir, '..')
const source = resolve(process.argv[2] ?? join(webDir, 'dist'))
const target = resolve(process.argv[3] ?? join(repoRoot, 'src/proxy/webui_dist'))

async function walk(dir) {
  const entries = await readdir(dir, { withFileTypes: true })
  const files = []
  for (const entry of entries) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      files.push(...await walk(full))
    } else if (entry.isFile()) {
      files.push(full)
    }
  }
  return files
}

await readFile(join(source, 'index.html'))
// 清理上一次的构建产物，但保留被跟踪的 PLACEHOLDER.md，
// 避免 `make build` / CI 构建后工作区出现被跟踪文件的改动。
const placeholderName = 'PLACEHOLDER.md'
const existing = await readdir(target, { withFileTypes: true }).catch(() => [])
for (const entry of existing) {
  if (entry.name === placeholderName) continue
  await rm(join(target, entry.name), { recursive: true, force: true })
}
await mkdir(target, { recursive: true })

const files = await walk(source)
let rawBytes = 0
let embeddedBytes = 0
let compressedFiles = 0

for (const file of files) {
  const rel = relative(source, file)
  const data = await readFile(file)
  const output = join(target, rel)
  await mkdir(dirname(output), { recursive: true })

  // index.html 保持可读，SPA 路由和首次请求无需解压。
  if (rel === 'index.html' || extname(rel) === '.gz') {
    await writeFile(output, data)
    embeddedBytes += data.length
  } else {
    const gz = gzipSync(data, { level: 9 })
    if (gz.length + 64 < data.length) {
      await writeFile(`${output}.gz`, gz)
      embeddedBytes += gz.length
      compressedFiles++
    } else {
      await writeFile(output, data)
      embeddedBytes += data.length
    }
  }
  rawBytes += data.length
}

const assets = await readdir(join(target, 'assets')).catch(() => [])
if (!assets.some((name) => name.endsWith('.js') || name.endsWith('.js.gz'))) {
  throw new Error(`WebUI assets are missing in ${target}`)
}

const saved = rawBytes - embeddedBytes
const percent = rawBytes === 0 ? 0 : (saved / rawBytes) * 100
console.log(
  `Embedded WebUI: ${files.length} files, ${rawBytes} -> ${embeddedBytes} bytes ` +
  `(${saved} saved, ${percent.toFixed(1)}%), ${compressedFiles} gzip assets`,
)
