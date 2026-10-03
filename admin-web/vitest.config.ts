import { defineConfig } from 'vitest/config'

// 只收集本应用 src 下的测试;vben/ 收编包的上游测试不进本仓测试链
// (其依赖的 happy-dom 环境与断言形态属于上游工具链,35 号票裁掉)。
export default defineConfig({
  test: {
    include: ['src/**/*.{test,spec}.?(c|m)[jt]s?(x)'],
  },
})
