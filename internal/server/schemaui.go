package server

// 「数据库字段」视图用的自定义请求（PLUGIN-PLAN P2-1 §7.3）。
//
// 设计：视图只发两个请求，服务端负责正则过滤与"展开才读表文件"的惰性加载：
//   - `bdl/schemaTables`  总览 + 按正则过滤的表列表（带总数，供"共 N 张表"展示）；
//   - `bdl/schemaColumns` 单张表的列（视图展开某个表节点时才发）。
//
// 全部只读内存/文件，不连库、不写盘（写盘是 tiptop 的职责）。

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/darcyjoven/bdl-lsp/internal/schema"
	"go.lsp.dev/jsonrpc2"
)

const (
	// MethodSchemaTables 返回（可过滤的）表清单与统计。
	MethodSchemaTables = "bdl/schemaTables"
	// MethodSchemaColumns 返回单张表的列（惰性加载）。
	MethodSchemaColumns = "bdl/schemaColumns"
)

type schemaTablesParams struct {
	// Query 是**正则**（大小写不敏感）；空 = 不过滤。
	Query string `json:"query"`
	// Limit 限制返回条数（0 = 不限制）；视图用它做分页/折叠。
	Limit int `json:"limit"`
}

// schemaTableInfo 是视图里一个表节点需要的信息。
type schemaTableInfo struct {
	Name    string `json:"name"`
	Desc    string `json:"desc,omitempty"`
	Columns int    `json:"columns"`
}

type schemaTablesResult struct {
	// Enabled 是 `bdl.schema.enable` 的当前值。
	Enabled bool `json:"enabled"`
	// Present 表示资料目录里确实有 tables.json。
	Present bool `json:"present"`
	// Error 是资料坏了时的原因（Present=false 且 Error!="" 才有意义）。
	Error string `json:"error,omitempty"`
	// DataDir / ImportedAt / Source / RowsTotal 是资料自身的元信息。
	DataDir    string `json:"dataDir,omitempty"`
	ImportedAt string `json:"importedAt,omitempty"`
	Source     string `json:"source,omitempty"`
	RowsTotal  int    `json:"rowsTotal,omitempty"`
	// Total 是表总数；Matched 是过滤后的条数；Returned 是实际返回的条数。
	Total    int `json:"total"`
	Matched  int `json:"matched"`
	Returned int `json:"returned"`
	// FilterError 是正则非法时的提示（仍返回字面量匹配的结果，视图不中断）。
	FilterError string            `json:"filterError,omitempty"`
	Tables      []schemaTableInfo `json:"tables,omitempty"`
}

type schemaColumnsParams struct {
	Table string `json:"table"`
}

type schemaColumnsResult struct {
	Enabled bool   `json:"enabled"`
	Present bool   `json:"present"`
	Table   string `json:"table"`
	Desc    string `json:"desc,omitempty"`
	// Missing 表示资料里没有这张表（或表文件读不出来）。
	Missing bool `json:"missing,omitempty"`
	// Columns 直接复用 schema.Column 的 JSON 形状（name/desc/type/length/...）。
	Columns []schema.Column `json:"columns,omitempty"`
}

func (s *Server) handleSchemaTables(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params schemaTablesParams
	if raw := req.Params(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return reply(ctx, nil, fmt.Errorf("解析 %s 参数: %w", MethodSchemaTables, err))
		}
	}
	settings := s.settings()
	res := schemaTablesResult{
		Enabled: settings.SchemaEnabled(),
		DataDir: s.schemaDataDir(),
	}
	ix := s.schemaIndex()
	if ix == nil {
		res.Error = s.schemaLoadErr()
		return reply(ctx, res, nil)
	}
	res.Present = true
	res.ImportedAt = ix.ImportedAt
	res.Source = ix.Source
	res.RowsTotal = ix.RowsTotal

	matched, total, ferr := ix.Search(params.Query, params.Limit)
	res.Total = total
	res.Matched = len(matched)
	if ferr != nil {
		res.FilterError = ferr.Error()
	}
	res.Returned = len(matched)
	res.Tables = make([]schemaTableInfo, 0, len(matched))
	for _, e := range matched {
		if e == nil {
			continue
		}
		res.Tables = append(res.Tables, schemaTableInfo{Name: e.Name, Desc: e.Desc, Columns: e.Columns})
	}
	return reply(ctx, res, nil)
}

func (s *Server) handleSchemaColumns(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params schemaColumnsParams
	if raw := req.Params(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return reply(ctx, nil, fmt.Errorf("解析 %s 参数: %w", MethodSchemaColumns, err))
		}
	}
	settings := s.settings()
	res := schemaColumnsResult{Enabled: settings.SchemaEnabled(), Table: params.Table}
	if params.Table == "" {
		res.Missing = true
		return reply(ctx, res, nil)
	}
	ix := s.schemaIndex()
	if ix == nil {
		res.Missing = true
		return reply(ctx, res, nil)
	}
	res.Present = true
	tb, ok := ix.Table(params.Table)
	if !ok || tb == nil {
		res.Missing = true
		return reply(ctx, res, nil)
	}
	res.Table = tb.Name
	res.Desc = tb.Desc
	res.Columns = tb.Columns
	return reply(ctx, res, nil)
}

// schemaDataDir 返回字段资料目录的绝对路径（供视图显示）。
func (s *Server) schemaDataDir() string {
	settings := s.settings()
	dir := settings.SchemaDataDir()
	if s.rootPath == "" {
		return dir
	}
	if dir != "" && dir[0] != '/' && !(len(dir) > 1 && dir[1] == ':') {
		return s.rootPath + "/" + dir
	}
	return dir
}
