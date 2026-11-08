import { readdir, readFile, stat } from 'node:fs/promises'
import { gzipSync } from 'node:zlib'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

// 构建体积预算：防止无意间把整包组件库打进某个页面 chunk。
// 以 gzip 后体积衡量（实际传输大小），阈值留出约 30% 余量。
const distRoot = fileURLToPath(new URL('../dist', import.meta.url))
const MAX_CHUNK_GZIP = 100 * 1024
const MAX_TOTAL_GZIP = 600 * 1024

async function collect(dir) {
  const entries = await readdir(dir, { withFileTypes: true })
  const files = []
  for (const entry of entries) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      files.push(...await collect(full))
    } else if (!entry.name.endsWith('.map')) {
      files.push(full)
    }
  }
  return files
}

let files
try {
  files = await collect(distRoot)
} catch {
  console.error(`[bundle-size] 未找到构建产物：${distRoot}，请先执行 npm run build`)
  process.exit(1)
}

let total = 0
const chunks = []
for (const file of files) {
  const info = await stat(file)
  const gzipSize = gzipSync(await readFile(file), { level: 9 }).length
  total += gzipSize
  chunks.push({ name: relative(distRoot, file), raw: info.size, gzip: gzipSize })
}

chunks.sort((a, b) => b.gzip - a.gzip)
const largest = chunks[0]
let failures = 0

for (const chunk of chunks) {
  if (chunk.gzip > MAX_CHUNK_GZIP) {
    console.error(
      `[bundle-size] ${chunk.name}: gzip ${(chunk.gzip / 1024).toFixed(1)}KB，超过单文件上限 ${MAX_CHUNK_GZIP / 1024}KB`,
    )
    failures++
  }
}

if (total > MAX_TOTAL_GZIP) {
  console.error(
    `[bundle-size] 总体积 gzip ${(total / 1024).toFixed(1)}KB，超过上限 ${MAX_TOTAL_GZIP / 1024}KB`,
  )
  failures++
}

if (failures > 0) {
  process.exit(1)
}

console.log(
  `bundle size ok: ${chunks.length} files, total gzip ${(total / 1024).toFixed(1)}KB, ` +
    `largest ${largest.name} ${(largest.gzip / 1024).toFixed(1)}KB`,
)
