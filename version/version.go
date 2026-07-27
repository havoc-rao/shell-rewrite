// Package version 提供 shr 的版本号。
// 版本号来自本目录的 VERSION 文件，编译时通过 go:embed 静态嵌入，
// 是整个项目的唯一版本真相源（goreleaser 与 CI 均读取此文件）。
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var raw string

// Version 为当前版本号（已去除首尾空白与换行）。
var Version = strings.TrimSpace(raw)
