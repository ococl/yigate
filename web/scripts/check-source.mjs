import { readdir, readFile } from 'node:fs/promises'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

// 轻量级源码门禁：不引入额外依赖，可在任何安装了 Node 的 CI 镜像中执行。
// 覆盖 ESLint 缺失时最容易漏掉的两类问题：类型逃逸与超长单文件。
const srcRoot = fileURLToPath(new URL('../src', import.meta.url))
const MAX_LINES = 1300
const FORBIDDEN = [
  { pattern: /\bas any\b/, label: 'as any' },
  { pattern: /\bany\[\]/, label: 'any[]' },
  { pattern: /@ts-ignore/, label: '@ts-ignore' },
]

async function collect(dir) {
  const entries = await readdir(dir, { withFileTypes: true })
  const files = []
  for (const entry of entries) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      files.push(...await collect(full))
    } else if (/\.(ts|vue)$/.test(entry.name) && !entry.name.endsWith('.d.ts')) {
      // auto-imports.d.ts / components.d.ts 由插件自动生成，不受人工规范约束。
      files.push(full)
    }
  }
  return files
}

const files = await collect(srcRoot)
let failures = 0
let largest = { name: '', lines: 0 }

for (const file of files) {
  const rel = relative(srcRoot, file)
  const content = await readFile(file, 'utf8')
  const lines = content.split('\n').length
  if (lines > largest.lines) largest = { name: rel, lines }
  if (lines > MAX_LINES) {
    console.error(`[source-check] ${rel}: ${lines} 行，超过上限 ${MAX_LINES}`)
    failures++
  }
  for (const { pattern, label } of FORBIDDEN) {
    const match = content.match(pattern)
    if (match) {
      console.error(`[source-check] ${rel}: 禁止使用 ${label}`)
      failures++
    }
  }
}

if (failures > 0) {
  console.error(`[source-check] ${failures} 项检查未通过`)
  process.exit(1)
}
console.log(`source checks passed: ${files.length} files, largest ${largest.name} (${largest.lines} lines)`)
