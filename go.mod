module github.com/darcyjoven/bdl-lsp

go 1.24

require (
	github.com/darcyjoven/tree-sitter-bdl v0.0.0
	github.com/darcyjoven/tree-sitter-per v0.0.0
	github.com/tree-sitter/go-tree-sitter v0.25.0
	go.lsp.dev/jsonrpc2 v0.10.0
	go.lsp.dev/protocol v0.12.0
)

require (
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.3.4 // indirect
	github.com/sijms/go-ora/v2 v2.9.0 // indirect
	go.lsp.dev/pkg v0.0.0-20210717090340-384b27a52fb2 // indirect
	go.lsp.dev/uri v0.3.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/sys v0.0.0-20220319134239-a9b59b0215f8 // indirect
)

// 开发期：两个 grammar 与 bdl-lsp 是兄弟目录。
// 发布/CI 时可去掉 replace，改用真实 tag（见 docs/PLAN-DETAILED-P1.md）。
replace github.com/darcyjoven/tree-sitter-bdl => ../tree-sitter-bdl

replace github.com/darcyjoven/tree-sitter-per => ../tree-sitter-per
