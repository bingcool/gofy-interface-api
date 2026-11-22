// Package interfaceapi exposes the version of the gofy-interface-api module.
//
// Applications and contract modules can report interfaceapi.Version in build
// logs instead of hard-coding a number that drifts from the library they build
// against. client.Version carries the same value into the User-Agent header.
// 包 interfaceapi 暴露 gofy-interface-api 模块的版本。
//
// 应用与契约模块可以在构建日志里报告 interfaceapi.Version，而不是硬编码一个会与所用库
// 脱节的数字。client.Version 把同一个值写进 User-Agent。
package interfaceapi

// Version is the gofy-interface-api module version, and the single place it is
// defined: client.Version reads this constant, so the User-Agent and the module
// version cannot drift apart.
// Version 是 gofy-interface-api 模块版本，也是它唯一的定义处：client.Version 读取这个
// 常量，因此 User-Agent 与模块版本不会各自漂移。
const Version = "0.8.0"
