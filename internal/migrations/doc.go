// Package migrations 是 migrations/ 留底文件链的纯对账包:**无生产 API**。
//
// 82 号票定案(ADR 0008):migrations/ 是全库 schema 的可读档案,**文件从不
// 执行** —— 运行时机制仍是各 store 的 EnsureSchema 双方言自愈,无 runner、
// 无记账表。装载原语(Script/Step/Chain/Load/Split)与执行原语(ApplyMySQL/
// ApplySQLite)全部住在本包的 _test.go 里,生产构建(go build ./...)不含
// 它们 —— 任何启动路径在编译期都无法引用本包执行迁移文件,「文件从不执行」
// 由编译器而非约定保证。本包唯一的消费方是自带的形状对账测试:真实执行
// 「基线 + 增量链」后与全部 EnsureSchema 终形逐表比对名集合,持续证明留底
// 与运行时同形;未来若把文件接上 runner,另立翻面票。
package migrations
